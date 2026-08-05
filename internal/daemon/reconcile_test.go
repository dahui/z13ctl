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

func TestReconcileTick(t *testing.T) {
	saved := curve(120)
	floorTDP := &api.TDPState{PL1SPL: 90, PL2SPPT: 90, FPPT: 90}

	tests := []struct {
		name      string
		obs       reconcileObs
		wantCurve bool
		wantFloor bool // the restored curve must be HighTDPFanCurve
		wantTDP   bool
	}{
		{
			name: "stock profile is left alone even with hardware in auto",
			obs:  reconcileObs{Profile: "balanced", WantCurve: saved, CurveMode: 2, PL1: 35},
		},
		{
			name: "empty profile is not custom",
			obs:  reconcileObs{Profile: "", WantCurve: saved, CurveMode: 2, PL1: 35},
		},
		{
			name: "curve still live: nothing to do",
			obs:  reconcileObs{Profile: "custom", WantCurve: saved, CurveMode: 1, PL1: 35},
		},
		{
			name: "unreadable mode: never act on an unknown",
			obs:  reconcileObs{Profile: "custom", WantCurve: saved, CurveMode: -1, PL1: 35},
		},
		{
			name: "full speed is more cooling than we would write",
			obs:  reconcileObs{Profile: "custom", WantCurve: saved, CurveMode: 0, PL1: 90},
		},
		{
			name:      "dropped to auto with a saved curve",
			obs:       reconcileObs{Profile: "custom", WantCurve: saved, CurveMode: 2, PL1: 35},
			wantCurve: true,
		},
		{
			name:      "unreadable PPT does not block restoring a saved curve",
			obs:       reconcileObs{Profile: "custom", WantCurve: saved, CurveMode: 2, PL1: -1},
			wantCurve: true,
		},
		{
			name:      "no saved curve but the high-TDP floor is required",
			obs:       reconcileObs{Profile: "custom", CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			wantFloor: true,
		},
		{
			// Caught on hardware: the saved curve is only vetted against the
			// limit in force when it is saved, and raising the TDP afterwards
			// leaves a sub-floor curve in state. Restoring it would undo the
			// floor at the moment it is needed.
			name:      "saved curve below the floor while the limit is high",
			obs:       reconcileObs{Profile: "custom", WantCurve: saved, CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			wantFloor: true,
		},
		{
			name:      "saved curve already meets the floor",
			obs:       reconcileObs{Profile: "custom", WantCurve: curve(cli.HighTDPMinPWM), CurveMode: 2, PL1: cli.TDPMaxSafe + 1},
			wantCurve: true,
			wantFloor: true, // its own points are at the floor
		},
		{
			name: "no saved curve and the limit is safe",
			obs:  reconcileObs{Profile: "custom", CurveMode: 2, PL1: cli.TDPMaxSafe},
		},
		{
			name: "no saved curve and PPT unreadable",
			obs:  reconcileObs{Profile: "custom", CurveMode: 2, PL1: -1},
		},
		{
			name:    "PPT drifted from the saved custom value",
			obs:     reconcileObs{Profile: "custom", CurveMode: 1, PL1: 35, WantTDP: floorTDP},
			wantTDP: true,
		},
		{
			name: "PPT matches the saved value",
			obs:  reconcileObs{Profile: "custom", CurveMode: 1, PL1: 90, WantTDP: floorTDP},
		},
		{
			name: "unreadable PPT is not a TDP mismatch",
			obs:  reconcileObs{Profile: "custom", CurveMode: 1, PL1: -1, WantTDP: floorTDP},
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
				if tt.wantFloor {
					if act.Curve[0].PWM != cli.HighTDPMinPWM {
						t.Errorf("restored curve starts at PWM %d, want the %d floor", act.Curve[0].PWM, cli.HighTDPMinPWM)
					}
				} else if act.Curve[0].PWM != saved[0].PWM {
					t.Errorf("restored curve starts at PWM %d, want the saved %d", act.Curve[0].PWM, saved[0].PWM)
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
	obs := reconcileObs{Profile: "custom", WantCurve: curve(120), CurveMode: 2, PL1: 35}

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
	st, act = reconcileTick(st, reconcileObs{Profile: "custom", CurveMode: 1, PL1: 35})
	if !act.none() {
		t.Fatalf("tick acted on a live curve: %+v", act)
	}
	if st.quiet || st.failures != 0 {
		t.Errorf("state after a clean tick = %+v, want failures 0 and quiet false", st)
	}
}

func TestReconcileTickTracksProfileForLogging(t *testing.T) {
	st, _ := reconcileTick(reconcileState{lastHW: "performance"},
		reconcileObs{Profile: "custom", CurveMode: 1, ProfileHW: "balanced"})
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
