package cli_test

import (
	"strings"
	"testing"

	"github.com/dahui/z13ctl/internal/cli"
)

func TestParseBrightness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    uint8
		wantErr bool
	}{
		{input: "off", want: 0},
		{input: "OFF", want: 0},
		{input: "0", want: 0},
		{input: "low", want: 1},
		{input: "LOW", want: 1},
		{input: "1", want: 1},
		{input: "medium", want: 2},
		{input: "med", want: 2},
		{input: "MEDIUM", want: 2},
		{input: "2", want: 2},
		{input: "high", want: 3},
		{input: "HIGH", want: 3},
		{input: "3", want: 3},
		// invalid
		{input: "", wantErr: true},
		{input: "4", wantErr: true},
		{input: "full", wantErr: true},
		{input: "bright", wantErr: true},
		{input: "-1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := cli.ParseBrightness(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseBrightness(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseBrightness(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input        string
		r, g, b      uint8
		wantErr      bool
		errSubstring string
	}{
		// named colors
		{input: "red", r: 0xFF, g: 0x00, b: 0x00},
		{input: "RED", r: 0xFF, g: 0x00, b: 0x00}, // case-insensitive lookup
		{input: "cyan", r: 0x00, g: 0xFF, b: 0xFF},
		{input: "hotpink", r: 0xFF, g: 0x69, b: 0xB4},
		{input: "white", r: 0xFF, g: 0xFF, b: 0xFF},
		{input: "navy", r: 0x00, g: 0x00, b: 0x80},
		{input: "magenta", r: 0xFF, g: 0x00, b: 0xFF},
		// hex without #
		{input: "FF0000", r: 0xFF, g: 0x00, b: 0x00},
		{input: "00FFFF", r: 0x00, g: 0xFF, b: 0xFF},
		{input: "00FF88", r: 0x00, g: 0xFF, b: 0x88},
		{input: "000000", r: 0x00, g: 0x00, b: 0x00},
		{input: "FFFFFF", r: 0xFF, g: 0xFF, b: 0xFF},
		// hex with # prefix
		{input: "#FF0000", r: 0xFF, g: 0x00, b: 0x00},
		{input: "#00FFFF", r: 0x00, g: 0xFF, b: 0xFF},
		// lowercase hex
		{input: "ff0000", r: 0xFF, g: 0x00, b: 0x00},
		{input: "abcdef", r: 0xAB, g: 0xCD, b: 0xEF},
		// unknown name
		{input: "mauve", wantErr: true, errSubstring: "unknown color name"},
		{input: "notacolor", wantErr: true, errSubstring: "unknown color name"},
		{input: "", wantErr: true},
		// invalid hex (right length, wrong chars)
		{input: "ZZZZZZ", wantErr: true, errSubstring: "invalid hex"},
		{input: "XYZ123", wantErr: true, errSubstring: "invalid hex"},
		// wrong length
		{input: "FFF", wantErr: true},
		{input: "FF00FF00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			r, g, b, err := cli.ParseColor(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseColor(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
					t.Errorf("ParseColor(%q) error = %q, want to contain %q", tt.input, err, tt.errSubstring)
				}
				return
			}
			if r != tt.r || g != tt.g || b != tt.b {
				t.Errorf("ParseColor(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.input, r, g, b, tt.r, tt.g, tt.b)
			}
		})
	}
}
