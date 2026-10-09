package safety

// The engine's two orderings — fans before power on the way up, power before
// fans on the way down — previously existed only as prose in CLAUDE.md and as
// the emergent behaviour of sysfs-level tests. These fakes record the actual
// call sequence so the orderings are pinned as such: a refactor that swaps
// them fails here by name, not by a fan-mode value three layers away.

import (
	"errors"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

type fakeRig struct {
	calls    []string
	fanErr   error
	powerErr error
	env      driver.PowerEnvelope

	// held is the limit the fake firmware enforces. A successful Apply sets it;
	// a successful Release resets it to firmware, which is what the real
	// firmware does on a fan release (issue #22) and what lets a test tell a
	// re-applied limit from one that was never touched.
	held     api.TDPState
	firmware api.TDPState
}

type rigFans struct{ r *fakeRig }

func (f rigFans) ApplyCurve(_ []api.FanCurvePoint) error {
	f.r.calls = append(f.r.calls, "fans.ApplyCurve")
	return f.r.fanErr
}

func (f rigFans) Release() error {
	f.r.calls = append(f.r.calls, "fans.Release")
	if f.r.fanErr == nil {
		f.r.held = f.r.firmware
	}
	return f.r.fanErr
}

type rigPower struct{ r *fakeRig }

func (p rigPower) Read() (api.TDPState, error) { return p.r.held, nil }

func (p rigPower) Apply(s api.TDPState) error {
	p.r.calls = append(p.r.calls, "power.Apply")
	if p.r.powerErr == nil {
		p.r.held = s
	}
	return p.r.powerErr
}

func (p rigPower) Envelope() driver.PowerEnvelope { return p.r.env }

func newRig() (*fakeRig, Engine) {
	r := &fakeRig{env: smallDevice()}
	return r, Engine{Fans: rigFans{r}, Power: rigPower{r}}
}

func sequence(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

func TestApplyTDPSafelyRaisesFansBeforePower(t *testing.T) {
	r, e := newRig()
	high := api.TDPState{PL1SPL: r.env.TDPMaxSafe + 1}
	if err := e.ApplyTDPSafely(high, nil); err != nil {
		t.Fatalf("ApplyTDPSafely: %v", err)
	}
	sequence(t, r.calls, "fans.ApplyCurve", "power.Apply")
}

func TestApplyTDPSafelyFailsClosedOnFanRefusal(t *testing.T) {
	r, e := newRig()
	r.fanErr = errors.New("curve dropped")
	high := api.TDPState{PL1SPL: r.env.TDPMaxSafe + 1}
	err := e.ApplyTDPSafely(high, nil)
	if err == nil {
		t.Fatal("fan refusal did not refuse the TDP")
	}
	if !strings.Contains(err.Error(), "refusing to apply") {
		t.Errorf("error %q does not say it refused", err)
	}
	sequence(t, r.calls, "fans.ApplyCurve") // power.Apply never ran
}

func TestApplyTDPSafelySkipsFansAtOrBelowSafeMax(t *testing.T) {
	r, e := newRig()
	if err := e.ApplyTDPSafely(api.TDPState{PL1SPL: r.env.TDPMaxSafe}, nil); err != nil {
		t.Fatalf("ApplyTDPSafely: %v", err)
	}
	sequence(t, r.calls, "power.Apply")
}

func TestApplyTDPSafelySkipsFansOnAFloorlessDevice(t *testing.T) {
	r, e := newRig()
	r.env = floorless()
	if err := e.ApplyTDPSafely(api.TDPState{PL1SPL: r.env.TDPMaxForced}, nil); err != nil {
		t.Fatalf("ApplyTDPSafely: %v", err)
	}
	sequence(t, r.calls, "power.Apply")
}

func TestReleaseTDPLowersPowerBeforeFans(t *testing.T) {
	r, e := newRig()
	if err := e.ReleaseTDP(api.TDPState{PL1SPL: 20}); err != nil {
		t.Fatalf("ReleaseTDP: %v", err)
	}
	sequence(t, r.calls, "power.Apply", "fans.Release")
}

func TestReleaseTDPKeepsTheFloorWhenLoweringFails(t *testing.T) {
	r, e := newRig()
	r.powerErr = errors.New("write refused")
	if err := e.ReleaseTDP(api.TDPState{PL1SPL: 20}); err == nil {
		t.Fatal("failed lowering reported success")
	}
	sequence(t, r.calls, "power.Apply") // fans.Release never ran
}

// The ReleaseFans tests pin the issue #22 fix. A fan release makes the firmware
// re-apply the profile's own power limits (the rig's held is reset to firmware),
// so a custom TDP written before the release was gone after it. ReleaseFans
// re-applies keep after the release, and refuses a keep that needs the floor.

func releaseRig() (*fakeRig, Engine, api.TDPState) {
	r, e := newRig()
	r.firmware = api.TDPState{PL1SPL: 28, PL2SPPT: 28, FPPT: 28}
	keep := api.TDPState{PL1SPL: 20, PL2SPPT: 20, FPPT: 20}
	r.held = keep
	return r, e, keep
}

func TestReleaseFansReappliesTheKeptLimit(t *testing.T) {
	r, e, keep := releaseRig()
	if err := e.ReleaseFans(&keep); err != nil {
		t.Fatalf("ReleaseFans = %v", err)
	}
	sequence(t, r.calls, "fans.Release", "power.Apply")
	if r.held != keep {
		t.Errorf("limit in force = %+v, want the kept %+v: the release reset was not undone", r.held, keep)
	}
}

func TestReleaseFansWithoutKeepLeavesTheFirmwareLimits(t *testing.T) {
	r, e, _ := releaseRig()
	if err := e.ReleaseFans(nil); err != nil {
		t.Fatalf("ReleaseFans(nil) = %v", err)
	}
	sequence(t, r.calls, "fans.Release")
	if r.held != r.firmware {
		t.Errorf("limit in force = %+v, want the firmware's %+v", r.held, r.firmware)
	}
}

// A keep above the safe maximum needs the floor the release would remove, so it
// is refused before anything is written — refusing after releasing is too late.
func TestReleaseFansRefusesAKeepThatNeedsTheFloor(t *testing.T) {
	r, e, _ := releaseRig()
	high := api.TDPState{PL1SPL: r.env.TDPMaxSafe + 10}
	if err := e.ReleaseFans(&high); err == nil {
		t.Fatal("ReleaseFans above the safe maximum = nil, want a refusal")
	}
	sequence(t, r.calls)
}

// Without a floor, a high limit imposes nothing on the fans, so it may be kept.
func TestReleaseFansKeepsAHighLimitOnAFloorlessDevice(t *testing.T) {
	r, e, _ := releaseRig()
	r.env = floorless()
	high := api.TDPState{PL1SPL: r.env.TDPMaxSafe + 10}
	if err := e.ReleaseFans(&high); err != nil {
		t.Fatalf("ReleaseFans on a floorless device = %v", err)
	}
	sequence(t, r.calls, "fans.Release", "power.Apply")
}

func TestReleaseFansReportsAFailedReapply(t *testing.T) {
	r, e, keep := releaseRig()
	r.powerErr = errors.New("ppt write failed")
	err := e.ReleaseFans(&keep)
	if err == nil || !strings.Contains(err.Error(), "re-applying") {
		t.Fatalf("ReleaseFans with a failing re-apply = %v, want an error saying the limit was not re-applied", err)
	}
}

func TestReleaseFansDoesNotReapplyWhenTheReleaseFailed(t *testing.T) {
	r, e, keep := releaseRig()
	r.fanErr = errors.New("pwm_enable write failed")
	if err := e.ReleaseFans(&keep); err == nil {
		t.Fatal("ReleaseFans with a failing release = nil, want the error")
	}
	sequence(t, r.calls, "fans.Release")
}

func TestReleaseFansIsANoOpWithoutFans(t *testing.T) {
	r, e, keep := releaseRig()
	e.Fans = nil
	if err := e.ReleaseFans(&keep); err != nil {
		t.Fatalf("ReleaseFans with no fan control = %v", err)
	}
	sequence(t, r.calls)
}

// The HandBackToFirmware tests pin issue #22's second half: the stock row is
// looser than the firmware's own limits, so a stock profile must end on the
// release's re-apply, never on the row. The rig's firmware differs from its
// row for exactly that reason.

func handBackRig() (*fakeRig, Engine) {
	r, e := newRig()
	r.env.StockProfilePPT = map[string]api.TDPState{"balanced": {PL1SPL: 20, PL2SPPT: 26, FPPT: 26}}
	r.firmware = api.TDPState{PL1SPL: 20, PL2SPPT: 20, FPPT: 20}
	return r, e
}

func TestHandBackToFirmwareEndsOnTheFirmwareLimits(t *testing.T) {
	for _, start := range []int{10, 40} { // 40 needs the floor until the row lowers it
		r, e := handBackRig()
		r.held = api.TDPState{PL1SPL: start, PL2SPPT: start, FPPT: start}
		if err := e.HandBackToFirmware("balanced"); err != nil {
			t.Fatalf("from %dW: HandBackToFirmware = %v", start, err)
		}
		sequence(t, r.calls, "power.Apply", "fans.Release")
		if r.held != r.firmware {
			t.Errorf("from %dW: limit in force = %+v, want the firmware's own %+v", start, r.held, r.firmware)
		}
	}
}

func TestHandBackToFirmwareKeepsTheFloorWhenTheRowFails(t *testing.T) {
	r, e := handBackRig()
	r.held = api.TDPState{PL1SPL: 40}
	r.powerErr = errors.New("ppt write failed")
	if err := e.HandBackToFirmware("balanced"); err == nil {
		t.Fatal("HandBackToFirmware with the limit stuck high = nil, want a refusal")
	}
	sequence(t, r.calls, "power.Apply")
}

func TestHandBackToFirmwareWithoutARowStillReleases(t *testing.T) {
	r, e := handBackRig()
	r.held = api.TDPState{PL1SPL: 10}
	if err := e.HandBackToFirmware(""); err != nil {
		t.Fatalf("HandBackToFirmware(\"\") = %v", err)
	}
	sequence(t, r.calls, "fans.Release")
	if r.held != r.firmware {
		t.Errorf("limit in force = %+v, want the firmware's own %+v", r.held, r.firmware)
	}
}

func TestHandBackToFirmwareWithoutFansWritesTheRow(t *testing.T) {
	r, e := handBackRig()
	e.Fans = nil
	if err := e.HandBackToFirmware("balanced"); err != nil {
		t.Fatalf("HandBackToFirmware with no fan control = %v", err)
	}
	sequence(t, r.calls, "power.Apply")
}
