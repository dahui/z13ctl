package aura_test

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/aura"
)

func TestModeFromString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    aura.Mode
		wantErr bool
	}{
		{"static", aura.ModeStatic, false},
		{"breathe", aura.ModeBreathe, false},
		{"cycle", aura.ModeCycle, false},
		{"rainbow", aura.ModeRainbow, false},
		{"strobe", aura.ModeStrobe, false},
		// ModeFromString is case-sensitive (matches CLI flag exactly)
		{"STATIC", 0, true},
		{"Static", 0, true},
		{"Breathe", 0, true},
		// unknown
		{"", 0, true},
		{"pulse", 0, true},
		{"disco", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := aura.ModeFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ModeFromString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ModeFromString(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSpeedFromString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    aura.Speed
		wantErr bool
	}{
		{"slow", aura.SpeedSlow, false},
		{"normal", aura.SpeedNormal, false},
		{"fast", aura.SpeedFast, false},
		// SpeedFromString is case-sensitive
		{"SLOW", 0, true},
		{"Normal", 0, true},
		// unknown
		{"", 0, true},
		{"medium", 0, true},
		{"turbo", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := aura.SpeedFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SpeedFromString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("SpeedFromString(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestModeByteValues pins the protocol byte values for each mode and speed.
// These are wire-protocol constants reverse-engineered from g-helper and must
// not change without deliberate intent.
func TestModeByteValues(t *testing.T) {
	t.Parallel()

	modeWant := map[aura.Mode]byte{
		aura.ModeStatic:  0,
		aura.ModeBreathe: 1,
		aura.ModeCycle:   2,
		aura.ModeRainbow: 3,
		aura.ModeStrobe:  10,
	}
	for mode, want := range modeWant {
		if byte(mode) != want {
			t.Errorf("Mode constant = %d, want %d", byte(mode), want)
		}
	}

	speedWant := map[aura.Speed]byte{
		aura.SpeedSlow:   0xe1,
		aura.SpeedNormal: 0xeb,
		aura.SpeedFast:   0xf5,
	}
	for speed, want := range speedWant {
		if byte(speed) != want {
			t.Errorf("Speed constant = %d, want %d", byte(speed), want)
		}
	}
}

// The mode table is what every client builds its controls from, so it must
// agree with the parser and with what SetMode puts on the wire: each name
// parses to its byte, and Color2 is exactly the mode SetMode sets the
// dual-colour flag for.
func TestModeTableMatchesTheProtocol(t *testing.T) {
	for _, m := range aura.Modes {
		got, err := aura.ModeFromString(m.Name)
		if err != nil || got != m.Mode {
			t.Errorf("%s: ModeFromString = %v, %v; want %v", m.Name, got, err, m.Mode)
		}
		if m.Color2 != (m.Mode == aura.ModeBreathe) {
			t.Errorf("%s: Color2 = %v, but only breathe sets the dual-colour flag", m.Name, m.Color2)
		}
	}
	for _, s := range aura.SpeedNames {
		if _, err := aura.SpeedFromString(s); err != nil {
			t.Errorf("speed %q does not parse: %v", s, err)
		}
	}
}
