package daemon

// resume_test.go — decision coverage for the sleep hook that hands the fans back
// to firmware auto so the EC stops them through s2idle.
//
// Every case here goes through the pure sleepTick. That is deliberate:
// internal/cli's sysfs path vars are unexported, so a daemon test that reached
// releaseVolatileState would lower the developer's real power limits and rewrite
// their fan mode.

import (
	"context"
	"io/fs"
	"syscall"
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
			// Fail closed: an unreadable PPT could be above the safe max, so the limit
			// is lowered before the fans are released rather than after. This is the
			// one path that used to fail open.
			name: "an unreadable limit lowers power first",
			obs:  sleepObs{Owned: true, CurveMode: 1, PL1: -1, Firmware: "balanced"},

			lowerPPT: true, releaseFans: true,
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
		{
			// Both releases are asus-wmi writes. A curve left running through one
			// suspend is noise; a write into an EC that has not answered since the
			// last resume is a hard lock.
			name: "nothing is released while the EC is wedged",
			obs:  sleepObs{ECWedged: true, Owned: true, CurveMode: 1, PL1: 45, Firmware: "balanced"},
		},
		{
			name: "not even the PPT lowering a high limit would otherwise need",
			obs:  sleepObs{ECWedged: true, Owned: true, CurveMode: 1, PL1: 90, Firmware: "balanced"},
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

// TestSuspendCeilingExceedsInhibitDelay guards the relationship the stand-down
// ceiling depends on and cannot check at runtime.
//
// The ceiling counts *awake* ticks, and the only awake window inside a suspend is
// the one logind's delay lock holds open — bounded by InhibitDelayMaxSec. If the
// ceiling is the shorter of the two, the "stale flag" backstop fires inside a real
// pre-freeze window and re-enables the curve, which is precisely what it exists to
// prevent. 60 s covers the configured values people actually use; the default is 5.
func TestSuspendCeilingExceedsInhibitDelay(t *testing.T) {
	t.Parallel()
	const longestPlausibleInhibitDelay = 60 * time.Second
	budget := time.Duration(reconcileSuspendMaxTicks) * reconcilePollInterval
	if budget <= longestPlausibleInhibitDelay {
		t.Errorf("stand-down budget is %v, which does not exceed a %v InhibitDelayMaxSec: "+
			"the ceiling would fire inside a real pre-freeze window",
			budget, longestPlausibleInhibitDelay)
	}
}

func TestClassifyECReadSeparatesAbsentFromWedged(t *testing.T) {
	t.Parallel()
	// The whole fix turns on this distinction. ENOENT is a machine with no
	// battery, which must still be restored; ENODEV is a registered power_supply
	// whose EC is not answering, which must not be written to. Collapsing both
	// into "did the read fail" is what let a wedged EC through to a PPT write.
	cases := []struct {
		name string
		err  error
		want ecStatus
	}{
		{"read succeeded", nil, ecReady},
		{"no battery at all", fs.ErrNotExist, ecAbsent},
		{"wrapped ENOENT", &fs.PathError{Err: syscall.ENOENT}, ecAbsent},
		{"EC not answering", &fs.PathError{Err: syscall.ENODEV}, ecWedged},
		{"read timed out", &fs.PathError{Err: syscall.ETIMEDOUT}, ecWedged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyECRead(tc.err); got != tc.want {
				t.Errorf("classifyECRead(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWaitForECWaitsForTheECToAnswer(t *testing.T) {
	t.Parallel()
	calls := 0
	probe := func() ecStatus {
		calls++
		if calls >= 3 {
			return ecReady
		}
		return ecWedged
	}
	status, ok := waitForECWith(t.Context(), probe, 0, time.Millisecond, time.Second)
	if !ok {
		t.Fatal("waitForECWith reported cancellation for a probe that answered")
	}
	if status != ecReady {
		t.Errorf("status = %v, want ecReady", status)
	}
	if calls != 3 {
		t.Errorf("probed %d times, want 3: the loop must retry until the EC answers", calls)
	}
}

func TestWaitForECRestoresAnywayWhenThereIsNoBattery(t *testing.T) {
	t.Parallel()
	// A bench supply or a removed pack never answers, but it is not a wedged EC:
	// the attribute is missing rather than failing. Skipping the restore there
	// would strand the machine on whatever the firmware left behind.
	absent := func() ecStatus { return ecAbsent }
	status, ok := waitForECWith(t.Context(), absent, 0, time.Millisecond, 5*time.Millisecond)
	if !ok {
		t.Fatal("a batteryless machine reported cancellation")
	}
	if status != ecAbsent {
		t.Errorf("status = %v, want ecAbsent: a batteryless machine must still be restored", status)
	}
}

func TestWaitForECReportsAWedgedECOnTimeout(t *testing.T) {
	t.Parallel()
	// The regression this whole wait exists for: the attribute is present and the
	// read keeps failing, so the EC is up but not answering. Reporting ecReady —
	// or the old blanket "restore anyway" — drives a WMI write into the stalled
	// ACPI mutex and hard-locks every core.
	wedged := func() ecStatus { return ecWedged }
	status, ok := waitForECWith(t.Context(), wedged, 0, time.Millisecond, 5*time.Millisecond)
	if !ok {
		t.Fatal("a wedged EC reported cancellation rather than a timeout")
	}
	if status != ecWedged {
		t.Errorf("status = %v, want ecWedged: the profile restore must be held back", status)
	}
}

func TestECStatusAfterSleepTreatsAVanishedBatteryAsWedged(t *testing.T) {
	t.Parallel()
	// The residual hole in the ENOENT/ENODEV split: a kernel that unregisters the
	// battery when the EC dies makes the read fail with ENOENT, which on its own
	// classifies as "no battery" and restores into the dead EC.
	cases := []struct {
		name       string
		probe      ecStatus
		hadBattery bool
		want       ecStatus
	}{
		{"battery vanished across the suspend", ecAbsent, true, ecWedged},
		{"never had a battery", ecAbsent, false, ecAbsent},
		{"answering", ecReady, true, ecReady},
		{"wedged stays wedged", ecWedged, false, ecWedged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ecStatusAfterSleep(tc.probe, tc.hadBattery); got != tc.want {
				t.Errorf("ecStatusAfterSleep(%v, %v) = %v, want %v", tc.probe, tc.hadBattery, got, tc.want)
			}
		})
	}
}

// TestRestoreLatchesAWedgedEC pins the hand-off from the resume wait to the
// watchers. restoreVolatileState is the only place the wait's verdict lands, so
// it must record it for the reconcile, power-source and sleep paths to stand down
// on, and a later resume that finds the EC answering must lift it.
//
// It runs the real function on a firmware profile with no HID device, which
// returns before any hardware access: the lighting block is skipped on a nil
// d.dev, and a firmware profile has nothing for applyCustomHW to restore.
func TestRestoreLatchesAWedgedEC(t *testing.T) {
	t.Parallel()
	d := &Daemon{state: api.State{Profile: "balanced"}}

	wedged := func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.ecWedged
	}

	d.restoreVolatileState(ecWedged)
	if !wedged() {
		t.Fatal("a wedged EC was not latched; the reconcile watcher would write into it two seconds later")
	}

	// A batteryless machine is not wedged: nothing about it blocks a WMI write.
	d.restoreVolatileState(ecAbsent)
	if wedged() {
		t.Error("ecAbsent left the latch set; a batteryless machine would never be restored")
	}

	d.restoreVolatileState(ecWedged)
	d.restoreVolatileState(ecReady)
	if wedged() {
		t.Error("a resume that found the EC answering did not lift the latch")
	}
}

func TestWaitForECAbandonsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	probe := func() ecStatus { return ecWedged }
	if _, ok := waitForECWith(ctx, probe, time.Hour, time.Hour, time.Hour); ok {
		t.Error("a cancelled context must abandon the wait, not restore")
	}
}

func TestECWaitFitsInsideSuspendCeiling(t *testing.T) {
	t.Parallel()
	// restoreVolatileState clears the suspending flag, so the EC wait holds it
	// set for its whole duration. If that could outlast the reconcile watcher's
	// stand-down budget the watcher would log a stale-flag warning and start
	// defending the fans mid-resume — against the very EC being waited on.
	worst := ecSettleDelay + ecProbeTimeout
	budget := time.Duration(reconcileSuspendMaxTicks) * reconcilePollInterval
	if worst >= budget {
		t.Errorf("worst-case EC wait is %v but the reconcile stand-down budget is %v: "+
			"the watcher would wake up inside a resume", worst, budget)
	}
}
