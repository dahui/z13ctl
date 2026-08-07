package daemon

// resume.go — sleep/resume watcher via systemd-logind DBus signals.
//
// Listens for org.freedesktop.login1.Manager.PrepareForSleep on the system bus.
// On (false) — resume — lighting and all volatile settings (undervolt, TDP, fan
// curves) are reapplied from daemon state. On (true) — sleep — the lightbar is
// turned off and the fans are handed back to firmware auto.
//
// The fan release is not housekeeping; without it the fans never stop while the
// machine is asleep (issue #15 follow-up). The Z13 supports only s2idle — there
// is no "deep" in /sys/power/mem_sleep — so the EC keeps running its fan control
// loop the whole time the machine is suspended. In firmware auto mode
// (pwm_enable=2) the EC stops the fans; with a custom curve (pwm_enable=1) it
// keeps driving them from the curve's PWM values, and every point of
// cli.HighTDPFanCurve is at or above 50%, so a machine on a high sustained TDP
// suspends with both fans at half speed indefinitely.
//
// Two things make this delicate rather than a one-line write:
//
//   - Releasing the fans while a sustained limit above cli.TDPMaxSafe is in force
//     would drop the very floor ApplyTDPSafely refuses to run without, so the
//     limits come down first and a failure there cancels the release.
//   - PrepareForSleep(true) is advisory unless someone holds a delay inhibitor.
//     Without one, logind is free to freeze userspace before these writes land,
//     which looks exactly like the bug being fixed — hence takeSleepInhibitor.

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/z13ctl/internal/aura"
	"github.com/dahui/z13ctl/internal/cli"
)

// watchResume connects to the system DBus and listens for sleep and resume
// events. PrepareForSleep(true) calls releaseVolatileState, (false) calls
// restoreVolatileState. Blocks until ctx is cancelled.
func (d *Daemon) watchResume(ctx context.Context) {
	conn, err := dbus.SystemBus()
	if err != nil {
		slog.Warn("cannot connect to system DBus for resume watcher", "err", err)
		return
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		slog.Warn("failed to add DBus match rule for PrepareForSleep", "err", err)
		return
	}

	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)

	// Held before the signal can arrive — a delay inhibitor taken after
	// PrepareForSleep(true) is already too late to delay anything.
	inhibitor := takeSleepInhibitor(conn)
	defer releaseSleepInhibitor(&inhibitor)

	slog.Info("resume watcher started (listening for PrepareForSleep)")

	for {
		select {
		case <-ctx.Done():
			conn.RemoveSignal(ch)
			return
		case sig := <-ch:
			if sig == nil {
				continue
			}
			if sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" {
				continue
			}
			if len(sig.Body) < 1 {
				continue
			}
			sleeping, ok := sig.Body[0].(bool)
			if !ok {
				continue
			}
			if sleeping {
				slog.Info("system entering sleep")
				// d.dev is guarded by d.mu: the hotplug watcher closes and
				// replaces it on keyboard reattach, so an unlocked read here
				// races with that swap and can write to a closed descriptor.
				d.mu.Lock()
				if d.dev != nil {
					if err := aura.TurnOff(d.dev); err != nil {
						slog.Warn("failed to turn off lighting before sleep", "err", err)
					}
				}
				d.mu.Unlock()

				if d.sleepRelease {
					d.releaseVolatileState()
				} else {
					slog.Info("sleep: fan release disabled (--no-sleep-release); the custom curve stays in force",
						"wakeup_count", wakeupCount())
				}

				// logind suspends as soon as the last delay lock closes, so this
				// must happen here rather than on the way out of the loop — the
				// whole point is that the writes above have already landed.
				releaseSleepInhibitor(&inhibitor)
				continue
			}
			slog.Info("system resumed from sleep, restoring volatile state")
			d.restoreVolatileState()
			inhibitor = takeSleepInhibitor(conn)
		}
	}
}

// sysPowerDir is /sys/power, a var so tests can point it at a temp tree.
var sysPowerDir = "/sys/power"

// wakeupCount reads /sys/power/wakeup_count — the kernel's running count of
// registered wakeup events. Returns -1 when it cannot be read.
//
// This is the measurement the whole sleep path is judged by. systemd-sleep reads
// this attribute and writes the value back immediately before writing
// /sys/power/state; a wakeup event registered in between makes the write fail and
// the suspend abort with pm_wakeup_pending() true — which on this machine shows up
// as a 21µs "PM: suspend entry" / "PM: suspend exit" pair with no "Freezing" line
// in between. Sampling the count either side of our own writes is what turns "the
// EC probably objects to this" into something observed rather than assumed.
//
// It is readable by an unprivileged user, so no part of this needs root.
func wakeupCount() int {
	return readPowerInt("wakeup_count")
}

// wakeupIRQ reads /sys/power/pm_wakeup_irq, the IRQ that caused the last wakeup.
// 9 is the ACPI SCI on this machine, i.e. the embedded controller. Returns -1 when
// unset or unreadable — it is empty until something has actually woken the system.
func wakeupIRQ() int {
	return readPowerInt("pm_wakeup_irq")
}

func readPowerInt(name string) int {
	data, err := os.ReadFile(sysPowerDir + "/" + name)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return n
}

// sleepSettleDelay is how long releaseVolatileState waits after its last write
// before releasing the logind delay lock.
//
// The ppt_* and pwm_enable writes are ACPI/WMI calls, and the suspicion this delay
// tests is that the EC answers them with a notification some hundreds of
// milliseconds later. Released immediately, that notification lands between
// systemd reading /sys/power/wakeup_count and writing /sys/power/state, where it
// aborts the suspend. Waiting here spends part of logind's InhibitDelayMaxSec
// budget (5s by default) to have the event delivered and counted while the system
// is still awake.
//
// A var so tests can zero it — no test should ever sleep — and so
// Z13CTL_SLEEP_SETTLE_MS can override it while the mechanism is being pinned down.
var sleepSettleDelay = 400 * time.Millisecond

// sleepSteps selects which pre-sleep writes run. It exists to isolate which write
// disturbs the EC and should be deleted once that is known; Z13CTL_SLEEP_STEPS
// sets it. Both fields default true.
type sleepSteps struct {
	PPT  bool
	Fans bool
}

// parseSleepSteps reads the Z13CTL_SLEEP_STEPS diagnostic knob: a comma-separated
// list of "ppt" and "fans", or "none". An empty or unrecognised value keeps both
// steps enabled — a typo must not silently turn the fan release off, which would
// look exactly like the bug it was set to investigate. The second return reports
// whether the value was understood, so the caller can say so.
func parseSleepSteps(v string) (sleepSteps, bool) {
	all := sleepSteps{PPT: true, Fans: true}
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return all, true
	}
	if v == "none" {
		return sleepSteps{}, true
	}
	var out sleepSteps
	for _, f := range strings.Split(v, ",") {
		switch strings.TrimSpace(f) {
		case "ppt":
			out.PPT = true
		case "fans":
			out.Fans = true
		default:
			return all, false
		}
	}
	return out, true
}

// applySleepEnv resolves the sleep-path configuration and echoes it, so every
// journal makes plain which variant produced it. Called once from Run, before any
// watcher starts, which is what makes d.sleepSteps safe to read without a lock.
//
// The two environment variables are diagnostic knobs for isolating which pre-sleep
// write disturbs the embedded controller, and should be removed once that is
// settled. --no-sleep-release is the permanent user-facing switch.
func (d *Daemon) applySleepEnv() {
	steps, ok := parseSleepSteps(os.Getenv(envSleepSteps))
	if !ok {
		slog.Warn("unrecognised "+envSleepSteps+"; keeping both pre-sleep steps enabled",
			"value", os.Getenv(envSleepSteps), "valid", "ppt, fans, ppt,fans, none")
	}
	d.sleepSteps = steps

	if v := os.Getenv(envSleepSettleMS); v != "" {
		ms, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || ms < 0 {
			slog.Warn("ignoring invalid "+envSleepSettleMS, "value", v)
		} else {
			sleepSettleDelay = time.Duration(ms) * time.Millisecond
		}
	}

	slog.Info("sleep path configured",
		"release", d.sleepRelease,
		"step_ppt", d.sleepSteps.PPT,
		"step_fans", d.sleepSteps.Fans,
		"settle_ms", sleepSettleDelay.Milliseconds())
}

const (
	envSleepSteps    = "Z13CTL_SLEEP_STEPS"
	envSleepSettleMS = "Z13CTL_SLEEP_SETTLE_MS"
)

// sleepObs is what the sleep hook observes before touching anything.
type sleepObs struct {
	Owned     bool   // daemon state says a non-empty custom profile is active
	CurveMode int    // curve device pwm1_enable; -1 if unreadable
	PL1       int    // effective sustained limit in watts; -1 if unreadable
	Firmware  string // platform_profile underneath, naming the stock PPT row
}

// sleepAction is what a sleep decided to do. A zero value means "leave the
// hardware alone", which is the case for a machine on a firmware profile.
type sleepAction struct {
	LowerPPT    bool
	ReleaseFans bool
	Reason      string
}

// none reports whether the action would touch anything.
func (a sleepAction) none() bool { return !a.LowerPPT && !a.ReleaseFans }

// sleepTick decides what to release from one observation. It is pure — no sysfs,
// no locks, no logging — for the same reason reconcileTick is: internal/cli's
// path vars are unexported, so a daemon test that reached the apply path would
// write the developer's actual fan hardware.
func sleepTick(obs sleepObs) sleepAction {
	// The ownership gate is what keeps sleep and resume symmetric. Owned is the
	// same condition restoreVolatileState restores under, so the invariant holds
	// in both directions: the sleep hook releases only what applyCustomHW will
	// put back. Without it the release is a one-way door — a curve set by asusctl
	// while z13ctl sits on a firmware profile also reads pwm_enable=1, and
	// releasing it (let alone lowering its PPT) leaves nothing on the resume side
	// to restore either. reconcileTick's !obs.Custom gate exists for this reason.
	if !obs.Owned {
		return sleepAction{}
	}

	switch obs.CurveMode {
	case -1:
		// Unreadable: never act on an unknown, as reconcileTick does not.
		return sleepAction{}
	case 0:
		// Someone deliberately forced full speed. We did not write it and have
		// nothing to restore it from, so it is not ours to undo.
		return sleepAction{}
	case 2:
		// Already on firmware auto, which is what the EC needs to stop the fans.
		return sleepAction{}
	}

	act := sleepAction{ReleaseFans: true, Reason: "custom fan curve keeps the fans running through s2idle"}
	if obs.PL1 > cli.TDPMaxSafe {
		// Dropping to firmware auto removes the floor this limit requires, so the
		// limit comes down first. A PL1 of -1 deliberately does not land here: an
		// unreadable PPT must not leave the fans running, the same stance
		// CheckFanCurveFloor takes on a read failure.
		act.LowerPPT = true
		act.Reason = "custom fan curve keeps the fans running through s2idle, and the sustained limit needs the floor lowered first"
	}
	return act
}

// releaseVolatileState hands the fans back to firmware auto before sleep, so the
// EC stops them. See the file comment for why that does not happen on its own.
//
// Daemon state is deliberately left alone: it still describes the profile the
// user selected, which is what restoreVolatileState reapplies and what a --get
// should keep reporting while the machine is asleep. No saveAndNotify either — a
// state-changed event for a transition that reverses itself on resume would only
// make clients redraw twice.
func (d *Daemon) releaseVolatileState() {
	// Before any hardware write: the reconcile watcher polls every 2s and would
	// otherwise re-enable the curve in the window before userspace freezes.
	d.setSuspending(true)

	// hwMu before d.mu, always — this writes the same attributes the socket
	// handlers and the reconcile watcher do.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	active, ok := d.state.ActiveCustomProfile()
	d.mu.Unlock()

	obs := sleepObs{
		Owned:     ok && !active.Empty(),
		CurveMode: -1,
		PL1:       -1,
		Firmware:  readProfileFromSysfs(),
	}
	// d.effectiveProfile() takes d.mu, which is why it is called with only hwMu
	// held.
	if modes, err := cli.ReadFanCurveModes(); err == nil {
		obs.CurveMode = modes[0]
	}
	if tdp, err := cli.ReadEffectivePPT(d.effectiveProfile()); err == nil {
		obs.PL1 = tdp.PL1SPL
	}

	act := sleepTick(obs)
	if act.none() {
		// No writes, so nothing to settle for. A machine on a firmware profile must
		// not pay sleepSettleDelay on every suspend for work that never happened.
		slog.Info("sleep: nothing to release", "owned", obs.Owned, "fan_mode", obs.CurveMode)
		return
	}

	// Each step is bracketed by a wakeup_count sample so the journal says which
	// write, if any, registered a wakeup event. See wakeupCount.
	start := time.Now()
	before := wakeupCount()
	afterPPT := before

	if act.LowerPPT && d.sleepSteps.PPT {
		if err := restoreStockPPTErr(obs.Firmware); err != nil {
			// Fail closed, exactly as ApplyTDPSafely does in the mirror image of
			// this sequence: a loud suspend is the right trade against leaving a
			// sustained limit above TDPMaxSafe with the fans on firmware auto.
			slog.Warn("sleep: not releasing the fans — could not lower the sustained limit first",
				"profile", obs.Firmware, "pl1", obs.PL1, "err", err)
			return
		}
		afterPPT = wakeupCount()
	}

	afterFans := afterPPT
	if d.sleepSteps.Fans {
		if err := cli.ResetAllFanCurves(); err != nil {
			slog.Warn("sleep: failed to release fans to firmware auto", "err", err)
			return
		}
		afterFans = wakeupCount()
	}

	slog.Info("sleep: released fans to firmware auto",
		"reason", act.Reason,
		"steps_ppt", act.LowerPPT && d.sleepSteps.PPT,
		"steps_fans", d.sleepSteps.Fans,
		"wakeup_count_before", before,
		"wakeup_count_after_ppt", afterPPT,
		"wakeup_count_after_fans", afterFans,
		"write_ms", time.Since(start).Milliseconds())

	if sleepSettleDelay > 0 {
		time.Sleep(sleepSettleDelay)
		slog.Info("sleep: settled before releasing the inhibitor",
			"settle_ms", sleepSettleDelay.Milliseconds(),
			"wakeup_count_after_settle", wakeupCount(),
			"total_ms", time.Since(start).Milliseconds())
	}
}

// takeSleepInhibitor holds a logind delay lock so the daemon's pre-sleep writes
// land before userspace is frozen. PrepareForSleep(true) is otherwise advisory:
// logind emits it and proceeds, so on a fast-suspending system the fan release
// may never run — which looks exactly like the bug it fixes.
//
// Best-effort by design. A system where Inhibit is refused behaves as it did
// before this existed, so the failure is logged at Debug and -1 returned.
func takeSleepInhibitor(conn *dbus.Conn) int {
	if !conn.SupportsUnixFDs() {
		slog.Debug("no sleep delay inhibitor: DBus connection does not support file descriptor passing")
		return -1
	}
	var fd dbus.UnixFD
	err := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").Call(
		"org.freedesktop.login1.Manager.Inhibit", 0,
		"sleep", "z13ctl", "releasing custom fan curve before sleep", "delay",
	).Store(&fd)
	if err != nil {
		slog.Debug("could not take a sleep delay inhibitor", "err", err)
		return -1
	}
	slog.Debug("holding a sleep delay inhibitor")
	return int(fd)
}

// releaseSleepInhibitor closes the delay lock and marks it released. logind
// suspends once the last delay lock is gone, so the call site decides when.
func releaseSleepInhibitor(fd *int) {
	if *fd < 0 {
		return
	}
	if err := syscall.Close(*fd); err != nil {
		slog.Debug("failed to close the sleep delay inhibitor", "err", err)
	}
	*fd = -1
}

// restoreVolatileState reapplies all settings that are lost on sleep/resume:
// lighting, fan curves, TDP, and Curve Optimizer offsets.
func (d *Daemon) restoreVolatileState() {
	// Cleared first, and via defer, so every return path below reaches it —
	// including the early one for a machine on a firmware profile. A suspending
	// flag left set would keep the reconcile watcher standing down, which is what
	// its own staleness ceiling is the backstop for.
	defer d.setSuspending(false)

	// hwMu before d.mu: the fan/TDP block below writes the same attributes the
	// socket handlers and the reconcile watcher do.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	// Both d.dev and d.state are guarded by d.mu, and applyLightingState reads
	// them directly, so hold the lock across it — the same discipline the socket
	// handlers use. cloneState is required because the plain struct copy would
	// alias the Devices map and the pointer fields still owned by d.state.
	d.mu.Lock()
	state := cloneState(d.state)
	if d.dev != nil {
		if err := d.applyLightingState(); err != nil {
			slog.Warn("resume: failed to restore lighting", "err", err)
		} else {
			slog.Info("resume: lighting restored")
		}
	}
	d.mu.Unlock()

	// The power source may have changed while the machine was asleep. There is
	// deliberately no autoswitch hook here: Go timers use CLOCK_MONOTONIC and do
	// not advance across suspend, so the power-source watcher's armed timer
	// fires promptly after resume and handles it. Duplicating the logic here
	// would race that watcher over hwMu and apply twice for one event. The cost
	// is a few seconds during which the pre-sleep profile is back in force,
	// which ApplyTDPSafely still keeps within the thermal floor.
	active, ok := state.ActiveCustomProfile()
	if !ok || active.Empty() {
		slog.Info("skipping volatile state restore (no custom profile active)", "profile", state.Profile)
		return
	}

	// Through the same helper as the socket command, the autoswitch watcher and
	// daemon startup. Resume used to carry its own copy of the apply sequence,
	// which is how the four drifted apart in the first place; every fix to the
	// ordering or the clearing rules now lands here too. hwMu is already held,
	// which is what applyCustomHW requires.
	//
	// Bracketed by the same wakeup_count samples as the sleep path, because this is
	// the unexamined half: on a lid-close suspend that immediately wakes, the lid is
	// still shut, so logind suspends again within seconds — and these writes (curve,
	// PPT and the SMU offsets) land shortly before that next attempt. A settle delay
	// on the sleep side alone would not cover an event provoked here.
	slog.Info("resume: restoring custom profile", "profile", active.Name,
		"wakeup_count", wakeupCount(), "wakeup_irq", wakeupIRQ())
	start := time.Now()
	d.applyCustomHW(active)
	slog.Info("resume: custom profile restored", "profile", active.Name,
		"wakeup_count_after", wakeupCount(), "write_ms", time.Since(start).Milliseconds())
}
