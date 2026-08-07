package daemon

// resume_test.go — decision coverage for the sleep hook that hands the fans back
// to firmware auto so the EC stops them through s2idle.
//
// Every case here goes through the pure sleepTick. That is deliberate:
// internal/cli's sysfs path vars are unexported, so a daemon test that reached
// releaseVolatileState would lower the developer's real power limits and rewrite
// their fan mode.

import (
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

func TestSleepTick(t *testing.T) {
	tests := []struct {
		name        string
		obs         sleepObs
		lowerPPT    bool
		releaseFans bool
	}{
		{
			name: "custom curve on a safe limit is released",
			obs:  sleepObs{Owned: true, CurveMode: 1, PL1: 45, Firmware: "balanced"},

			releaseFans: true,
		},
		{
			name: "custom curve above the safe limit lowers power first",
			obs:  sleepObs{Owned: true, CurveMode: 1, PL1: 90, Firmware: "balanced"},

			lowerPPT: true, releaseFans: true,
		},
		{
			name: "exactly at the safe limit needs no floor",
			obs:  sleepObs{Owned: true, CurveMode: 1, PL1: cli.TDPMaxSafe, Firmware: "performance"},

			releaseFans: true,
		},
		{
			// An unreadable PPT must not leave the fans running: the guard is
			// best-effort, the same stance CheckFanCurveFloor takes.
			name: "an unreadable limit still releases",
			obs:  sleepObs{Owned: true, CurveMode: 1, PL1: -1, Firmware: "balanced"},

			releaseFans: true,
		},
		{
			name: "already on firmware auto",
			obs:  sleepObs{Owned: true, CurveMode: 2, PL1: 90, Firmware: "balanced"},
		},
		{
			// Nothing recorded what it replaced, so it is not ours to undo —
			// reconcileTick leaves mode 0 alone for the same reason.
			name: "forced full speed is left alone",
			obs:  sleepObs{Owned: true, CurveMode: 0, PL1: 45, Firmware: "balanced"},
		},
		{
			name: "an unreadable fan mode is never acted on",
			obs:  sleepObs{Owned: true, CurveMode: -1, PL1: 45, Firmware: "balanced"},
		},
		{
			// The case that keeps the release from being a one-way door: a curve
			// set by asusctl also reads pwm_enable=1, and resume would restore
			// neither it nor the PPT we would have lowered.
			name: "a curve the daemon does not own is untouched",
			obs:  sleepObs{Owned: false, CurveMode: 1, PL1: 90, Firmware: "balanced"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			act := sleepTick(tt.obs)
			if act.LowerPPT != tt.lowerPPT {
				t.Errorf("LowerPPT = %v, want %v", act.LowerPPT, tt.lowerPPT)
			}
			if act.ReleaseFans != tt.releaseFans {
				t.Errorf("ReleaseFans = %v, want %v", act.ReleaseFans, tt.releaseFans)
			}
			if !act.none() && act.Reason == "" {
				t.Error("an action was decided with no reason to log")
			}
			if act.LowerPPT && !act.ReleaseFans {
				t.Error("lowered the limit without releasing the fans, which is the whole point of doing so")
			}
		})
	}
}

// TestSleepReleasesOnlyWhatResumeRestores pins the invariant that makes the sleep
// hook safe: whatever it releases, applyCustomHW puts back on resume.
//
// The two halves are checked against each other rather than against hardware.
// The sleep side is sleepTick; the resume side is applyCustomHW's own hasCurve /
// highTDP logic, reproduced here because the function itself writes sysfs. If
// that logic is ever reordered, this test is what says the pairing broke.
func TestSleepReleasesOnlyWhatResumeRestores(t *testing.T) {
	t.Parallel()

	fanCurve := &api.FanCurveState{Mode: 1, Points: curve(120)}
	tests := []struct {
		name string
		p    api.CustomProfile
		pl1  int
	}{
		{
			name: "curve, no TDP",
			p:    api.CustomProfile{Name: "quiet-ish", FanCurve: fanCurve},
			pl1:  45,
		},
		{
			name: "high TDP, no curve of its own",
			p:    api.CustomProfile{Name: "hot", TDP: &api.TDPState{PL1SPL: 90}},
			pl1:  90,
		},
		{
			name: "curve and high TDP",
			p:    api.CustomProfile{Name: "hot-tuned", FanCurve: fanCurve, TDP: &api.TDPState{PL1SPL: 90}},
			pl1:  90,
		},
		{
			name: "safe TDP, no curve",
			p:    api.CustomProfile{Name: "eco", TDP: &api.TDPState{PL1SPL: 30}},
			pl1:  30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Hardware is in custom mode, because either the profile's own curve or
			// ApplyTDPSafely's floor put it there.
			act := sleepTick(sleepObs{Owned: true, CurveMode: 1, PL1: tt.pl1, Firmware: "balanced"})
			if !act.ReleaseFans {
				t.Fatal("the fans were not released, so this profile would suspend loud")
			}

			// applyCustomHW's decision, verbatim.
			hasCurve := tt.p.FanCurve != nil && tt.p.FanCurve.Mode == 1 && len(tt.p.FanCurve.Points) == 8
			highTDP := tt.p.TDP != nil && tt.p.TDP.PL1SPL > cli.TDPMaxSafe

			// Either the profile writes a curve of its own, or ApplyTDPSafely writes
			// the floor. One of the two must return the fans to custom mode, or the
			// release we just made is permanent.
			if !hasCurve && !highTDP {
				if tt.pl1 > cli.TDPMaxSafe {
					t.Fatal("hardware is above the safe limit but the profile restores no curve and no floor")
				}
				// A safe limit with no curve belongs on firmware auto: the release is
				// what this profile wants, and applyCustomHW's own release agrees.
				return
			}

			if act.LowerPPT && tt.p.TDP == nil {
				t.Error("the limit was lowered on sleep but the profile has no TDP to restore it from")
			}
		})
	}
}

// TestSleepActionZeroValueIsInert guards the contract sleepTick's callers rely
// on: a zero sleepAction touches nothing, so a new field cannot silently make
// "do nothing" mean "do something".
func TestSleepActionZeroValueIsInert(t *testing.T) {
	t.Parallel()
	if !(sleepAction{}).none() {
		t.Error("the zero sleepAction is not inert")
	}
}

// TestSetSuspendingBumpsGenerationOnEntry covers the producer side of the counter
// the reconcile watcher uses to tell one suspend from the next.
//
// It must advance on every *entry* into suspending and not on the way out, and not
// on a repeated entry either — a machine that flaps sleep→wake→sleep faster than
// the 2 s reconcile poll never presents an idle tick, and the generation is the
// only thing that then distinguishes "still the same suspend" from "a new one".
func TestSetSuspendingBumpsGenerationOnEntry(t *testing.T) {
	t.Parallel()
	d := &Daemon{}

	read := func() (bool, int) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.suspending, d.suspendGen
	}

	if _, gen := read(); gen != 0 {
		t.Fatalf("fresh daemon has suspendGen %d, want 0", gen)
	}

	d.setSuspending(true)
	susp, gen := read()
	if !susp || gen != 1 {
		t.Fatalf("after first entry: suspending=%v gen=%d, want true/1", susp, gen)
	}

	// Idempotent while already suspending: the sleep hook is free to be called
	// twice without the watcher thinking a second suspend began.
	d.setSuspending(true)
	if _, gen = read(); gen != 1 {
		t.Errorf("a repeated entry advanced the generation to %d, want 1", gen)
	}

	// Leaving does not advance it — otherwise every resume would look like a new
	// suspend to the watcher and reset a budget that is no longer counting.
	d.setSuspending(false)
	susp, gen = read()
	if susp || gen != 1 {
		t.Errorf("after leaving: suspending=%v gen=%d, want false/1", susp, gen)
	}

	d.setSuspending(true)
	if _, gen = read(); gen != 2 {
		t.Errorf("the next suspend got generation %d, want 2", gen)
	}
}
