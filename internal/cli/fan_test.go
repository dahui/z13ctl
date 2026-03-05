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
