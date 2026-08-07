package daemon

// powersource_test.go — decision coverage for the AC/battery autoswitch watcher.
//
// Every case goes through the pure powerTick. That is deliberate and not
// negotiable: internal/cli's sysfs path vars are unexported, so a daemon test
// that reached applyProfileLocked would rewrite the developer's actual power
// limits, fan mode, and Curve Optimizer offset.

import (
	"testing"

	"github.com/dahui/z13ctl/api"
)

func auto(enabled bool, ac, battery string) *api.AutoswitchState {
	return &api.AutoswitchState{Enabled: enabled, AC: ac, Battery: battery}
}

// latched returns the state a watcher would hold after settling on a source
// with autoswitch in the given enablement.
func latched(onAC, enabled bool) powerState {
	return powerState{known: true, onAC: onAC, enabled: enabled}
}

func TestPowerTick(t *testing.T) {
	cfg := auto(true, "balanced", "battery-uv")

	tests := []struct {
		name string
		prev powerState
		obs  powerObs
		want string // profile the tick should apply; "" for none
	}{
		{
			name: "unknown source never acts",
			prev: latched(true, true),
			obs:  powerObs{OnAC: false, Known: false, Auto: cfg, Current: "balanced"},
		},
		{
			name: "first observation latches without acting",
			obs:  powerObs{OnAC: false, Known: true, Auto: cfg, Current: "balanced"},
		},
		{
			name: "no change, nothing to do",
			prev: latched(true, true),
			obs:  powerObs{OnAC: true, Known: true, Auto: cfg, Current: "balanced"},
		},
		{
			name: "an unconfirmed transition waits for the settle window",
			prev: latched(true, true),
			obs:  powerObs{OnAC: false, Known: true, Auto: cfg, Current: "balanced"},
		},
		{
			name: "disabled autoswitch tracks the source but does not act",
			prev: powerState{known: true, onAC: true, enabled: false, pending: true, pendAC: false},
			obs:  powerObs{OnAC: false, Known: true, Auto: auto(false, "balanced", "battery-uv"), Current: "balanced"},
		},
		{
			name: "no configuration at all",
			prev: latched(true, false),
			obs:  powerObs{OnAC: false, Known: true, Auto: nil, Current: "balanced"},
		},
		{
			name: "an unconfigured side is left to the desktop",
			prev: powerState{known: true, onAC: true, enabled: true, pending: true, pendAC: false},
			obs:  powerObs{OnAC: false, Known: true, Auto: auto(true, "balanced", ""), Current: "balanced"},
		},
		{
			name: "confirmed unplug applies the battery profile",
			prev: powerState{known: true, onAC: true, enabled: true, pending: true, pendAC: false},
			obs:  powerObs{OnAC: false, Known: true, Auto: cfg, Current: "balanced"},
			want: "battery-uv",
		},
		{
			name: "confirmed replug applies the AC profile",
			prev: powerState{known: true, onAC: false, enabled: true, pending: true, pendAC: true},
			obs:  powerObs{OnAC: true, Known: true, Auto: cfg, Current: "battery-uv"},
			want: "balanced",
		},
		{
			// Something else — PPD, Fn+F5, asusctl — may have moved the profile
			// between transitions. Skipping on equality would leave the machine
			// on the wrong profile until the next unplug.
			name: "a confirmed transition applies even when the target is already active",
			prev: powerState{known: true, onAC: false, enabled: true, pending: true, pendAC: true},
			obs:  powerObs{OnAC: true, Known: true, Auto: cfg, Current: "balanced"},
			want: "balanced",
		},
		{
			name: "enabling while running applies the current source's target",
			prev: latched(false, false),
			obs:  powerObs{OnAC: false, Known: true, Auto: cfg, Current: "balanced"},
			want: "battery-uv",
		},
		{
			name: "enabling is a no-op when the target is already active",
			prev: latched(false, false),
			obs:  powerObs{OnAC: false, Known: true, Auto: cfg, Current: "battery-uv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, act := powerTick(tt.prev, tt.obs)
			if act.Profile != tt.want {
				t.Errorf("powerTick() applied %q, want %q", act.Profile, tt.want)
			}
			if tt.want != "" && act.Reason == "" {
				t.Error("an action carries no reason, so the journal line would say nothing")
			}
		})
	}
}

// TestPowerTickAppliesAutoswitchConfiguredBeforeTheFirstTick covers a window
// the watcher used to swallow: autoswitch configured in the two seconds between
// daemon start and the first observation. Latching the observed enablement meant
// the enable edge had already passed, so nothing applied until a real plug or
// unplug.
func TestPowerTickAppliesAutoswitchConfiguredBeforeTheFirstTick(t *testing.T) {
	cfg := auto(true, "balanced", "battery-uv")
	obs := powerObs{OnAC: false, Known: true, Auto: cfg, Current: "quiet"}

	st, act := powerTick(powerState{}, obs)
	if !act.none() {
		t.Fatalf("acted on the first observation: %+v — Run() owns the startup apply", act)
	}

	_, act = powerTick(st, obs)
	if act.Profile != "battery-uv" {
		t.Errorf("second tick applied %q, want \"battery-uv\"", act.Profile)
	}
}

// TestPowerTickDoesNotRepeatTheStartupApply is the other half: when Run() has
// already put the target in force, the re-check must be a no-op rather than a
// second apply.
func TestPowerTickDoesNotRepeatTheStartupApply(t *testing.T) {
	cfg := auto(true, "balanced", "battery-uv")
	obs := powerObs{OnAC: false, Known: true, Auto: cfg, Current: "battery-uv"}

	st, _ := powerTick(powerState{}, obs)
	if _, act := powerTick(st, obs); !act.none() {
		t.Errorf("re-applied a profile that is already active: %+v", act)
	}
}

// TestPowerTickLatchesThroughADisabledTransition keeps the source latch honest
// while autoswitch is off, so re-enabling later does not read as a transition
// that already happened.
func TestPowerTickLatchesThroughADisabledTransition(t *testing.T) {
	off := auto(false, "balanced", "battery-uv")
	st := latched(true, false)

	st, act := powerTick(st, powerObs{OnAC: false, Known: true, Auto: off, Current: "balanced"})
	if act.Profile != "" {
		t.Fatalf("applied a profile while disabled: %+v", act)
	}
	st, act = powerTick(st, powerObs{OnAC: false, Known: true, Auto: off, Current: "balanced"})
	if act.Profile != "" {
		t.Fatalf("applied a profile while disabled: %+v", act)
	}
	// The transition is still announced: an AC/battery indicator is useful on a
	// machine that never configures autoswitch, which is most of them.
	if !act.SourceChanged {
		t.Error("a confirmed transition was not announced while autoswitch was disabled")
	}
	if st.onAC {
		t.Fatal("the source latch did not follow the transition while disabled")
	}

	// Re-enabling now is a one-shot for the source we are actually on.
	if _, act = powerTick(st, powerObs{OnAC: false, Known: true, Auto: auto(true, "balanced", "battery-uv"), Current: "balanced"}); act.Profile != "battery-uv" {
		t.Errorf("enabling applied %q, want \"battery-uv\"", act.Profile)
	}
}

// TestPowerTickSettleWindow walks a real transition tick by tick. The first
// observation of a new source only arms it; the second applies. That window is
// what lets power-profiles-daemon's own transition write land first, so a
// custom fan curve is not dropped in the gap before reconcile.go notices.
func TestPowerTickSettleWindow(t *testing.T) {
	cfg := auto(true, "balanced", "battery-uv")
	st := latched(true, true)

	st, act := powerTick(st, powerObs{OnAC: false, Known: true, Auto: cfg, Current: "balanced"})
	if !act.none() {
		t.Fatalf("acted on the first observation of a new source: %+v", act)
	}
	if !st.pending {
		t.Fatal("the transition was not armed, so the confirming tick has nothing to confirm")
	}

	st, act = powerTick(st, powerObs{OnAC: false, Known: true, Auto: cfg, Current: "balanced"})
	if act.Profile != "battery-uv" {
		t.Fatalf("the confirming tick applied %q, want \"battery-uv\"", act.Profile)
	}
	if st.onAC {
		t.Error("the new source was not latched, so the next tick would apply again")
	}

	// Settled: no further action without another transition.
	_, act = powerTick(st, powerObs{OnAC: false, Known: true, Auto: cfg, Current: "battery-uv"})
	if !act.none() {
		t.Errorf("acted again with no transition: %+v — this is the level-trigger that would fight PPD", act)
	}
}

// TestPowerTickIgnoresAFlappingCharger is the other half of the settle window.
// A loose USB-C connector that bounces "online" between observations must not
// drive a full PPT + fan + SMU write per bounce.
func TestPowerTickIgnoresAFlappingCharger(t *testing.T) {
	cfg := auto(true, "balanced", "battery-uv")
	st := latched(true, true)

	for i, onAC := range []bool{false, true, false, true} {
		var act powerAction
		st, act = powerTick(st, powerObs{OnAC: onAC, Known: true, Auto: cfg, Current: "balanced"})
		if !act.none() {
			t.Fatalf("flap %d (onAC=%v) triggered an apply: %+v", i, onAC, act)
		}
	}
	if !st.onAC {
		t.Error("the latched source moved during a flap, so the machine's real source was lost")
	}
}

// TestPowerTickNeverInfersBatteryFromAnUnreadableSource pins the contract that
// keeps a VM or a desktop — anything with no mains device — from having the
// battery profile applied to it forever.
func TestPowerTickNeverInfersBatteryFromAnUnreadableSource(t *testing.T) {
	cfg := auto(true, "balanced", "battery-uv")
	var st powerState
	for range 5 {
		var act powerAction
		st, act = powerTick(st, powerObs{Known: false, Auto: cfg, Current: "balanced"})
		if !act.none() {
			t.Fatalf("acted on an unknown source: %+v", act)
		}
	}
	if st.known {
		t.Error("an unknown source was latched, so a later real reading would look like no change")
	}
}

func TestAutoswitchTarget(t *testing.T) {
	profiles := map[string]api.CustomProfile{
		"battery-uv": {Name: "battery-uv", TDP: &api.TDPState{PL1SPL: 35}},
		"blank":      {Name: "blank"}, // created but never populated
	}

	tests := []struct {
		name string
		s    api.State
		onAC bool
		want string
	}{
		{
			name: "no configuration",
			s:    api.State{Profile: "balanced"},
			onAC: true,
		},
		{
			name: "disabled",
			s:    api.State{Profile: "balanced", Autoswitch: auto(false, "quiet", "battery-uv"), CustomProfiles: profiles},
		},
		{
			name: "battery target",
			s:    api.State{Profile: "balanced", Autoswitch: auto(true, "quiet", "battery-uv"), CustomProfiles: profiles},
			want: "battery-uv",
		},
		{
			name: "AC target",
			s:    api.State{Profile: "battery-uv", Autoswitch: auto(true, "quiet", "battery-uv"), CustomProfiles: profiles},
			onAC: true,
			want: "quiet",
		},
		{
			name: "already active, nothing to do at startup",
			s:    api.State{Profile: "battery-uv", Autoswitch: auto(true, "quiet", "battery-uv"), CustomProfiles: profiles},
			want: "",
		},
		{
			name: "unconfigured side",
			s:    api.State{Profile: "balanced", Autoswitch: auto(true, "", "battery-uv"), CustomProfiles: profiles},
			onAC: true,
		},
		{
			// A profile deleted from a hand-edited state file, or lost in a
			// downgrade. Returning it would have Run() set state.Profile to a
			// name that does not resolve.
			name: "target that no longer exists",
			s:    api.State{Profile: "balanced", Autoswitch: auto(true, "quiet", "deleted"), CustomProfiles: profiles},
		},
		{
			// applyProfileLocked refuses an empty profile, so returning it here
			// would have startup claim a profile the watcher declines to select,
			// leaving hardware describing the previous one.
			name: "target exists but holds no settings",
			s:    api.State{Profile: "balanced", Autoswitch: auto(true, "quiet", "blank"), CustomProfiles: profiles},
		},
		{
			name: "custom targets that do hold settings are still selected",
			s:    api.State{Profile: "balanced", Autoswitch: auto(true, "quiet", "battery-uv"), CustomProfiles: profiles},
			want: "battery-uv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoswitchTarget(tt.s, tt.onAC); got != tt.want {
				t.Errorf("autoswitchTarget(onAC=%v) = %q, want %q", tt.onAC, got, tt.want)
			}
		})
	}
}

// TestAutoswitchDeferralSurvivesTheSuspend is the regression test for a "deferral"
// that permanently discarded the transition.
//
// powerSourceOnce must stand down *before* powerTick, returning prev untouched.
// The watcher is edge-triggered: powerTick latches st.onAC on the confirming tick,
// so an implementation that ran powerTick and dropped only the apply consumed the
// edge — every later tick then computed sourceChanged == false and the machine ran
// the AC profile on battery until the charger was physically cycled.
func TestAutoswitchDeferralSurvivesTheSuspend(t *testing.T) {
	t.Parallel()

	auto := &api.AutoswitchState{Enabled: true, AC: "balanced", Battery: "quiet"}
	obs := powerObs{OnAC: false, Known: true, Auto: auto, Current: "balanced"}
	onAC := powerState{known: true, onAC: true, enabled: true}

	// What the watcher does while suspending: prev is returned, powerTick unrun.
	suspended := onAC

	// After the resume the edge must still be there to find: one tick to arm the
	// settle window, the next to confirm and apply.
	st, act := powerTick(suspended, obs)
	if !act.none() {
		t.Fatalf("acted on an unconfirmed transition after resume: %+v", act)
	}
	_, act = powerTick(st, obs)
	if act.Profile != "quiet" {
		t.Fatalf("after the resume the battery profile was never applied (Profile=%q); "+
			"the transition was consumed by the suspend instead of deferred", act.Profile)
	}

	// And the contrast: consuming the edge (what the broken version returned) loses
	// it for good, however many ticks follow.
	consumed, _ := powerTick(onAC, obs)
	consumed, _ = powerTick(consumed, obs) // confirms, latching onAC=false
	for i := 1; i <= 4; i++ {
		var a powerAction
		consumed, a = powerTick(consumed, obs)
		if !a.none() {
			t.Fatalf("tick %d after a consumed edge produced %+v; this test's premise is wrong", i, a)
		}
	}
}
