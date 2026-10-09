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
//   - PrepareForSleep(false) arrives early in the resume path, before the
//     platform drivers have finished re-initialising. Restoring immediately can
//     drive a PPT write into an embedded controller that is not answering yet,
//     which wedges the ACPI global mutex and hard-locks the machine — hence
//     waitForEC.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
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
	inhibitBudget := inhibitDelayMax(conn)

	// Whether a battery was registered going into the suspend. It is what lets the
	// resume wait read a battery that has vanished outright as a wedged EC rather
	// than a batteryless machine — see ecStatusAfterSleep. Seeded here so a resume
	// signal with no preceding sleep signal still has evidence to go on.
	hadBattery := cli.HasBattery()

	slog.Info("resume watcher started (listening for PrepareForSleep)")

	cycle := &sleepCycle{
		waitEC: waitForEC,
		restore: func(status ecStatus) {
			// Deliberately not skipped: restoreVolatileState is what clears the
			// suspending flag, and skipping it wholesale would stand the
			// reconcile watcher down until its staleness ceiling expired. It also
			// restores lighting, which goes over hidraw and never touches the EC.
			// Only the profile apply at the end has to be held back.
			if status == ecWedged {
				slog.Error("EC unresponsive after resume; restoring lighting only and leaving the "+
					"profile to the reconcile watcher, to keep WMI writes off a stalled ACPI mutex",
					"waited", ecProbeTimeout)
			}
			slog.Info("restoring volatile state")
			d.restoreVolatileState(status)
		},
	}
	// Joined on the way out so a restore cannot outlive Run's cleanup, which
	// closes the HID device it writes to.
	defer cycle.settle()

	// What the kernel had counted when the last suspend was handed to logind;
	// zero until the first one. See wakeup.go.
	var beforeSleep wakeSnapshot

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
				started := time.Now()
				// A restore still waiting for the EC is abandoned rather than
				// left to run into the freeze. See sleepCycle.
				if cycle.settle() {
					slog.Info("resume: suspending again before the EC settled; restore skipped")
				}
				hadBattery = cli.HasBattery()
				// A (true) with no intervening (false) — a retried suspend, an
				// aborted one, or a lost resume signal — leaves us holding nothing.
				// Re-taking is too late to delay *this* suspend, but it restores the
				// invariant for the next one instead of never holding a lock again.
				if inhibitor < 0 {
					slog.Debug("no sleep inhibitor held at PrepareForSleep(true); re-taking for the next cycle")
					inhibitor = takeSleepInhibitor(conn)
				}
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

				wrote := false
				if d.sleepRelease {
					wrote = d.releaseVolatileState()
				} else {
					slog.Info("sleep: fan release disabled (--no-sleep-release); the custom curve stays in force")
				}

				// A suspend that follows a fan release too closely is woken
				// again by the EC within a couple of seconds. See releaseHold.
				if wrote {
					hold := releaseHold(inhibitBudget, time.Since(started))
					before := sciCount()
					select {
					case <-ctx.Done():
					case <-time.After(hold):
					}
					slog.Info("sleep: held the suspend after the fan release", "held", hold,
						"ec_events", sub(sciCount(), before))
				}

				// Taken last, so a wakeup counted from here on happened after
				// everything z13ctl did. How long that took and whether it
				// wrote the EC are what tell an EC answering our own writes
				// apart from a wake z13ctl had no part in.
				beforeSleep = snapshotWakeups()
				slog.Info("sleep: handing the suspend to logind",
					"pre_sleep_work", time.Since(started).Round(time.Millisecond), "wrote_fans_or_ppt", wrote)

				// logind suspends as soon as the last delay lock closes, so this
				// must happen here rather than on the way out of the loop — the
				// whole point is that the writes above have already landed.
				releaseSleepInhibitor(&inhibitor)
				continue
			}
			slog.Info("system resumed from sleep")
			if !beforeSleep.At.IsZero() {
				after := snapshotWakeups()
				diffWakeups(beforeSleep, after, readWakeExtras(after.Failed > beforeSleep.Failed)).log()
				beforeSleep = wakeSnapshot{}
			}
			// Re-taken here rather than after the restore: the EC wait and restore
			// below run for seconds, and logind must not be free to run an unheld
			// suspend cycle in that window — that is the same gap takeSleepInhibitor
			// exists to close. The loop stays free to take the sleep edge meanwhile,
			// so holding the lock delays a re-suspend only by the release itself.
			//
			// Release before re-taking. The sleep branch normally leaves this at -1,
			// but nothing guarantees the two edges alternate — a (false) with no
			// preceding (true) would otherwise overwrite a still-open fd, leaking it
			// and leaving logind counting a delay lock nobody can release.
			releaseSleepInhibitor(&inhibitor)
			inhibitor = takeSleepInhibitor(conn)

			cycle.resumed(ctx, hadBattery)
		}
	}
}

// sleepCycle runs the post-resume EC wait and restore off the signal loop, so a
// PrepareForSleep(true) arriving during it is handled at once (issue #24).
//
// The loop used to run them inline: 3 s of settle, up to 20 s more of probing,
// then the restore — all while holding the delay lock it re-took on resume. A
// suspend requested in that window (the lid closed again, or logind re-suspending
// a machine that woke with its lid shut) sat unread in the channel. logind waits
// InhibitDelayMaxSec, 5 s by default, for a lock and then suspends anyway, so the
// restore's pwm_enable=1 and PPT writes could land at the freeze — leaving the
// custom curve driving the fans through s2idle, which is the very thing the
// pre-sleep release exists to prevent — and the stale sleep signal was handled
// only after the next wake. Now the sleep edge cancels a restore that has not
// started writing and waits for one that has, so the release that follows
// always runs last. A wake that is followed by an immediate re-suspend then
// writes nothing to the EC at all: the fans were released before the previous
// suspend and nothing put the curve back.
//
// The suspending flag stays set across a skipped restore, exactly as it is
// between any two sleep signals; the next restore clears it, and the reconcile
// watcher's tick-counted ceiling covers a resume signal that never comes.
//
// It is used only from the watchResume goroutine and needs no lock of its own.
type sleepCycle struct {
	waitEC  func(ctx context.Context, hadBattery bool) (ecStatus, bool)
	restore func(ecStatus)

	cancel context.CancelFunc
	done   chan bool // receives whether the restore ran
}

// resumed starts the wait-and-restore for one resume, first settling any
// earlier one still pending — two resume signals with no sleep between them.
func (c *sleepCycle) resumed(ctx context.Context, hadBattery bool) {
	c.settle()
	wctx, cancel := context.WithCancel(ctx)
	done := make(chan bool, 1)
	c.cancel, c.done = cancel, done
	go func() {
		status, ok := c.waitEC(wctx, hadBattery)
		if !ok {
			done <- false
			return
		}
		// Not cancellable once started: the restore takes hwMu, and a sleep
		// edge waiting in settle then releases whatever it wrote.
		c.restore(status)
		done <- true
	}()
}

// settle cancels a pending restore and waits for it to finish. It reports
// whether a restore was skipped, as opposed to having run or there being none.
func (c *sleepCycle) settle() (skipped bool) {
	if c.done == nil {
		return false
	}
	c.cancel()
	ran := <-c.done
	c.cancel, c.done = nil, nil
	return !ran
}

// sleepObs is what the sleep hook observes before touching anything.
type sleepObs struct {
	ECWedged  bool   // the last resume left the EC unresponsive; see Daemon.ecWedged
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
	// Both releases are asus-wmi writes, and an EC that has not answered since
	// the last resume is the one place such a write hard-locks the machine. A
	// curve left running through this suspend is a noisy night; a write into a
	// stalled EC is a power-button reset. Returning no action also keeps
	// d.suspending unarmed, since it is armed only once something is released.
	if obs.ECWedged {
		return sleepAction{}
	}

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
	if obs.PL1 == -1 || obs.PL1 > cli.TDPMaxSafe {
		// Dropping to firmware auto removes the floor a high limit requires, so the
		// limit comes down first.
		//
		// An unreadable PL1 lands here too, which is a change: it used to release the
		// fans and leave the limit alone, on the reasoning that a read failure must
		// not leave the fans running. That is the one place in this codebase that
		// failed *open* — an unreadable PPT could be 93W, and releasing the fans then
		// is precisely what ApplyTDPSafely refuses to do and what CheckFanFloorRelease
		// refuses for a `fancurve --reset`. Lowering first costs nothing when the
		// limit was already safe, and the fail-closed path below means a machine whose
		// PPT cannot be read suspends loud rather than unfloored.
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
//
// It reports whether it wrote fan or PPT hardware, for the sleep log: an EC that
// answers those writes during s2idle is one candidate for issue #24's wake loop.
func (d *Daemon) releaseVolatileState() (wrote bool) {
	// hwMu before d.mu, always — this writes the same attributes the socket
	// handlers and the reconcile watcher do.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	active, ok := d.state.ActiveCustomProfile()
	wedged := d.ecWedged
	d.mu.Unlock()

	if wedged {
		slog.Warn("sleep: leaving fans and power limits as they are; the EC has not answered since the last resume")
	}

	obs := sleepObs{
		ECWedged:  wedged,
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
	// Not while wedged: sleepTick stands down then anyway, and on asus-armoury a
	// PPT read evaluates the AC adapter's _PSR — a live ACPI call into the EC.
	if !wedged {
		if tdp, err := cli.ReadEffectivePPT(d.effectiveProfile()); err == nil {
			obs.PL1 = tdp.PL1SPL
		}
	}

	act := sleepTick(obs)
	if act.none() {
		slog.Debug("sleep: nothing to release", "owned", obs.Owned, "fan_mode", obs.CurveMode)
		return false
	}

	// Armed only now, and only because something is about to be written. Arming it
	// unconditionally at the top stood both watchers down for suspends that release
	// nothing — a machine on a firmware profile, or one already on firmware auto —
	// and a stand-down is not free: the power-source watcher skips a tick while it
	// is set, and a lost resume signal leaves it set for the whole staleness
	// ceiling. Setting it here is still early enough, because hwMu is held from
	// above until after the writes land, and every watcher that could undo them
	// needs hwMu to do so.
	d.setSuspending(true)

	if act.LowerPPT {
		if err := lowerLimitForRelease(obs.Firmware); err != nil {
			// Fail closed, exactly as ApplyTDPSafely does in the mirror image of
			// this sequence: a loud suspend is the right trade against leaving a
			// sustained limit above TDPMaxSafe with the fans on firmware auto.
			slog.Warn("sleep: not releasing the fans — could not lower the sustained limit first",
				"profile", obs.Firmware, "pl1", obs.PL1, "err", err)
			return true
		}
	}

	// nil: the machine is going to sleep, and the resume runs applyCustomHW anyway.
	if err := cli.ReleaseFans(nil); err != nil {
		slog.Warn("sleep: failed to release fans to firmware auto", "err", err)
		return true
	}
	slog.Info("sleep: released fans to firmware auto", "reason", act.Reason)
	return true
}

// lowerLimitForRelease brings the sustained limit down far enough that releasing
// the fans to firmware auto is safe, so that the release can go ahead.
//
// The stock row for the firmware profile underneath is the preferred landing
// place, since that is where the machine belongs while asleep. But
// restoreStockPPTErr fails for any profile absent from cli.StockProfilePPT —
// including the empty string a platform_profile read failure returns — and simply
// giving up there abandoned the fan release and reintroduced the all-night-fans
// bug on an error path. Clamping PL1 to TDPMaxSafe is enough to satisfy the guard,
// leaves every other limit alone, and is undone on resume by applyCustomHW
// rewriting the profile's own TDP.
func lowerLimitForRelease(firmware string) error {
	stockErr := restoreStockPPTErr(firmware)
	if stockErr == nil {
		return nil
	}
	slog.Debug("sleep: no stock PPT row; clamping the sustained limit instead",
		"profile", firmware, "err", stockErr)

	cur, err := cli.ReadAllPPT()
	if err != nil {
		return fmt.Errorf("reading current PPT to clamp it: %w", err)
	}
	if cur.PL1SPL <= cli.TDPMaxSafe {
		return nil // already low enough; the guard is satisfied
	}
	cur.PL1SPL = cli.TDPMaxSafe
	if err := cli.SetTDPState(cur); err != nil {
		return fmt.Errorf("clamping the sustained limit to %dW: %w", cli.TDPMaxSafe, err)
	}
	slog.Info("sleep: clamped the sustained limit so the fans could be released",
		"pl1", cli.TDPMaxSafe, "profile", firmware)
	return nil
}

// releaseSettle is how long a suspend is held after the pre-sleep fan release
// (issue #24). Every pwm_enable=2 write makes the firmware re-apply its thermal
// policy, and a suspend that starts within a moment of it is often woken again
// by the EC: 1–2.5 s into s2idle the EC's SCI and IRQ 1 fire together and the
// kernel logs "Wakeup after ACPI Notify sync", after about 0.2 s of hardware
// sleep. On a lid-closed machine logind suspends it again, the resume has put
// the curve back by then, and the next release wakes it again — the sleep/wake
// cycling in #24. Measured 2026-10-09 on the GZ302EA, RTC-timed 20 s suspends:
// 7 of 15 woke early with the suspend handed over straight after the release,
// 4 of 6 with a 300 ms wait for the EC's event burst to finish, 0 of 7 with a
// 3 s hold, and 0 of 10 with no release at all. The burst itself is over within
// about 230 ms while awake, so this is not waiting it out; it is keeping the
// suspend away from the policy change. Paid only on a suspend that released
// something.
const releaseSettle = 3 * time.Second

// inhibitMargin is left unused of logind's delay budget, so the inhibitor is
// closed before logind gives up on it and suspends regardless.
const inhibitMargin = 500 * time.Millisecond

// defaultInhibitDelay is logind's InhibitDelayMaxSec default, assumed when the
// property cannot be read.
const defaultInhibitDelay = 5 * time.Second

// releaseHold is how long to hold the suspend after a release: releaseSettle,
// cut short so that the pre-sleep work already done plus the hold stays inside
// logind's delay budget. logind suspends once the budget runs out whether or not
// the lock is closed, and a hold that ran past it would leave the snapshot and
// the log line below racing the freeze. Pure, so the arithmetic is tested.
func releaseHold(budget, used time.Duration) time.Duration {
	hold := min(releaseSettle, budget-inhibitMargin-used)
	return max(hold, 0)
}

// inhibitDelayMax reads logind's InhibitDelayMaxUSec, the longest it waits for
// a delay lock. Distributions and users do lower it, so the hold must not
// assume the 5 s default.
func inhibitDelayMax(conn *dbus.Conn) time.Duration {
	v, err := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		GetProperty("org.freedesktop.login1.Manager.InhibitDelayMaxUSec")
	if err != nil {
		slog.Debug("could not read logind's InhibitDelayMaxUSec; assuming the default", "err", err)
		return defaultInhibitDelay
	}
	us, ok := v.Value().(uint64)
	if !ok {
		return defaultInhibitDelay
	}
	return time.Duration(us) * time.Microsecond
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

// EC settling after resume. logind emits PrepareForSleep(false) as soon as the
// kernel returns from suspend, which is before the ASUS platform drivers have
// finished re-initialising. Writing a PPT limit through asus_wmi in that window
// blocks inside acpi_evaluate_object holding the ACPI global mutex; because that
// mutex serialises every ACPI operation on the system, the EC event handler and
// anything else touching thermals or battery pile up behind it and the NMI
// watchdog starts reporting hard lockups across every core.
const (
	// ecSettleDelay is the unconditional pause before the EC is first probed.
	ecSettleDelay = 3 * time.Second
	// ecProbeInterval is how often the EC is probed after that pause.
	ecProbeInterval = 1 * time.Second
	// ecProbeTimeout bounds the total wait before restoring regardless.
	ecProbeTimeout = 20 * time.Second
)

// ecStatus is what one probe of the EC found. A plain bool could not separate
// the last two cases, and they need opposite handling on timeout: an absent
// battery must still be restored, a wedged EC must not be written to at all.
type ecStatus int

const (
	// ecReady means the probe read succeeded, so the EC is answering.
	ecReady ecStatus = iota
	// ecAbsent means the attribute does not exist. A machine with no battery at
	// all — bench supply, pack removed — reports this way, and nothing about
	// waiting longer will change it.
	ecAbsent
	// ecWedged means the attribute exists but the read failed, in practice with
	// ENODEV: the power_supply device is registered while its EC is not
	// answering. This is the state that must not be written to.
	ecWedged
)

// probeEC reports what the EC looks like right now. Reading the battery capacity
// is the cheapest available signal: while the EC is still coming up the sysfs
// attribute is present but its device is not, so the read fails fast with
// ENODEV rather than blocking on the ACPI mutex this whole function exists to
// stay off.
//
// The ENOENT/ENODEV split is what makes the timeout decision possible.
// FindBatteryCapacityPath globs for BAT*/capacity and falls back to a BAT0 path
// that will not exist, so a machine with no battery fails the read with ENOENT
// while a wedged EC fails an existing path with ENODEV.
func probeEC() ecStatus {
	_, err := os.ReadFile(cli.FindBatteryCapacityPath())
	return classifyECRead(err)
}

// classifyECRead maps the probe read's error to a status. Split out from probeEC
// so the ENOENT/ENODEV distinction — the one thing this fix turns on — can be
// tested without redirecting sysfs, which internal/daemon cannot do because
// cli's path vars are unexported.
func classifyECRead(err error) ecStatus {
	switch {
	case err == nil:
		return ecReady
	case errors.Is(err, fs.ErrNotExist):
		return ecAbsent
	default:
		return ecWedged
	}
}

// ecStatusAfterSleep corrects a probe result with what was true before the
// suspend. ENOENT means "no battery" only on a machine that never had one: if the
// kernel unregisters the power_supply device when its EC dies, the attribute is
// gone rather than failing, and classifyECRead alone would wave the restore
// through into the very EC it exists to protect. The Z13's battery is internal,
// so on this hardware a battery that was there before sleep and is missing after
// it is a wedge, not a removal.
func ecStatusAfterSleep(s ecStatus, hadBattery bool) ecStatus {
	if s == ecAbsent && hadBattery {
		return ecWedged
	}
	return s
}

// waitForEC blocks until the EC answers, ctx is cancelled, or ecProbeTimeout
// elapses. The bool is false only on cancellation. hadBattery is whether a
// battery was registered going into the suspend; see ecStatusAfterSleep.
func waitForEC(ctx context.Context, hadBattery bool) (ecStatus, bool) {
	probe := func() ecStatus { return ecStatusAfterSleep(probeEC(), hadBattery) }
	return waitForECWith(ctx, probe, ecSettleDelay, ecProbeInterval, ecProbeTimeout)
}

// waitForECWith is waitForEC with its probe and timings injected, so the policy
// can be tested without real sysfs or real delays.
//
// On timeout it returns what the last probe actually saw rather than a blanket
// "go ahead". ecAbsent still restores: a batteryless machine has nothing to wait
// for and must not be stranded on whatever the firmware left behind. ecWedged
// does not, and the caller restores only what does not touch the EC — driving a
// WMI write into an EC that is not answering is what wedges the ACPI global
// mutex and hard-locks every core, which is the whole reason this wait exists.
func waitForECWith(ctx context.Context, probe func() ecStatus, settle, interval, timeout time.Duration) (ecStatus, bool) {
	select {
	case <-ctx.Done():
		return ecWedged, false
	case <-time.After(settle):
	}

	deadline := time.Now().Add(timeout)
	for {
		status := probe()
		if status == ecReady {
			return ecReady, true
		}
		if time.Now().After(deadline) {
			return status, true
		}
		select {
		case <-ctx.Done():
			return ecWedged, false
		case <-time.After(interval):
		}
	}
}

// restoreVolatileState reapplies all settings that are lost on sleep/resume:
// lighting, fan curves, TDP, and Curve Optimizer offsets.
//
// ec is what the resume wait last saw. Everything up to the profile apply is
// safe in any state — lighting is hidraw and the suspending flag must be cleared
// on every path — so only the closing applyCustomHW is gated on it.
func (d *Daemon) restoreVolatileState(ec ecStatus) {
	// hwMu before d.mu: the fan/TDP block below writes the same attributes the
	// socket handlers and the reconcile watcher do.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	// Registered *after* the hwMu defer so LIFO runs it first — while the lock is
	// still held. Clearing the flag after releasing hwMu left a window in which a
	// watcher already blocked on the lock acquired it and still saw a suspend in
	// progress, standing itself down for a machine that had finished resuming.
	// Every return path below reaches this, including the early one for a firmware
	// profile.
	defer d.setSuspending(false)

	// Both d.dev and d.state are guarded by d.mu, and applyLightingState reads
	// them directly, so hold the lock across it — the same discipline the socket
	// handlers use. cloneState is required because the plain struct copy would
	// alias the Devices map and the pointer fields still owned by d.state.
	d.mu.Lock()
	// Latched here, under hwMu, so no watcher can run between the wait's verdict
	// and the latch: the reconcile and power-source watchers both take hwMu
	// first. A later resume that finds the EC answering clears it again.
	d.ecWedged = ec == ecWedged
	state := cloneState(d.state)
	if d.dev != nil {
		// USB re-enumerates across suspend, so the handle opened at startup (or
		// at the last reattach) usually points at a hidraw node that no longer
		// exists — the write then fails with ENODEV and the lighting is never
		// restored. The hotplug watcher does not cover this: it fires on an
		// absent -> present transition, and the keyboard comes back faster than
		// its 2 s poll can observe it missing, so the stale handle is never
		// replaced.
		//
		// A failed reopen leaves d.dev as it was, which is exactly the previous
		// behaviour, so this can only improve on it.
		if err := d.reopenDeviceLocked(); err != nil {
			slog.Warn("resume: failed to reopen HID device", "err", err)
		} else if err := d.applyLightingState(); err != nil {
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

	// applyCustomHW writes fan curves and PPT through asus-wmi, so it is exactly
	// the call that must not run against an EC that is not answering. It is not
	// lost: the reconcile watcher keeps probing while d.ecWedged is set, and runs
	// this same apply once the EC reads cleanly. Until then every watcher that
	// writes fan or PPT hardware stands down on the latch set above — the curve
	// this skips would otherwise be "repaired" by reconcile two seconds later,
	// into the same stalled EC.
	if ec == ecWedged {
		slog.Error("skipping custom profile restore: the EC is not answering",
			"profile", active.Name)
		return
	}

	// Through the same helper as the socket command, the autoswitch watcher and
	// daemon startup. Resume used to carry its own copy of the apply sequence,
	// which is how the four drifted apart in the first place; every fix to the
	// ordering or the clearing rules now lands here too. hwMu is already held and
	// d.mu is not, which is what applyCustomHW requires.
	slog.Info("resume: restoring custom profile", "profile", active.Name)
	d.applyCustomHW(active)
}
