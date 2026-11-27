package safety

// curve_test.go — the fan-curve rules (moved here from internal/cli).

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// shape8 is the Z13's fan-curve shape, which every legacy case below was
// written against. The shape-specific subtests at the bottom are what pin the
// parse actually being driven by it.
var shape8 = driver.FanShape{Points: 8, TempMin: 35, TempMax: 105, PWMMax: 255}

func TestParseFanCurve(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		curve := "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"
		points, err := ParseFanCurve(shape8, curve)
		if err != nil {
			t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
		}
		if len(points) != 8 {
			t.Fatalf("expected 8 points, got %d", len(points))
		}
		if points[0].Temp != 48 || points[0].PWM != 2 {
			t.Errorf("point 0: got {%d, %d}, want {48, 2}", points[0].Temp, points[0].PWM)
		}
		if points[7].Temp != 76 || points[7].PWM != 102 {
			t.Errorf("point 7: got {%d, %d}, want {76, 102}", points[7].Temp, points[7].PWM)
		}
	})

	t.Run("wrong point count", func(t *testing.T) {
		if _, err := ParseFanCurve(shape8, "48:2,53:22,57:30"); err == nil {
			t.Error("expected error for 3 points")
		}
	})

	t.Run("non-monotonic temps", func(t *testing.T) {
		// Point 3 temp (50) is less than point 2 temp (53).
		if _, err := ParseFanCurve(shape8, "48:2,53:22,50:30,60:43,63:56,65:68,70:89,76:102"); err == nil {
			t.Error("expected error for non-monotonic temps")
		}
	})

	t.Run("decreasing PWM", func(t *testing.T) {
		// Point 3 pwm (20) is less than point 2 pwm (22).
		if _, err := ParseFanCurve(shape8, "48:2,53:22,57:20,60:43,63:56,65:68,70:89,76:102"); err == nil {
			t.Error("expected error for decreasing PWM")
		}
	})

	t.Run("temp out of range", func(t *testing.T) {
		if _, err := ParseFanCurve(shape8, "48:2,53:22,57:30,60:43,63:56,65:68,70:89,130:102"); err == nil {
			t.Error("expected error for temp > 120")
		}
	})

	t.Run("PWM out of range", func(t *testing.T) {
		if _, err := ParseFanCurve(shape8, "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:300"); err == nil {
			t.Error("expected error for PWM > 255")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		if _, err := ParseFanCurve(shape8, "48-2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"); err == nil {
			t.Error("expected error for invalid format")
		}
	})

	t.Run("percentage", func(t *testing.T) {
		curve := "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"
		points, err := ParseFanCurve(shape8, curve)
		if err != nil {
			t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
		}
		// 1% of 255 = 2 (integer division), 40% of 255 = 102.
		if points[0].PWM != 2 {
			t.Errorf("point 0 PWM: got %d, want 2 (1%% of 255)", points[0].PWM)
		}
		if points[7].PWM != 102 {
			t.Errorf("point 7 PWM: got %d, want 102 (40%% of 255)", points[7].PWM)
		}
	})

	t.Run("mixed formats", func(t *testing.T) {
		curve := "48:1%,53:22,57:12%,60:43,63:22%,65:68,70:35%,76:102"
		points, err := ParseFanCurve(shape8, curve)
		if err != nil {
			t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
		}
		if points[0].PWM != 2 {
			t.Errorf("point 0 PWM: got %d, want 2 (1%%)", points[0].PWM)
		}
		if points[1].PWM != 22 {
			t.Errorf("point 1 PWM: got %d, want 22 (raw)", points[1].PWM)
		}
	})

	t.Run("percentage out of range", func(t *testing.T) {
		if _, err := ParseFanCurve(shape8, "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:101%"); err == nil {
			t.Error("expected error for percentage > 100")
		}
	})

	t.Run("percentage 100", func(t *testing.T) {
		curve := "30:100%,40:100%,50:100%,60:100%,65:100%,70:100%,75:100%,80:100%"
		points, err := ParseFanCurve(shape8, curve)
		if err != nil {
			t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
		}
		if points[0].PWM != 255 {
			t.Errorf("100%% should be PWM 255, got %d", points[0].PWM)
		}
	})

	// The point of the shape parameter: another device's shape changes what
	// parses, without this function knowing any device.
	t.Run("point count follows the shape", func(t *testing.T) {
		four := driver.FanShape{Points: 4, TempMin: 30, TempMax: 90, PWMMax: 255}
		if _, err := ParseFanCurve(four, "40:50,50:100,60:150,70:200"); err != nil {
			t.Errorf("4-point curve on a 4-point shape = %v, want nil", err)
		}
		if _, err := ParseFanCurve(four, "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"); err == nil {
			t.Error("8-point curve on a 4-point shape = nil, want an error")
		}
	})

	t.Run("percentage converts against the shape's PWM ceiling", func(t *testing.T) {
		hundred := driver.FanShape{Points: 2, TempMin: 30, TempMax: 90, PWMMax: 100}
		points, err := ParseFanCurve(hundred, "40:50%,60:100%")
		if err != nil {
			t.Fatalf("ParseFanCurve = %v, want nil", err)
		}
		if points[0].PWM != 50 || points[1].PWM != 100 {
			t.Errorf("points = %+v, want PWM 50 and 100 against a 100 ceiling", points)
		}
		if _, err := ParseFanCurve(hundred, "40:101,60:102"); err == nil {
			t.Error("PWM above the shape's ceiling = nil, want an error")
		}
	})

	t.Run("zero shape is an error, not a parse against nothing", func(t *testing.T) {
		if _, err := ParseFanCurve(driver.FanShape{}, "40:50"); err == nil {
			t.Error("zero shape accepted a curve")
		}
	})
}

// TestTDPStateFor pins the flag-resolution rule the tdp command documents:
// non-zero per-limit overrides replace the unified watts value, and APU sPPT
// and Platform sPPT always follow PL2.
