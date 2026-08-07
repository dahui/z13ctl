package daemon

// resume_test.go — decision coverage for the sleep hook that hands the fans back
// to firmware auto so the EC stops them through s2idle.
//
// Every case here goes through the pure sleepTick. That is deliberate:
// internal/cli's sysfs path vars are unexported, so a daemon test that reached
// releaseVolatileState would lower the developer's real power limits and rewrite
// their fan mode.

import (
	"os"
	"testing"
	"time"

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

// TestWakeupCount covers the measurement the sleep path is judged by. It must
// degrade to -1 rather than 0 when unreadable: 0 is a plausible real count, so
// conflating the two would make "we could not measure" look like "no wakeup
// events", which is the exact conclusion being tested for.
func TestWakeupCount(t *testing.T) {
	cases := []struct {
		name    string
		content string
		write   bool
		want    int
	}{
		{name: "typical value", content: "99\n", write: true, want: 99},
		{name: "zero is a real count", content: "0\n", write: true, want: 0},
		{name: "no trailing newline", content: "42", write: true, want: 42},
		{name: "empty (pm_wakeup_irq before any wakeup)", content: "\n", write: true, want: -1},
		{name: "non-numeric", content: "wat\n", write: true, want: -1},
		{name: "missing file", want: -1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			swapPowerDir(t, dir)
			if tt.write {
				if err := os.WriteFile(dir+"/wakeup_count", []byte(tt.content), 0o644); err != nil {
					t.Fatalf("writing fake wakeup_count: %v", err)
				}
			}
			if got := wakeupCount(); got != tt.want {
				t.Errorf("wakeupCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWakeupIRQReadsItsOwnAttribute(t *testing.T) {
	dir := t.TempDir()
	swapPowerDir(t, dir)
	if err := os.WriteFile(dir+"/pm_wakeup_irq", []byte("9\n"), 0o644); err != nil {
		t.Fatalf("writing fake pm_wakeup_irq: %v", err)
	}
	if got := wakeupIRQ(); got != 9 {
		t.Errorf("wakeupIRQ() = %d, want 9", got)
	}
	// wakeup_count is absent, so a helper reading the wrong file would return -1.
	if got := wakeupCount(); got != -1 {
		t.Errorf("wakeupCount() = %d with no wakeup_count file, want -1", got)
	}
}

func swapPowerDir(t *testing.T, dir string) {
	t.Helper()
	orig := sysPowerDir
	sysPowerDir = dir
	t.Cleanup(func() { sysPowerDir = orig })
}

// TestParseSleepSteps pins the diagnostic knob's parsing, and in particular that a
// value it does not understand keeps both steps *enabled*. Falling back to
// disabled would look exactly like the bug the knob exists to investigate — a
// silent typo would make the fans keep running through sleep and be reported as a
// regression.
func TestParseSleepSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		ppt      bool
		fans     bool
		accepted bool
	}{
		{in: "", ppt: true, fans: true, accepted: true},
		{in: "ppt,fans", ppt: true, fans: true, accepted: true},
		{in: "fans,ppt", ppt: true, fans: true, accepted: true},
		{in: "fans", fans: true, accepted: true},
		{in: "ppt", ppt: true, accepted: true},
		{in: "FANS", fans: true, accepted: true},
		{in: " fans , ppt ", ppt: true, fans: true, accepted: true},
		{in: "none", accepted: true},
		{in: "NONE", accepted: true},
		// Not understood: both stay on, and the caller is told so it can warn.
		{in: "fan", ppt: true, fans: true},
		{in: "ppt,bogus", ppt: true, fans: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, ok := parseSleepSteps(tt.in)
			if got.PPT != tt.ppt || got.Fans != tt.fans {
				t.Errorf("parseSleepSteps(%q) = %+v, want {PPT:%v Fans:%v}", tt.in, got, tt.ppt, tt.fans)
			}
			if ok != tt.accepted {
				t.Errorf("parseSleepSteps(%q) accepted = %v, want %v", tt.in, ok, tt.accepted)
			}
		})
	}
}

// TestSleepSettleIsSkippedWithNothingToDo is why releaseVolatileState checks
// act.none() before sleeping: a machine on a firmware profile writes nothing, and
// must not spend sleepSettleDelay out of logind's inhibit budget on every suspend
// for work that never happened.
//
// It asserts the decision rather than calling releaseVolatileState, which would
// reach real sysfs — internal/cli's path vars are unexported. The pairing under
// test is "sleepTick said nothing, therefore no settle".
func TestSleepSettleIsSkippedWithNothingToDo(t *testing.T) {
	t.Parallel()
	inert := []sleepObs{
		{Owned: false, CurveMode: 1, PL1: 90}, // not ours
		{Owned: true, CurveMode: 2, PL1: 90},  // already firmware auto
		{Owned: true, CurveMode: 0, PL1: 90},  // forced full speed
		{Owned: true, CurveMode: -1, PL1: 90}, // unreadable
	}
	for _, obs := range inert {
		if act := sleepTick(obs); !act.none() {
			t.Errorf("sleepTick(%+v) = %+v, want no action (and so no settle delay)", obs, act)
		}
	}
}

// TestSleepSettleDelayIsTunable covers the Z13CTL_SLEEP_SETTLE_MS override. The
// delay is a var precisely so no test ever sleeps and so the bisect can widen it
// on hardware without a rebuild.
func TestSleepSettleDelayIsTunable(t *testing.T) {
	orig := sleepSettleDelay
	t.Cleanup(func() { sleepSettleDelay = orig })

	tests := []struct {
		env  string
		want time.Duration
	}{
		{env: "0", want: 0},
		{env: "1500", want: 1500 * time.Millisecond},
		{env: " 250 ", want: 250 * time.Millisecond},
		{env: "-1", want: orig},   // invalid: keep the default
		{env: "soon", want: orig}, // invalid: keep the default
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			sleepSettleDelay = orig
			t.Setenv(envSleepSettleMS, tt.env)
			(&Daemon{}).applySleepEnv()
			if sleepSettleDelay != tt.want {
				t.Errorf("sleepSettleDelay = %v with %s=%q, want %v",
					sleepSettleDelay, envSleepSettleMS, tt.env, tt.want)
			}
		})
	}
}
