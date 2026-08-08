package safety

// The Z13 behaviour of every function here is pinned by internal/cli's tests,
// which exercise these rules through that package's forwarders with the real
// envelope. These tests cover what those cannot: envelopes that are not the
// Z13's, and devices that declare no floor at all — the generalization this
// package exists for.

import (
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/driver"
)

// smallDevice is a fictional envelope with a low safe maximum and a short,
// non-Z13 floor so nothing passes by matching the Z13's numbers.
func smallDevice() driver.PowerEnvelope {
	return driver.PowerEnvelope{
		TDPMin:       4,
		TDPMaxSafe:   30,
		TDPMaxForced: 45,
		FloorCurve: []api.FanCurvePoint{
			{Temp: 40, PWM: 100},
			{Temp: 60, PWM: 200},
		},
	}
}

// floorless declares no floor: high limits impose nothing on the fans.
func floorless() driver.PowerEnvelope {
	return driver.PowerEnvelope{TDPMin: 4, TDPMaxSafe: 30, TDPMaxForced: 45}
}

func TestFanCurveForTDPUsesTheEnvelopesThreshold(t *testing.T) {
	env := smallDevice()
	want := []api.FanCurvePoint{{Temp: 40, PWM: 10}, {Temp: 60, PWM: 20}}

	if got := FanCurveForTDP(env, env.TDPMaxSafe, want); got != nil {
		t.Errorf("at the safe max: got %v, want nil (no floor imposed)", got)
	}
	got := FanCurveForTDP(env, env.TDPMaxSafe+1, want)
	if got == nil {
		t.Fatal("one over the safe max: got nil, want a floored curve")
	}
	// Raised to this device's floor at each point's own temperature.
	if got[0].PWM != 100 || got[1].PWM != 200 {
		t.Errorf("floored curve = %v, want PWMs raised to [100 200]", got)
	}
	// The caller's curve is never mutated.
	if want[0].PWM != 10 || want[1].PWM != 20 {
		t.Errorf("want was mutated: %v", want)
	}
}

func TestFanCurveForTDPNoWantReturnsACopyOfTheFloor(t *testing.T) {
	env := smallDevice()
	got := FanCurveForTDP(env, env.TDPMaxSafe+1, nil)
	if len(got) != len(env.FloorCurve) {
		t.Fatalf("got %v, want the floor curve %v", got, env.FloorCurve)
	}
	got[0].PWM = 1
	if env.FloorCurve[0].PWM != 100 {
		t.Error("mutating the result reached the envelope's own floor slice")
	}
}

func TestFloorlessDeviceImposesNothing(t *testing.T) {
	env := floorless()
	high := env.TDPMaxForced
	low := []api.FanCurvePoint{{Temp: 90, PWM: 0}}

	if got := FanCurveForTDP(env, high, low); got != nil {
		t.Errorf("FanCurveForTDP = %v, want nil on a floorless device", got)
	}
	if FloorAdjustsCurve(env, high, low) {
		t.Error("FloorAdjustsCurve = true on a floorless device")
	}
	if err := CheckCurveAgainstTDP(env, low, high); err != nil {
		t.Errorf("CheckCurveAgainstTDP = %v, want nil on a floorless device", err)
	}
	if err := CheckFanFloorReleaseAt(env, high); err != nil {
		t.Errorf("CheckFanFloorReleaseAt = %v, want nil on a floorless device", err)
	}
}

func TestFloorPWMAtInterpolatesAnArbitraryFloor(t *testing.T) {
	floor := smallDevice().FloorCurve // 40:100 → 60:200
	for _, tt := range []struct{ temp, want int }{
		{0, 100},   // below the first point: a floor does not taper off
		{40, 100},  // at the first point
		{50, 150},  // halfway
		{55, 175},  // three quarters
		{60, 200},  // at the last point
		{100, 200}, // past the last point
	} {
		if got := FloorPWMAt(floor, tt.temp); got != tt.want {
			t.Errorf("FloorPWMAt(%d) = %d, want %d", tt.temp, got, tt.want)
		}
	}
	if got := FloorPWMAt(nil, 50); got != 0 {
		t.Errorf("FloorPWMAt(nil, 50) = %d, want 0", got)
	}
}

func TestCheckCurveAgainstTDPSpeaksTheEnvelopesNumbers(t *testing.T) {
	env := smallDevice()
	err := CheckCurveAgainstTDP(env, []api.FanCurvePoint{{Temp: 60, PWM: 50}}, env.TDPMaxSafe+1)
	if err == nil {
		t.Fatal("curve below this device's floor was accepted")
	}
	// The refusal must describe this device, not the Z13: its safe max and its
	// floor's endpoints.
	for _, want := range []string{"30W", "from 100 at 40°C", "to 200 at 60°C"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestCheckFanFloorReleaseAtUsesTheEnvelopesBottom(t *testing.T) {
	env := smallDevice()
	if err := CheckFanFloorReleaseAt(env, env.TDPMaxSafe); err != nil {
		t.Errorf("release at the safe max refused: %v", err)
	}
	err := CheckFanFloorReleaseAt(env, env.TDPMaxSafe+1)
	if err == nil {
		t.Fatal("release above the safe max was allowed on a floored device")
	}
	if !strings.Contains(err.Error(), "above 100 PWM") {
		t.Errorf("error %q does not name this device's floor bottom", err)
	}
}
