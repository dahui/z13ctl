package cli_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/internal/cli"
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
			got := cli.ResolveColor(tt.input)
			if got != tt.want {
				t.Errorf("ResolveColor(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestColorDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		// known names → normalised lowercase name
		{"red", "red"},
		{"RED", "red"},
		{"hotpink", "hotpink"},
		{"cyan", "cyan"},
		// unknown/hex → "#" + uppercased value (# stripped if present)
		{"FF0000", "#FF0000"},
		{"ff0000", "#FF0000"},
		{"#FF0000", "#FF0000"},
		{"00FFFF", "#00FFFF"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := cli.ColorDisplay(tt.input)
			if got != tt.want {
				t.Errorf("ColorDisplay(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNamedColorsMap(t *testing.T) {
	t.Parallel()

	// Spot-check key entries and verify map is populated from the list.
	cases := map[string]string{
		"red":      "ff0000",
		"cyan":     "00ffff",
		"blue":     "0000ff",
		"hotpink":  "ff69b4",
		"white":    "ffffff",
		"magenta":  "ff00ff",
		"navy":     "000080",
		"indigo":   "4b0082",
		"turquoise": "40e0d0",
	}
	for name, wantHex := range cases {
		if got, ok := cli.NamedColors[name]; !ok {
			t.Errorf("NamedColors[%q] missing", name)
		} else if got != wantHex {
			t.Errorf("NamedColors[%q] = %q, want %q", name, got, wantHex)
		}
	}
}

func TestPrintColorList(t *testing.T) {
	// Capture stdout to verify output content.
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	cli.PrintColorList()

	_ = w.Close()
	os.Stdout = orig

	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	output := buf.String()

	for _, want := range []string{"Supported color names:", "red", "cyan", "hotpink", "#FF0000", "#00FFFF"} {
		if !strings.Contains(output, want) {
			t.Errorf("PrintColorList output missing %q", want)
		}
	}
}
