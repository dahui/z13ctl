package daemon

// powersource.go — applies a different profile on AC and on battery (issue #6).
//
// The watcher is EDGE-TRIGGERED on the mains adapter's "online" attribute, and
// deliberately never reads or reasons about platform_profile. That is what
// makes it safe to run alongside power-profiles-daemon: it reacts only to a
// value that neither z13ctl nor PPD nor the desktop can write, so the feedback
// loop that would produce a write-fight does not exist. A level-triggered
// "keep my profile applied" watcher would both fight PPD over every AC
// transition — the very thing reconcile.go was written to avoid — and make a
// manual profile change impossible to hold.
//
// The consequence, which is the intended semantics: a profile chosen by hand
// sticks until the power source actually changes. Between transitions z13ctl
// yields. GNOME's "Automatic Power Saver", for instance, is a low-battery
// trigger rather than an unplug trigger, and this watcher correctly ignores it.
//
// Detection has two triggers and one observation. UPower's OnBattery property
// wakes the loop immediately where UPower is running; a 2 s poll is the backstop
// that keeps this working in Steam Gaming Mode, on a bare session, or wherever
// UPower is absent. Either way the answer comes from cli.OnACPower() reading
// sysfs, so there is one code path to reason about and one to test.
//
// An observed edge is confirmed on the following observation before anything is
// applied. That settle window buys two things: the apply lands after PPD's own
// transition write, so a custom fan curve is not dropped in the gap before
// reconcile.go notices, and a loose USB-C connector flapping "online" cannot
// drive a full PPT + fan + SMU write per flap.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

const (
	// powerPollInterval matches watchHotplug and watchReconcile. The read is one
	// small sysfs file, so the cost is negligible.
	powerPollInterval = 2 * time.Second

	// powerQuietAfter is how many consecutive failed applies are logged before
	// the watcher goes quiet, mirroring reconcileQuietAfter.
	powerQuietAfter = 3
)

// powerObs is one observation of the power source and the configuration that
// governs it.
type powerObs struct {
	OnAC    bool // mains adapter online
	Known   bool // false when the source could not be determined
	Auto    *api.AutoswitchState
	Current string // the active profile, for the enable-time no-op check
}

// powerState is the latched watcher state carried between ticks.
type powerState struct {
	known    bool // an observation has been latched
	onAC     bool // last latched source
	enabled  bool // last latched autoswitch enablement
	pending  bool // an edge is waiting for its confirming observation
	pendAC   bool // the source that edge moved to
	failures int
	quiet    bool
}

// powerAction is what a tick decided to do. The zero value means "nothing".
type powerAction struct {
	Profile string
	Reason  string
}

func (a powerAction) none() bool { return a.Profile == "" }

// powerTick decides whether a power-source observation calls for a profile
// change. It is pure — no sysfs, no locks, no logging — because internal/cli's
// path vars are unexported, so a daemon test that reached the apply path would
// rewrite the developer's actual machine.
func powerTick(prev powerState, obs powerObs) (powerState, powerAction) {
	st := prev

	// An unreadable source is unknown, never "on battery". Latching it would
	// have a machine with no mains device — a VM, a desktop, a driver not yet
	// bound — apply the battery profile forever.
	if !obs.Known {
		return st, powerAction{}
	}

	enabled := obs.Auto != nil && obs.Auto.Enabled
	source := obs.OnAC

	// The first observation is latched without acting. Run() owns the startup
	// apply, because it holds the same-value guard that keeps a redundant
	// platform_profile write — and the WMI fan-controller reset that comes with
	// it — out of every daemon restart.
	//
	// Enablement is latched as false regardless of what was observed, so the
	// next tick sees an enable edge and re-checks. Latching the observed value
	// instead swallowed any autoswitch configured in the two seconds before the
	// first tick: the edge had already passed, and nothing applied until a real
	// plug or unplug. The re-check is not a duplicate of Run()'s apply, because
	// the enable-time path skips when the target is already active — which it is
	// whenever Run() has just applied it.
	if !st.known {
		st.known = true
		st.onAC = source
		st.enabled = false
		return st, powerAction{}
	}

	sourceChanged := source != st.onAC
	// Enabling autoswitch while running is a deliberate one-shot: apply the
	// target for the source we are already on.
	justEnabled := enabled && !st.enabled

	st.enabled = enabled

	// Confirm a source change on the next observation before acting on it.
	if sourceChanged {
		if !st.pending || st.pendAC != source {
			st.pending = true
			st.pendAC = source
			return st, powerAction{}
		}
	}
	confirmed := sourceChanged && st.pending && st.pendAC == source
	st.pending = false

	if confirmed {
		st.onAC = source
	}
	if !confirmed && !justEnabled {
		return st, powerAction{}
	}

	if !enabled {
		return st, powerAction{}
	}
	target := obs.Auto.Target(source)
	if target == "" {
		// This side is unconfigured: hand it to the desktop.
		return st, powerAction{}
	}
	// Only the enable-time one-shot checks for equality. A confirmed source
	// transition applies unconditionally: something else may have moved
	// platform_profile behind our back, and skipping would leave the machine on
	// the wrong profile with no way back until the next unplug.
	if justEnabled && !confirmed && target == obs.Current {
		return st, powerAction{}
	}

	reason := "switched to battery"
	if source {
		reason = "switched to AC"
	}
	if justEnabled && !confirmed {
		reason = "autoswitch enabled"
	}
	return st, powerAction{Profile: target, Reason: reason}
}

// watchPowerSource runs until ctx is done, applying the configured profile on
// each confirmed AC/battery transition.
func (d *Daemon) watchPowerSource(ctx context.Context) {
	nudge := make(chan struct{}, 4)
	go watchUPower(ctx, nudge)

	var st powerState
	for {
		select {
		case <-ctx.Done():
			return
		case <-nudge:
			// UPower says something moved. Give the kernel and PPD a moment to
			// settle before the confirming observation reads sysfs.
		case <-time.After(powerPollInterval):
		}
		st = d.powerSourceOnce(st)
	}
}

// powerSourceOnce performs one observe-decide-apply cycle and returns the new
// latched state. It is the only part of the watcher that touches hardware.
func (d *Daemon) powerSourceOnce(prev powerState) powerState {
	// hwMu before d.mu, always: applyProfileLocked writes the same sysfs
	// attributes the socket handlers do.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	obs := powerObs{Current: d.state.Profile}
	if a := d.state.Autoswitch; a != nil {
		auto := *a
		obs.Auto = &auto
	}
	d.mu.Unlock()

	if onAC, err := cli.OnACPower(); err == nil {
		obs.OnAC = onAC
		obs.Known = true
	}

	st, act := powerTick(prev, obs)
	if act.none() {
		return st
	}

	source := "battery"
	if obs.OnAC {
		source = "AC"
	}
	slog.Info("autoswitch", "source", source, "profile", act.Profile, "reason", act.Reason)

	if err := d.applyProfileLocked(act.Profile); err != nil {
		st.failures++
		if !st.quiet {
			slog.Warn("autoswitch failed to apply profile", "profile", act.Profile, "err", err)
		}
		if st.failures >= powerQuietAfter && !st.quiet {
			slog.Warn("autoswitch keeps failing; further attempts will not be logged", "attempts", st.failures)
			st.quiet = true
		}
		// The source really did change, so the latch stands: retrying every
		// tick against an unwritable sysfs would be a write storm, and
		// reconcile.go independently defends whatever custom settings are live.
		return st
	}
	st.failures = 0
	st.quiet = false
	return st
}

// watchUPower nudges the poll loop whenever UPower reports a change to
// OnBattery. It fails soft in exactly the way watchResume does: log once and
// return, leaving the 2 s poll as the only trigger. Nothing depends on it — it
// only shortens the latency of a transition from up to 4 s to about 2 s.
func watchUPower(ctx context.Context, nudge chan<- struct{}) {
	conn, err := dbus.SystemBus()
	if err != nil {
		slog.Debug("no system DBus for the power source watcher; polling only", "err", err)
		return
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath("/org/freedesktop/UPower"),
	); err != nil {
		slog.Debug("cannot watch UPower for power source changes; polling only", "err", err)
		return
	}
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	slog.Info("power source watcher started (UPower signals plus sysfs polling)")

	for {
		select {
		case <-ctx.Done():
			conn.RemoveSignal(ch)
			return
		case sig := <-ch:
			if sig == nil || len(sig.Body) < 2 {
				continue
			}
			iface, _ := sig.Body[0].(string)
			if iface != "org.freedesktop.UPower" {
				continue
			}
			changed, ok := sig.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}
			if _, ok := changed["OnBattery"]; !ok {
				continue
			}
			select {
			case nudge <- struct{}{}:
			default: // a nudge is already queued; one is as good as many
			}
		}
	}
}

// autoswitchTarget returns the profile the current power source calls for, or
// "" when autoswitch is off, unconfigured for that source, or already active.
//
// Run() uses this to resolve the startup profile before its restore blocks, so
// a machine that was on AC when the daemon stopped and is on battery now lands
// on the battery profile directly instead of applying the AC profile and
// switching moments later.
func autoswitchTarget(s api.State, onAC bool) string {
	target := s.Autoswitch.Target(onAC)
	if target == "" || target == s.Profile {
		return ""
	}
	if cli.IsStockProfile(target) {
		return target
	}
	if !s.IsCustomProfile(target) {
		return ""
	}
	// An empty custom profile has nothing to apply. applyProfileLocked refuses
	// it, so returning it here would have startup claim a profile the watcher
	// would decline to select — and leave hardware describing the old one.
	if s.CustomProfiles[target].Empty() {
		return ""
	}
	return target
}

func (d *Daemon) handleAutoswitch(req request) response {
	ac := strings.ToLower(strings.TrimSpace(req.AC))
	battery := strings.ToLower(strings.TrimSpace(req.Battery))

	d.mu.Lock()
	// Only when enabling. Turning autoswitch off must always be possible, even
	// if a target has gone stale — refusing there would leave it enabled with no
	// way back short of editing the state file.
	//
	// Ordered, not a map range: with both sides wrong, a random iteration order
	// would report a different one each run.
	if req.Enabled {
		for _, side := range []struct{ slot, name string }{{"ac", ac}, {"battery", battery}} {
			if side.name == "" {
				continue
			}
			if !cli.IsStockProfile(side.name) && !d.state.IsCustomProfile(side.name) {
				d.mu.Unlock()
				return response{OK: false, Error: "autoswitch: unknown " + side.slot + " profile " + side.name}
			}
		}
	}
	d.state.Autoswitch = &api.AutoswitchState{Enabled: req.Enabled, AC: ac, Battery: battery}
	s := cloneState(d.state)
	d.mu.Unlock()
	if err := saveState(s); err != nil {
		slog.Warn("failed to save state", "err", err)
	}
	slog.Info("autoswitch", "enabled", req.Enabled, "ac", ac, "battery", battery)
	return response{OK: true}
}

func (d *Daemon) handleAutoswitchGet() response {
	d.mu.Lock()
	a := api.AutoswitchState{}
	if d.state.Autoswitch != nil {
		a = *d.state.Autoswitch
	}
	d.mu.Unlock()

	// Include the live source so a client can render "active on battery"
	// without a second round trip.
	out := struct {
		api.AutoswitchState
		OnAC  bool `json:"on_ac"`
		Known bool `json:"source_known"`
	}{AutoswitchState: a}
	if onAC, err := cli.OnACPower(); err == nil {
		out.OnAC = onAC
		out.Known = true
	}
	data, _ := json.Marshal(out)
	return response{OK: true, Value: string(data)}
}
