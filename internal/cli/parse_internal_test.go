package cli

// parse_internal_test.go — in-package tests for the pure validation helpers
// that moved here with the driver extraction: fan-curve parsing, TDP flag
// resolution, and custom profile names. In-package (unlike parse_test.go's
// external suite) because the profile-name cases pin maxProfileNameLen.

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
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
func TestTDPStateFor(t *testing.T) {
	tests := []struct {
		name                 string
		watts, pl1, pl2, pl3 int
		want                 api.TDPState
	}{
		{
			name:  "unified watts fills all limits",
			watts: 45,
			want:  api.TDPState{PL1SPL: 45, PL2SPPT: 45, FPPT: 45, APUSPPT: 45, PlatformSPPT: 45},
		},
		{
			name:  "non-zero overrides replace watts",
			watts: 45, pl1: 40, pl2: 60, pl3: 70,
			want: api.TDPState{PL1SPL: 40, PL2SPPT: 60, FPPT: 70, APUSPPT: 60, PlatformSPPT: 60},
		},
		{
			name:  "zero override falls back to watts",
			watts: 50, pl2: 65,
			want: api.TDPState{PL1SPL: 50, PL2SPPT: 65, FPPT: 50, APUSPPT: 65, PlatformSPPT: 65},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TDPStateFor(tt.watts, tt.pl1, tt.pl2, tt.pl3); got != tt.want {
				t.Errorf("TDPStateFor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"plain", "gaming", false},
		{"digits and separators", "battery-uv_2", false},
		{"empty", "", true},
		{"uppercase", "Gaming", true},
		{"stock quiet", "quiet", true},
		{"stock balanced", "balanced", true},
		{"stock performance", "performance", true},
		{"reserved custom", "custom", true},
		{"stock name with different case", "Balanced", true},
		{"leading space", " gaming", true},
		{"trailing space", "gaming ", true},
		{"space inside", "my profile", true},
		{"punctuation", "gaming!", true},
		{"slash would escape the state map", "a/b", true},
		{"too long", strings.Repeat("a", maxProfileNameLen+1), true},
		{"at the length limit", strings.Repeat("a", maxProfileNameLen), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProfileName(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProfileName(%q) = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

// TestNoStockProfileNameIsAcceptedAsCustom is the invariant the safety engine's
// ReadEffective silently relies on: it treats any name absent from the
// envelope's stock table as custom and disables its stale-5W fallback, so a
// custom profile named "balanced" would make the machine misreport its power
// limits. (The companion data test in internal/device checks the envelope's
// own table keys against the same reservation.)
func TestNoStockProfileNameIsAcceptedAsCustom(t *testing.T) {
	for _, name := range []string{"quiet", "balanced", "performance"} {
		if !api.IsStockProfileName(name) {
			t.Errorf("api.IsStockProfileName(%q) = false, want true", name)
		}
		if err := ValidateProfileName(name); err == nil {
			t.Errorf("ValidateProfileName(%q) = nil, want an error — a custom profile may not shadow a firmware profile", name)
		}
		var s api.State
		if s.IsCustomProfile(name) {
			t.Errorf("State.IsCustomProfile(%q) = true, want false", name)
		}
	}
}

// TestIsCustomProfileIgnoresAShadowingMapEntry covers the hand-edited state
// file: even with a "balanced" entry in the profile map, the firmware profile
// must win.
func TestIsCustomProfileIgnoresAShadowingMapEntry(t *testing.T) {
	s := api.State{CustomProfiles: map[string]api.CustomProfile{
		"balanced": {Name: "balanced"},
		"gaming":   {Name: "gaming"},
	}}
	if s.IsCustomProfile("balanced") {
		t.Error("IsCustomProfile(\"balanced\") = true, want false even with a map entry of that name")
	}
	if !s.IsCustomProfile("gaming") {
		t.Error("IsCustomProfile(\"gaming\") = false, want true")
	}
	if !s.IsCustomProfile(api.DefaultCustomProfile) {
		t.Error("IsCustomProfile(\"custom\") = false, want true even with no map entry")
	}
	if s.IsCustomProfile("never-saved") {
		t.Error("IsCustomProfile(\"never-saved\") = true, want false")
	}
}
