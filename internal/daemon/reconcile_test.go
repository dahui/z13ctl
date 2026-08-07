package daemon

// reconcile_test.go — decision coverage for the watcher that puts a custom fan
// curve back after the kernel drops it.
//
// Every case here goes through the pure reconcileTick. That is deliberate:
// internal/cli's sysfs path vars are unexported, so a daemon test that reached
// the apply path would write the developer's actual fan hardware.

import (
	"sync"
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

func curve(pwm int) []api.FanCurvePoint {
	points := make([]api.FanCurvePoint, 8)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: pwm}
	}
	return points
}

// TestReconcileCustomFlagFollowsTheProfileMap covers what feeds obs.Custom:
// reconcileOnce derives it from ActiveCustomProfile, so a named profile must
// be defended exactly as "custom" is, and a stale or reserved name must not be.
// The apply path itself cannot be exercised here — internal/cli's path vars are
// unexported, so it would write the developer's real fan hardware.
func TestReconcileCustomFlagFollowsTheProfileMap(t *testing.T) {
	profiles := map[string]api.CustomProfile{
		"battery-uv": {Name: "battery-uv", FanCurve: &api.FanCurveState{Mode: 1, Points: curve(120)}},
		"balanced":   {Name: "balanced"}, // a hand-edited state file
	}
	tests := []struct {
		profile string
		want    bool
	}{
		{"battery-uv", true},
		{api.DefaultCustomProfile, true},
		{"balanced", false}, // reserved: the firmware profile always wins
		{"performance", false},
		{"deleted-profile", false},
		{"", false},
	}
	for _, tt := range tests {
		s := api.State{Profile: tt.profile, CustomProfiles: profiles}
		active, ok := s.ActiveCustomProfile()
		if ok != tt.want {
			t.Errorf("ActiveCustomProfile() with Profile=%q returned ok=%v, want %v", tt.profile, ok, tt.want)
		}
		if ok && tt.profile == "battery-uv" && active.FanCurve == nil {
			t.Error("the named profile's curve did not reach the watcher, so it would not be defended")
		}
	}
}

func TestReconcileTick(t *testing.T) {
	saved := curve(120)
	floorTDP := &api.TDPState{PL1SPL: 90, PL2SPPT: 90, FPPT: 90}

	tests := []struct {
		name      string
		obs       reconcileObs
		wantCurve bool
		// expect is the exact curve that must be restored. Leave it nil to mean
		// "obs.WantCurve, untouched" — the common case, and the one the floor rule
		// must not disturb.
		expect  []api.FanCurvePoint
		wantTDP bool
	}{
		{
			name: "stock profile is left alone even with hardware in auto",
			obs:  reconcileObs{Custom: false, WantCurve: saved, CurveMode: 2, PL1: 35},
		},
		{
			name: "curve still live: nothing to do",
			obs:  reconcileObs{Custom: true, WantCurve: saved, CurveMode: 1, PL1: 35},
		},
		{
			name: "unreadable mode: never act on an unknown",
			obs:  reconcileObs{Custom: true, WantCurve: saved, CurveMode: -1, PL1: 35},
		},
		{
			name: "full speed is more cooling than we would write",
			obs:  reconcileObs{Custom: true, WantCurve: saved, CurveMode: 0, PL1: 90},
		},
		{
			name:      "dropped to auto with a saved curve",
			obs:       reconcileObs{Custom: true, WantCurve: saved, CurveMode: 2, PL1: 35},
			wantCurve: true,
		},
		{
			name:      "unreadable PPT does not block restoring a saved curve",
			obs:       reconcileObs{Custom: true, WantCurve: saved, CurveMode: 2, PL1: -1},
			wantCurve: true,
		},
		{
			// Nothing to clamp, so the whole built-in floor curve is written.
			name:      "no saved curve but the high-TDP floor is required",
			obs:       reconcileObs{Custom: true, CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			expect:    cli.HighTDPFanCurve(),
		},
		{
			// Caught on hardware: the saved curve is only vetted against the limit in
			// force when it is saved, and raising the TDP afterwards leaves sub-floor
			// points in state. They are raised to the floor — and only they are; the
			// temperatures and every at-or-above-floor point stay as the user drew
			// them, which is why this expects the clamped saved curve rather than
			// HighTDPFanCurve.
			name:      "saved curve below the floor while the limit is high",
			obs:       reconcileObs{Custom: true, WantCurve: saved, CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			expect:    curve(cli.HighTDPMinPWM),
		},
		{
			// Exactly at the floor is not below it, so this curve is kept as drawn
			// rather than replaced. It reads as a near-tie only at point 0; from
			// 50°C HighTDPFanCurve ramps well above a flat 127, so the two are
			// genuinely different curves and the assertion below compares all eight
			// points.
			name:      "saved curve sitting exactly at the floor is kept",
			obs:       reconcileObs{Custom: true, WantCurve: curve(cli.HighTDPMinPWM), CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
		},
		{
			name: "no saved curve and the limit is safe",
			obs:  reconcileObs{Custom: true, CurveMode: 2, PL1: cli.TDPMaxSafe},
		},
		{
			name: "no saved curve and PPT unreadable",
			obs:  reconcileObs{Custom: true, CurveMode: 2, PL1: -1},
		},
		{
			name:    "PPT drifted from the saved custom value",
			obs:     reconcileObs{Custom: true, CurveMode: 1, PL1: 35, WantTDP: floorTDP},
			wantTDP: true,
		},
		{
			name: "PPT matches the saved value",
			obs:  reconcileObs{Custom: true, CurveMode: 1, PL1: 90, WantTDP: floorTDP},
		},
		{
			name: "unreadable PPT is not a TDP mismatch",
			obs:  reconcileObs{Custom: true, CurveMode: 1, PL1: -1, WantTDP: floorTDP},
		},
		{
			// The floor is a minimum, not an override. This case did not exist and
			// is the watcher-side half of the reported bug: a saved curve well above
			// the floor must be restored as drawn even under a high limit, which is
			// also what cli.ApplyTDPSafely now does on the apply path.
			name:      "high limit restores a saved curve that clears the floor",
			obs:       reconcileObs{Custom: true, WantCurve: curve(204), CurveMode: 2, PL1: 90},
			wantCurve: true,
		},
		{
			// And the PPT arm must not throw that curve away either — it used to
			// hand nil to ApplyTDPSafely, which then wrote the floor over a live
			// curve that already cleared it.
			name:      "high limit restores the curve and the drifted PPT together",
			obs:       reconcileObs{Custom: true, WantCurve: curve(204), CurveMode: 2, PL1: 60, WantTDP: floorTDP},
			wantCurve: true,
			wantTDP:   true,
		},
		{
			// The sleep hook released the fans on purpose so the EC can stop them
			// through s2idle. Restoring the curve here is the bug.
			name: "suspending: the deliberate release is not undone",
			obs:  reconcileObs{Custom: true, Suspending: true, WantCurve: saved, CurveMode: 2, PL1: 35},
		},
		{
			name: "suspending: a drifted PPT is left alone too",
			obs:  reconcileObs{Custom: true, Suspending: true, CurveMode: 1, PL1: 35, WantTDP: floorTDP},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, act := reconcileTick(reconcileState{}, tt.obs)

			if got := act.Curve != nil; got != tt.wantCurve {
				t.Errorf("restored a curve = %v, want %v", got, tt.wantCurve)
			}
			if got := act.TDP != nil; got != tt.wantTDP {
				t.Errorf("restored TDP = %v, want %v", got, tt.wantTDP)
			}
			if act.Curve != nil {
				// The whole curve, not just point 0. Comparing the first point alone
				// could not tell HighTDPFanCurve from a saved curve that happens to
				// start at the floor, which is exactly the distinction the reported
				// bug turned on — and it could not see a clamp at all.
				want := tt.obs.WantCurve
				if tt.expect != nil {
					want = tt.expect
				}
				if len(act.Curve) != len(want) {
					t.Fatalf("restored %d points, want %d", len(act.Curve), len(want))
				}
				for i := range act.Curve {
					if act.Curve[i] != want[i] {
						t.Errorf("restored point %d = %+v, want %+v", i+1, act.Curve[i], want[i])
					}
				}
				if act.Reason == "" {
					t.Error("an action was returned with no reason to log")
				}
			}
		})
	}
}

// TestReconcileTickQuietsAfterRepeatedFailure covers the latch that keeps a
// restore which can never succeed — revoked permissions, an unbound driver —
// from writing a warning to the journal every two seconds forever.
func TestReconcileTickQuietsAfterRepeatedFailure(t *testing.T) {
	obs := reconcileObs{Custom: true, WantCurve: curve(120), CurveMode: 2, PL1: 35}

	// The loop increments failures itself; the tick must not clear them while
	// the hardware still disagrees.
	st := reconcileState{failures: reconcileQuietAfter, quiet: true}
	st, act := reconcileTick(st, obs)
	if act.none() {
		t.Fatal("tick stopped acting once quiet; it must keep restoring, only stop logging")
	}
	if !st.quiet {
		t.Error("quiet latch was cleared while the curve was still not live")
	}

	// A tick with nothing to do clears the latch, so the next real failure is
	// logged again.
	st, act = reconcileTick(st, reconcileObs{Custom: true, CurveMode: 1, PL1: 35})
	if !act.none() {
		t.Fatalf("tick acted on a live curve: %+v", act)
	}
	if st.quiet || st.failures != 0 {
		t.Errorf("state after a clean tick = %+v, want failures 0 and quiet false", st)
	}
}

// TestReconcileSuspendCeiling covers the backstop for a PrepareForSleep(false)
// that never arrives. Standing down while suspending is what keeps the sleep
// release from being undone; standing down *forever* would leave a custom curve
// undefended for the rest of the daemon's life, so the flag expires.
//
// The ceiling is counted in ticks rather than elapsed time because Go timers use
// CLOCK_MONOTONIC, which does not advance across suspend — these are awake ticks,
// and a real suspend consumes one or two of them.
func TestReconcileSuspendCeiling(t *testing.T) {
	obs := reconcileObs{Custom: true, Suspending: true, WantCurve: curve(120), CurveMode: 2, PL1: 35}

	var st reconcileState
	for i := 1; i < reconcileSuspendMaxTicks; i++ {
		var act reconcileAction
		st, act = reconcileTick(st, obs)
		if !act.none() {
			t.Fatalf("tick %d acted while suspending: %+v", i, act)
		}
		if st.suspendedTicks != i {
			t.Fatalf("suspendedTicks after tick %d = %d, want %d", i, st.suspendedTicks, i)
		}
	}

	st, act := reconcileTick(st, obs)
	if act.none() {
		t.Fatalf("the watcher never resumed defending the curve after %d suspending ticks",
			reconcileSuspendMaxTicks)
	}
	// reconcileOnce keys its "stale flag" log and the daemon-side clear on the
	// counter being back to zero, so the reset is part of the contract.
	if st.suspendedTicks != 0 {
		t.Errorf("suspendedTicks = %d after falling through, want 0", st.suspendedTicks)
	}
}

// TestReconcileSuspendClearsOnResume covers the ordinary path: the flag goes away
// on resume, and the counter goes with it so the next suspend gets a full window.
func TestReconcileSuspendClearsOnResume(t *testing.T) {
	st, _ := reconcileTick(reconcileState{},
		reconcileObs{Custom: true, Suspending: true, WantCurve: curve(120), CurveMode: 2, PL1: 35})
	if st.suspendedTicks != 1 {
		t.Fatalf("suspendedTicks = %d, want 1", st.suspendedTicks)
	}

	st, act := reconcileTick(st, reconcileObs{Custom: true, WantCurve: curve(120), CurveMode: 2, PL1: 35})
	if act.none() {
		t.Error("the curve was not defended again after the suspending flag cleared")
	}
	if st.suspendedTicks != 0 {
		t.Errorf("suspendedTicks = %d, want 0 once no longer suspending", st.suspendedTicks)
	}
}

func TestReconcileTickTracksProfileForLogging(t *testing.T) {
	st, _ := reconcileTick(reconcileState{lastHW: "performance"},
		reconcileObs{Custom: true, CurveMode: 1, ProfileHW: "balanced"})
	if st.lastHW != "balanced" {
		t.Errorf("lastHW = %q, want \"balanced\"", st.lastHW)
	}
}

// TestReconcileOnceIsRaceFree guards the aliasing hazard the watcher adds: it
// reads state.FanCurve.Points and state.TDP, which handlers mutate under d.mu.
// Run under -race.
//
// The daemon state deliberately holds a stock profile: reconcileTick returns no
// action for it whatever the hardware reports, so this exercises the snapshot
// and locking without any path to a sysfs write.
func TestReconcileOnceIsRaceFree(t *testing.T) {
	d := &Daemon{state: sampleState()}
	d.state.Profile = "balanced"

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var st reconcileState
		for range 200 {
			st = d.reconcileOnce(st)
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 200 {
			d.mu.Lock()
			d.state.FanCurve.Points[0].PWM = i % 256
			d.state.TDP.PL1SPL = 20 + i%40
			d.mu.Unlock()
		}
	}()
	wg.Wait()

	// Guard the premise: if the profile were "custom", the ticks above would
	// have reached the apply path and written the machine's real fan hardware.
	if d.state.Profile != "balanced" {
		t.Fatalf("state.Profile = %q, want \"balanced\" — this test must never reach the apply path", d.state.Profile)
	}
}
