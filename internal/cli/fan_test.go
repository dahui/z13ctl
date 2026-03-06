package cli_test

import (
	"testing"

	"github.com/dahui/z13ctl/internal/cli"
)

func TestParseFanCurve_Valid(t *testing.T) {
	curve := "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"
	points, err := cli.ParseFanCurve(curve)
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
}

func TestParseFanCurve_WrongPointCount(t *testing.T) {
	_, err := cli.ParseFanCurve("48:2,53:22,57:30")
	if err == nil {
		t.Error("expected error for 3 points")
	}
}

func TestParseFanCurve_NonMonotonicTemp(t *testing.T) {
	// Point 2 temp (50) is less than point 1 temp (53)
	_, err := cli.ParseFanCurve("48:2,53:22,50:30,60:43,63:56,65:68,70:89,76:102")
	if err == nil {
		t.Error("expected error for non-monotonic temps")
	}
}

func TestParseFanCurve_DecreasingPWM(t *testing.T) {
	// Point 2 pwm (20) is less than point 1 pwm (22)
	_, err := cli.ParseFanCurve("48:2,53:22,57:20,60:43,63:56,65:68,70:89,76:102")
	if err == nil {
		t.Error("expected error for decreasing PWM")
	}
}

func TestParseFanCurve_TempOutOfRange(t *testing.T) {
	_, err := cli.ParseFanCurve("48:2,53:22,57:30,60:43,63:56,65:68,70:89,130:102")
	if err == nil {
		t.Error("expected error for temp > 120")
	}
}

func TestParseFanCurve_PWMOutOfRange(t *testing.T) {
	_, err := cli.ParseFanCurve("48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:300")
	if err == nil {
		t.Error("expected error for PWM > 255")
	}
}

func TestParseFanCurve_InvalidFormat(t *testing.T) {
	_, err := cli.ParseFanCurve("48-2,53:22,57:30,60:43,63:56,65:68,70:89,76:102")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestParseFanCurve_Percentage(t *testing.T) {
	curve := "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"
	points, err := cli.ParseFanCurve(curve)
	if err != nil {
		t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
	}
	// 1% of 255 = 2 (integer division), 40% of 255 = 102
	if points[0].PWM != 2 {
		t.Errorf("point 0 PWM: got %d, want 2 (1%% of 255)", points[0].PWM)
	}
	if points[7].PWM != 102 {
		t.Errorf("point 7 PWM: got %d, want 102 (40%% of 255)", points[7].PWM)
	}
}

func TestParseFanCurve_MixedFormats(t *testing.T) {
	// Mix PWM and percentage in the same curve.
	curve := "48:1%,53:22,57:12%,60:43,63:22%,65:68,70:35%,76:102"
	points, err := cli.ParseFanCurve(curve)
	if err != nil {
		t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
	}
	if points[0].PWM != 2 {
		t.Errorf("point 0 PWM: got %d, want 2 (1%%)", points[0].PWM)
	}
	if points[1].PWM != 22 {
		t.Errorf("point 1 PWM: got %d, want 22 (raw)", points[1].PWM)
	}
}

func TestParseFanCurve_PercentageOutOfRange(t *testing.T) {
	_, err := cli.ParseFanCurve("48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:101%")
	if err == nil {
		t.Error("expected error for percentage > 100")
	}
}

func TestParseFanCurve_Percentage100(t *testing.T) {
	curve := "30:100%,40:100%,50:100%,60:100%,65:100%,70:100%,75:100%,80:100%"
	points, err := cli.ParseFanCurve(curve)
	if err != nil {
		t.Fatalf("ParseFanCurve(%q) = error %v", curve, err)
	}
	if points[0].PWM != 255 {
		t.Errorf("100%% should be PWM 255, got %d", points[0].PWM)
	}
}

func TestFanModeName(t *testing.T) {
	tests := []struct {
		mode int
		want string
	}{
		{0, "full-speed"},
		{1, "custom"},
		{2, "auto"},
		{99, "unknown(99)"},
	}
	for _, tt := range tests {
		got := cli.FanModeName(tt.mode)
		if got != tt.want {
			t.Errorf("FanModeName(%d) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}
