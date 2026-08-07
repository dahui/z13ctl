package daemon

// reconcile_test.go — decision coverage for the watcher that puts a custom fan
// curve back after the kernel drops it.
//
// Every case here goes through the pure reconcileTick. That is deliberate:
// internal/cli's sysfs path vars are unexported, so a daemon test that reached
// the apply path would write the developer's actual fan hardware.

import (
	"strings"
	"sync"
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

// rampedFrom is curve(pwm) after the high-TDP ramp has raised the points it
// exceeds. The PWM values are cli.HighTDPFanCurve's, written out rather than
// computed, so the test states the expected outcome instead of restating the rule.
func rampedFrom(pwm int) []api.FanCurvePoint {
	// cli.FloorPWMAt evaluated at curve()'s temperatures (30,35,…,65), written out
	// rather than computed so the test states the outcome instead of the rule.
	ramp := []int{127, 127, 127, 133, 140, 152, 165, 190}
	out := curve(pwm)
	for i := range out {
		if out[i].PWM < ramp[i] {
			out[i].PWM = ramp[i]
		}
	}
	return out
}

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
			// force when it is saved, and raising the TDP afterwards leaves sub-ramp
			// points in state. Each point comes up to the ramp — and only the ones
			// below it do; the temperatures stay as the user drew them, which is why
			// this expects the raised saved curve rather than HighTDPFanCurve.
			name:      "saved curve below the ramp while the limit is high",
			obs:       reconcileObs{Custom: true, WantCurve: saved, CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			expect:    rampedFrom(120),
		},
		{
			// Flat at the scalar minimum clears it everywhere and still falls under
			// the ramp from the third point on, so the ramp raises it. Accepting this
			// curve verbatim is what let a 93W limit run 50% fans at 90°C.
			name:      "saved curve flat at the minimum is raised to the ramp",
			obs:       reconcileObs{Custom: true, WantCurve: curve(cli.HighTDPMinPWM), CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			expect:    rampedFrom(cli.HighTDPMinPWM),
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
			name:      "high limit restores a saved curve, raising only the ramp's top",
			obs:       reconcileObs{Custom: true, WantCurve: curve(204), CurveMode: 2, PL1: 90},
			wantCurve: true,
			expect:    rampedFrom(204),
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

// TestReconcileReasonDescribesWhatWasDone is the regression test for a log line
// that lied. The curve branch used to set act.Reason unconditionally, so a
// PPT-only reconcile on a curveless custom profile at a safe limit reported
// "high-TDP fan floor was released while the limit is still in force" — the TDP
// arm only fills in a reason when one is not already set, so the fan text won.
func TestReconcileReasonDescribesWhatWasDone(t *testing.T) {
	t.Parallel()

	_, act := reconcileTick(reconcileState{}, reconcileObs{
		Custom: true, WantCurve: nil, CurveMode: 2, PL1: 52,
		WantTDP: &api.TDPState{PL1SPL: 67},
	})
	if act.Curve != nil {
		t.Errorf("restored a curve for a profile that has none at a safe limit: %+v", act.Curve)
	}
	if act.TDP == nil {
		t.Fatal("the drifted PPT was not restored")
	}
	if strings.Contains(act.Reason, "fan") {
		t.Errorf("reason mentions fans for a PPT-only action: %q", act.Reason)
	}
}

// TestReconcileSuspendBudgetIsPerSuspend is the regression test for a stand-down
// budget that accumulated across suspends. reconcileState resets on a
// non-suspending tick, but a machine flapping sleep→wake→sleep faster than the 2 s
// poll never presents one — so the counter climbed until it expired *inside* a real
// pre-freeze window and the watcher undid the release. obs.SuspendGen fixes it.
func TestReconcileSuspendBudgetIsPerSuspend(t *testing.T) {
	t.Parallel()

	var st reconcileState
	// Well past the ceiling, each iteration a distinct suspend, and never an idle
	// tick in between — the flap loop that used to defeat the counter.
	for gen := 1; gen <= reconcileSuspendMaxTicks*3; gen++ {
		var act reconcileAction
		st, act = reconcileTick(st, reconcileObs{
			Custom: true, Suspending: true, SuspendGen: gen,
			WantCurve: curve(204), CurveMode: 2, PL1: 52,
		})
		if !act.none() {
			t.Fatalf("suspend %d: re-enabled the curve mid-suspend, undoing the release", gen)
		}
		if st.suspendedTicks != 1 {
			t.Fatalf("suspend %d: suspendedTicks = %d, want 1 — the budget carried over", gen, st.suspendedTicks)
		}
	}

	// The ceiling must still fire within a *single* suspend, which is the missed
	// resume signal it exists for.
	st = reconcileState{}
	for i := 1; i <= reconcileSuspendMaxTicks; i++ {
		var act reconcileAction
		st, act = reconcileTick(st, reconcileObs{
			Custom: true, Suspending: true, SuspendGen: 7,
			WantCurve: curve(204), CurveMode: 2, PL1: 52,
		})
		if i < reconcileSuspendMaxTicks && !act.none() {
			t.Fatalf("tick %d of one suspend acted before the ceiling", i)
		}
		if i == reconcileSuspendMaxTicks && act.none() {
			t.Fatal("the ceiling never fired within a single suspend, so a missed resume leaves the curve undefended")
		}
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

// TestReconcileCurveForLeavesCurvelessProfilesAlone pins the third outcome of
// reconcileCurveFor: with no curve of the profile's own and a limit that needs no
// floor, nothing is written.
//
// The regression it guards is specific. reconcileOnce writes the TDP first and
// then recomputes the curve against the limit that write established, and it used
// to fall back to the tick's own act.Curve when the recomputation yielded nothing.
// act.Curve was computed against the *drifted* limit, so a curveless profile whose
// PPT had wandered above TDPMaxSafe had both arms fire, the TDP arm restored its own
// 52W, and the fallback then wrote the built-in high-TDP ramp — leaving both fans
// pinned to a 50% minimum on a profile that controls no fan curve at all. Nothing
// corrected it afterwards: pwm_enable read back as 1 ("the curve is live") and the
// TDP matched, so every later tick was a no-op.
func TestReconcileCurveForLeavesCurvelessProfilesAlone(t *testing.T) {
	t.Parallel()

	user := curve(220) // comfortably above the floor everywhere

	cases := []struct {
		name       string
		floorLimit int
		want       []api.FanCurvePoint
		expect     []api.FanCurvePoint
	}{
		{
			name:       "no curve and a safe limit writes nothing",
			floorLimit: 52,
			want:       nil,
			expect:     nil,
		},
		{
			name:       "no curve and a high limit writes the built-in floor",
			floorLimit: 90,
			want:       nil,
			expect:     cli.HighTDPFanCurve(),
		},
		{
			name:       "a curve at a safe limit is written as drawn",
			floorLimit: 52,
			want:       user,
			expect:     user,
		},
		{
			name:       "a curve at a high limit is raised where the ramp demands it",
			floorLimit: 90,
			want:       curve(100),
			expect:     rampedFrom(100),
		},
		{
			name:       "a curve already above the ramp is untouched at a high limit",
			floorLimit: 90,
			want:       user,
			expect:     user,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := reconcileCurveFor(tc.floorLimit, tc.want)
			if len(got) != len(tc.expect) {
				t.Fatalf("got %d points, want %d (got %v)", len(got), len(tc.expect), got)
			}
			for i := range got {
				if got[i] != tc.expect[i] {
					t.Fatalf("point %d = %v, want %v (got %v)", i, got[i], tc.expect[i], got)
				}
			}
		})
	}
}

// TestReconcileCurveForDoesNotMutateSavedCurve guards the same copying rule
// cli.FanCurveForTDP has: want aliases the profile's points in daemon state, so
// raising in place would rewrite the curve the user saved and leave nothing to
// restore when the limit comes back down.
func TestReconcileCurveForDoesNotMutateSavedCurve(t *testing.T) {
	t.Parallel()

	saved := curve(100)
	before := append([]api.FanCurvePoint(nil), saved...)

	reconcileCurveFor(90, saved)

	for i := range saved {
		if saved[i] != before[i] {
			t.Fatalf("saved curve was mutated at point %d: %v, want %v", i, saved[i], before[i])
		}
	}
}
