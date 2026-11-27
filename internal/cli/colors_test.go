package cli_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/cli"
)

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
