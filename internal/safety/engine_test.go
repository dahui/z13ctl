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

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/driver"
)

type fakeRig struct {
	calls    []string
	fanErr   error
	powerErr error
	env      driver.PowerEnvelope
}

type rigFans struct{ r *fakeRig }

func (f rigFans) ApplyCurve(_ []api.FanCurvePoint) error {
	f.r.calls = append(f.r.calls, "fans.ApplyCurve")
	return f.r.fanErr
}

func (f rigFans) Release() error {
	f.r.calls = append(f.r.calls, "fans.Release")
	return f.r.fanErr
}

type rigPower struct{ r *fakeRig }

func (p rigPower) Read() (api.TDPState, error) { return api.TDPState{}, nil }

func (p rigPower) Apply(api.TDPState) error {
	p.r.calls = append(p.r.calls, "power.Apply")
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
