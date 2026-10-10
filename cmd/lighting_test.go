package cmd

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

func TestParseLightingBrightness(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		input   string
		max     int
		want    int
		wantErr bool
	}{
		// The Aura scale (0–3): the names the old fixed parser took.
		{"off", 3, 0, false}, {"OFF", 3, 0, false}, {"0", 3, 0, false},
		{"low", 3, 1, false}, {"LOW", 3, 1, false}, {"1", 3, 1, false},
		{"medium", 3, 2, false}, {"med", 3, 2, false}, {"MEDIUM", 3, 2, false}, {"2", 3, 2, false},
		{"high", 3, 3, false}, {"HIGH", 3, 3, false}, {"3", 3, 3, false},
		{"", 3, 0, true}, {"4", 3, 0, true}, {"full", 3, 0, true}, {"bright", 3, 0, true}, {"-1", 3, 0, true},
		// Another scale: the names are positions on it, and its numbers are legal.
		{"high", 10, 10, false}, {"medium", 10, 5, false}, {"7", 10, 7, false}, {"11", 10, 0, true},
		// No brightness control at all.
		{"high", 0, 0, true},
	} {
		got, err := parseLightingBrightness(tt.input, tt.max)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
			t.Errorf("parseLightingBrightness(%q, %d) = %d, %v; want %d, err=%v", tt.input, tt.max, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestCheckLightingZone(t *testing.T) {
	t.Parallel()
	c := driver.LightingCaps{Zones: []driver.LightingZone{{Name: "keyboard"}, {Name: "lightbar"}}}
	for _, ok := range []string{"", "keyboard", "lightbar", "/dev/hidraw3"} {
		if err := checkLightingZone(c, ok); err != nil {
			t.Errorf("checkLightingZone(%q) = %v, want accepted", ok, err)
		}
	}
	if err := checkLightingZone(c, "logo"); err == nil {
		t.Error("a zone the device does not have was accepted")
	}
}

// The confirmation names exactly the inputs the mode uses.
func TestAppliedText(t *testing.T) {
	colorFlag, color2Flag, speedFlag, brightnessFlag = "red", "blue", "slow", "high"
	if got, want := appliedText("", driver.LightingMode{Name: "cycle", Speed: true}),
		"Applied: mode=cycle speed=slow brightness=high"; got != want {
		t.Errorf("cycle: %q, want %q", got, want)
	}
	if got := appliedText("lightbar", driver.LightingMode{Name: "static", Color: true}); got[:17] != "Applied: lightbar" {
		t.Errorf("zone not named: %q", got)
	}
}
