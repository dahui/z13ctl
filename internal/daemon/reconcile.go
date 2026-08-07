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
//
// It stands down entirely while d.suspending is set. The sleep hook releases the
// fans on purpose — firmware auto is the only mode in which the EC stops them
// through s2idle (see resume.go) — and this watcher would otherwise put the curve
// back within two seconds, in the window before userspace freezes. The stand-down
// expires after reconcileSuspendMaxTicks so a missed resume signal cannot leave a
// live curve undefended forever.

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

// reconcileSuspendMaxTicks is how many consecutive suspending ticks the watcher
// stands down for before deciding the flag is stale and defending the fans
// again. It is a backstop for a PrepareForSleep(false) that never arrives, which
// would otherwise leave the watcher inert for the rest of the daemon's life.
//
// The ceiling is safe because Go timers use CLOCK_MONOTONIC, which does not
// advance across suspend — so these are *awake* ticks, and the only awake window
// inside a suspend is the one logind's delay lock holds open.
//
// It must therefore exceed logind's InhibitDelayMaxSec, or the "stale flag"
// backstop fires inside a real pre-freeze window and re-enables the curve — the
// very thing it exists to prevent. 60 ticks is 120 s of awake time against a
// default InhibitDelayMaxSec of 5 s, with room for the 30 s and 60 s values people
// configure. Only a PrepareForSleep(false) that never arrives can reach it, and
// the cost of the larger margin is that such a daemon defends a live curve two
// minutes later than it otherwise would.
const reconcileSuspendMaxTicks = 60

// reconcileObs is one observation: what daemon state says should be in force,
// and what the hardware currently reports.
type reconcileObs struct {
	Custom     bool                // daemon state says a custom profile is active
	Suspending bool                // the sleep hook has released the fans on purpose
	SuspendGen int                 // increments per suspend; see the gate in reconcileTick
	WantCurve  []api.FanCurvePoint // that profile's curve; nil if none
	WantTDP    *api.TDPState       // that profile's TDP; nil if none
	CurveMode  int                 // curve device pwm1_enable; -1 if unreadable
	PL1        int                 // effective sustained limit in watts; -1 if unreadable
	ProfileHW  string              // platform_profile, for logging only
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
	failures       int    // consecutive unsuccessful restorations
	quiet          bool   // stop logging repeated failures
	lastHW         string // last observed platform_profile
	suspendedTicks int    // ticks observed during the current suspend
	suspendGen     int    // the suspend suspendedTicks is counting for
}

// reconcileTick decides what to restore from one observation. It is pure — no
// sysfs, no locks, no logging — so every branch below is unit-testable without
// hardware, which is the only way daemon-side logic can be tested in this
// project (internal/cli's path vars are unexported, so a daemon test that
// reaches hardware writes the developer's actual machine).
func reconcileTick(prev reconcileState, obs reconcileObs) (reconcileState, reconcileAction) {
	st := prev
	st.lastHW = obs.ProfileHW

	// The sleep hook released the fans deliberately so the EC can stop them
	// through s2idle; re-enabling the curve here would undo it in the window
	// before userspace freezes. Falling through past the ceiling is the backstop
	// for a resume signal that never arrived — see reconcileSuspendMaxTicks.
	//
	// The budget is per-suspend, and obs.SuspendGen is what makes that true. A
	// machine that flaps sleep→wake→sleep faster than the 2s poll never presents an
	// idle tick to reset on, so counting alone let the budget accumulate across
	// suspends until it expired *inside* a real pre-freeze window and the watcher
	// undid the release.
	if obs.SuspendGen != st.suspendGen {
		st.suspendGen = obs.SuspendGen
		st.suspendedTicks = 0
	}
	if obs.Suspending {
		st.suspendedTicks++
		if st.suspendedTicks < reconcileSuspendMaxTicks {
			return st, reconcileAction{}
		}
	}
	st.suspendedTicks = 0

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

	switch obs.CurveMode {
	case -1:
		// Unreadable: never act on an unknown.
	case 1:
		// Curve is live.
	case 0:
		// Someone deliberately forced full speed. That is more cooling than
		// any curve we would write, so leave it.
	default:
		// The saved curve is restored as drawn, except that any point below the
		// high-TDP floor is raised to it — cli.FanCurveForTDP, the same rule the
		// apply path uses. The clamp is needed because CheckFanCurveFloor only vets
		// a curve against the limit in force when the curve is *saved*: raising the
		// TDP afterwards leaves sub-floor points in state, and handleTDP deliberately
		// keeps them for when the limit comes back down. With no saved curve at all
		// FanCurveForTDP yields HighTDPFanCurve, which is the other half of
		// ApplyTDPSafely's fail-closed guard — the floor was written once and could
		// then be dropped while the PPT stayed high.
		want := obs.WantCurve
		if c := cli.FanCurveForTDP(obs.PL1, obs.WantCurve); c != nil {
			want = c
		}
		// Guarded: with no saved curve and a safe limit there is nothing to put
		// back, and want stays nil. Setting act.Reason unconditionally here made a
		// PPT-only reconcile on a curveless profile log "high-TDP fan floor was
		// released while the limit is still in force" for a machine at 52W, because
		// the TDP arm below only fills in a reason when one is not already set.
		if want != nil {
			act.Curve = want
			switch {
			case obs.WantCurve == nil:
				act.Reason = "high-TDP fan floor was released while the limit is still in force"
			case cli.FloorAdjustsCurve(obs.PL1, obs.WantCurve):
				act.Reason = "fan curve was disabled and its sub-floor points were raised to the high-TDP floor"
			default:
				act.Reason = "saved custom fan curve was disabled"
			}
		}
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

// reconcileCurveFor returns the curve to write once the tick's TDP arm has run,
// or nil when the fans should be left exactly where the observation found them.
//
// It is evaluated against floorLimit — the limit now in force — rather than the
// one observed at the top of the tick, because a reconcile that lowers a drifted
// PPT changes what the floor requires. Three outcomes:
//
//   - the profile's own curve, raised where the limit still demands it
//   - the built-in HighTDPFanCurve, when the limit demands a floor and the
//     profile has no curve to raise
//   - nil, when the limit demands no floor and the profile has no curve — there
//     is nothing to put back
//
// That third case is the one this function exists for. reconcileOnce used to fall
// back to the tick's own act.Curve, which was computed against the *drifted*
// limit: a curveless profile whose PPT had wandered above TDPMaxSafe produced
// both arms, the TDP arm brought the limit back down to the profile's own 52 W,
// and the fallback then wrote the high-TDP ramp regardless — pinning both fans to
// a 50 % minimum on a profile that controls no fan curve. It stuck, because the
// following tick read pwm_enable=1 as "the curve is live" and the TDP now matched,
// so nothing corrected it. Pure, so the table in reconcile_test.go can cover it
// without touching the developer's fan controller.
func reconcileCurveFor(floorLimit int, want []api.FanCurvePoint) []api.FanCurvePoint {
	if c := cli.FanCurveForTDP(floorLimit, want); c != nil {
		return c
	}
	return want
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
	suspending, suspendGen := d.suspending, d.suspendGen
	d.mu.Unlock()

	obs := reconcileObs{
		Suspending: suspending,
		SuspendGen: suspendGen,
		CurveMode:  -1,
		PL1:        -1,
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

	// The tick decided the suspending flag is stale — a PrepareForSleep(false)
	// that never arrived. Clear it so the flag stops suppressing anything else
	// that consults it, and say so once rather than every 2s.
	if suspending && st.suspendedTicks == 0 {
		slog.Warn("clearing a stale suspending flag: no resume signal arrived",
			"after_ticks", reconcileSuspendMaxTicks)
		d.setSuspending(false)
	}

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

	// The TDP goes first, and the curve is then recomputed against the limit that
	// is now in force rather than the one observed at the top of the tick.
	//
	// Writing act.Curve first was wrong whenever both arms fired: act.Curve was
	// floored against obs.PL1 — the *drifted* limit — so restoring a profile whose
	// own limit is 52W while hardware sat at 90W wrote the 90W ramp and then
	// lowered the limit, leaving a loud curve the profile does not describe. The
	// next tick then read pwm_enable=1 as "the curve is live" and never corrected
	// it. ApplyTDPSafely still raises its own floor before raising power, so
	// ordering it first opens no unfloored window.
	ok := true
	floorLimit := obs.PL1
	if act.TDP != nil {
		// obs.WantCurve, not nil: restoring a drifted PPT must not throw away a
		// live user curve that already satisfies the floor. Passing nil here was
		// the same defect as on the apply path, reached from the other direction.
		if err := cli.ApplyTDPSafely(*act.TDP, obs.WantCurve); err != nil {
			ok = false
			if !st.quiet {
				slog.Warn("failed to re-apply TDP", "err", err)
			}
		} else {
			floorLimit = act.TDP.PL1SPL
		}
	}
	if act.Curve != nil {
		if curve := reconcileCurveFor(floorLimit, obs.WantCurve); curve == nil {
			slog.Debug("fan floor no longer required and the profile has no curve of its own; leaving the fans alone",
				"pl1", floorLimit)
		} else if err := cli.SetBothFanCurves(curve); err != nil {
			ok = false
			if !st.quiet {
				slog.Warn("failed to re-apply fan curve", "err", err)
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
