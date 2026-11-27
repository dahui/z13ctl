package aura_test

// colors_test.go — the named colour table and its resolver (moved here from
// internal/cli, which only forwarded to them).

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/aura"
)

func TestResolveColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		// known names → hex (lowercase, no #)
		{"red", "ff0000"},
		{"RED", "ff0000"}, // case-insensitive
		{"cyan", "00ffff"},
		{"hotpink", "ff69b4"},
		{"white", "ffffff"},
		{"navy", "000080"},
		// unknown → passthrough unchanged
		{"mauve", "mauve"},
		{"#FF0000", "#FF0000"},
		{"FF0000", "FF0000"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := aura.ResolveColor(tt.input)
			if got != tt.want {
				t.Errorf("ResolveColor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNamedColorsMap(t *testing.T) {
	t.Parallel()

	// Spot-check key entries and verify map is populated from the list.
	cases := map[string]string{
		"red":       "ff0000",
		"cyan":      "00ffff",
		"blue":      "0000ff",
		"hotpink":   "ff69b4",
		"white":     "ffffff",
		"magenta":   "ff00ff",
		"navy":      "000080",
		"indigo":    "4b0082",
		"turquoise": "40e0d0",
	}
	for name, wantHex := range cases {
		if got, ok := aura.NamedColors[name]; !ok {
			t.Errorf("NamedColors[%q] missing", name)
		} else if got != wantHex {
			t.Errorf("NamedColors[%q] = %q, want %q", name, got, wantHex)
		}
	}
}
