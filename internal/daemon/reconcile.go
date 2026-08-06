package daemon

// reconcile.go — puts the custom fan curve (and the high-TDP fan floor) back
// after something else takes it away.
//
// The asus-wmi driver disables custom fan curves on every platform_profile
// write: throttle_thermal_policy_write() ends by clearing
// custom_fan_curves[*].enabled for each fan, and fan_curve_write() then returns
// early on !enabled. Nothing is reported to the process that set the curve. On
// a GNOME desktop, power-profiles-daemon writes platform_profile on every
// AC/battery transition and on any PPD hold, so a curve set by z13ctl stops
// working minutes later for no visible reason — issue #15. Fn+F5, asusctl,
// tuned and a module reload all do the same thing.
//
// The watcher polls the curve device's pwm_enable rather than
// platform_profile: fan_curve_enable_show() returns the driver's cached
// enabled flag, so it is ground truth for "is the curve live" and it catches
// every cause rather than only the one we predicted. platform_profile is read
// in the same tick for the log line alone.
//
// It never writes platform_profile. Profile ownership stays with the desktop;
// putting z13ctl in a write-fight with power-profiles-daemon over every AC
// transition would be worse than the bug. It also acts only while daemon state
// says the profile is "custom", so a deliberate switch to a stock profile is
// left alone by construction.

import (
	"context"
	"log/slog"
	"time"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

// reconcilePollInterval is how often watchReconcile compares hardware against
// the daemon's saved custom state. Both reads it makes are served from driver
// caches (no WMI call), so the cost is negligible; 2 s matches watchHotplug.
const reconcilePollInterval = 2 * time.Second

// reconcileQuietAfter is how many consecutive failed restorations are logged
// before the watcher goes quiet. A restore that can never succeed — revoked
// permissions, an unbound driver — would otherwise fill the journal forever.
const reconcileQuietAfter = 3

// reconcileObs is one observation: what daemon state says should be in force,
// and what the hardware currently reports.
type reconcileObs struct {
	Custom    bool                // daemon state says a custom profile is active
	WantCurve []api.FanCurvePoint // that profile's curve; nil if none
	WantTDP   *api.TDPState       // that profile's TDP; nil if none
	CurveMode int                 // curve device pwm1_enable; -1 if unreadable
	PL1       int                 // effective sustained limit in watts; -1 if unreadable
	ProfileHW string              // platform_profile, for logging only
}

// reconcileAction is what a tick decided to put back. A zero value means
// "leave the hardware alone", which is the common case.
type reconcileAction struct {
	Curve  []api.FanCurvePoint
	TDP    *api.TDPState
	Reason string
}

// none reports whether the action would touch anything.
func (a reconcileAction) none() bool { return a.Curve == nil && a.TDP == nil }

// reconcileState is the latched watcher state carried between ticks.
type reconcileState struct {
	failures int    // consecutive unsuccessful restorations
	quiet    bool   // stop logging repeated failures
	lastHW   string // last observed platform_profile
}

// reconcileTick decides what to restore from one observation. It is pure — no
// sysfs, no locks, no logging — so every branch below is unit-testable without
// hardware, which is the only way daemon-side logic can be tested in this
// project (internal/cli's path vars are unexported, so a daemon test that
// reaches hardware writes the developer's actual machine).
func reconcileTick(prev reconcileState, obs reconcileObs) (reconcileState, reconcileAction) {
	st := prev
	st.lastHW = obs.ProfileHW

	// Only a custom profile means the daemon is claiming ownership of fans and
	// PPT. Every route to a stock profile — handleProfile, handleTDPReset,
	// handleFanCurveReset — updates state first, so this is what keeps the
	// watcher from fighting a legitimate profile switch. A firmware profile name
	// is never custom even if a profile of that name somehow reached the map,
	// so this cannot be subverted by a hand-edited state file.
	if !obs.Custom {
		st.failures = 0
		st.quiet = false
		return st, reconcileAction{}
	}

	var act reconcileAction

	switch {
	case obs.CurveMode == -1:
		// Unreadable: never act on an unknown.
	case obs.CurveMode == 1:
		// Curve is live.
	case obs.CurveMode == 0:
		// Someone deliberately forced full speed. That is more cooling than
		// any curve we would write, so leave it.
	case obs.WantCurve != nil && obs.PL1 > cli.TDPMaxSafe && curveBelowFloor(obs.WantCurve):
		// The saved curve is not always safe to restore. CheckFanCurveFloor
		// only vets a curve against the limit in force when it is *saved*, and
		// raising the TDP afterwards does not rewrite it — handleTDP applies
		// HighTDPFanCurve to hardware but deliberately keeps the user's curve
		// in state for when the limit comes back down. Restoring it here would
		// undo the floor at exactly the moment it is needed.
		act.Curve = cli.HighTDPFanCurve()
		act.Reason = "fan curve was disabled and the saved curve is below the high-TDP floor"
	case obs.WantCurve != nil:
		act.Curve = obs.WantCurve
		act.Reason = "saved custom fan curve was disabled"
	case obs.PL1 > cli.TDPMaxSafe:
		// No saved curve, but the sustained limit still requires the floor.
		// This is the half of ApplyTDPSafely's fail-closed guard that was
		// missing: the floor was written once and could then be silently
		// dropped while the PPT stayed high.
		act.Curve = cli.HighTDPFanCurve()
		act.Reason = "high-TDP fan floor was released while the limit is still in force"
	}

	// PPT is defended against third parties (asusctl, ryzenadj), not against
	// the kernel: a platform_profile write does not reset the ppt_* values.
	if obs.WantTDP != nil && obs.PL1 != -1 && obs.PL1 != obs.WantTDP.PL1SPL {
		act.TDP = obs.WantTDP
		if act.Reason == "" {
			act.Reason = "sustained TDP no longer matches the saved custom value"
		}
	}

	if act.none() {
		st.failures = 0
		st.quiet = false
	}
	return st, act
}

// curveBelowFloor reports whether any point sits under the PWM floor that a
// sustained limit above cli.TDPMaxSafe requires.
func curveBelowFloor(points []api.FanCurvePoint) bool {
	for _, p := range points {
		if p.PWM < cli.HighTDPMinPWM {
			return true
		}
	}
	return false
}

// watchReconcile runs until ctx is done, restoring the custom fan curve and
// high-TDP floor whenever something else removes them.
func (d *Daemon) watchReconcile(ctx context.Context) {
	var st reconcileState
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconcilePollInterval):
		}
		st = d.reconcileOnce(st)
	}
}

// reconcileOnce performs one observe-decide-apply cycle and returns the new
// latched state. It is the only part of the watcher that touches hardware.
func (d *Daemon) reconcileOnce(prev reconcileState) reconcileState {
	// hwMu before d.mu, always: this sequence writes the same sysfs attributes
	// the socket handlers do, and interleaving with them would leave the fans
	// in whichever mode happened to be written last.
	d.hwMu.Lock()
	defer d.hwMu.Unlock()

	d.mu.Lock()
	s := cloneState(d.state) // must clone: FanCurve.Points and TDP alias live state
	d.mu.Unlock()

	obs := reconcileObs{
		CurveMode: -1,
		PL1:       -1,
	}
	if active, ok := s.ActiveCustomProfile(); ok {
		obs.Custom = true
		if fc := active.FanCurve; fc != nil && fc.Mode == 1 && len(fc.Points) == 8 {
			obs.WantCurve = fc.Points
		}
		obs.WantTDP = active.TDP
	}

	// Cheap enough to read unconditionally, and reading them even when the
	// profile is stock keeps lastHW meaningful for the log line.
	if modes, err := cli.ReadFanCurveModes(); err == nil {
		obs.CurveMode = modes[0]
	}
	if tdp, err := cli.ReadEffectivePPT(d.effectiveProfile()); err == nil {
		obs.PL1 = tdp.PL1SPL
	}
	obs.ProfileHW = readProfileFromSysfs()

	st, act := reconcileTick(prev, obs)
	if act.none() {
		return st
	}

	logf := slog.Warn
	if st.quiet {
		logf = slog.Debug
	}
	logf("reconciling custom thermal settings",
		"reason", act.Reason,
		"platform_profile", obs.ProfileHW,
		"pwm_enable", obs.CurveMode)

	ok := true
	if act.Curve != nil {
		if err := cli.SetBothFanCurves(act.Curve); err != nil {
			ok = false
			if !st.quiet {
				slog.Warn("failed to re-apply fan curve", "err", err)
			}
		}
	}
	if act.TDP != nil {
		if err := cli.ApplyTDPSafely(*act.TDP); err != nil {
			ok = false
			if !st.quiet {
				slog.Warn("failed to re-apply TDP", "err", err)
			}
		}
	}

	if ok {
		st.failures = 0
		st.quiet = false
		return st
	}
	st.failures++
	if st.failures >= reconcileQuietAfter {
		if !st.quiet {
			slog.Warn("reconciliation keeps failing; further attempts will not be logged",
				"attempts", st.failures)
		}
		st.quiet = true
	}
	return st
}
