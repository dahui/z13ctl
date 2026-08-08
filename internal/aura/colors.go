package aura

// colors.go — named color table and color parsing for Aura lighting.
//
// Names are matched case-insensitively; hex strings (with or without #) pass
// through. This lives with the protocol rather than in the CLI because color
// strings arrive from two directions — CLI flags and api.LightingState fields
// a lighting driver must apply — and both must agree on what "blue" means.

import (
	"fmt"
	"strconv"
	"strings"
)

// NamedColor is one supported color name and its hex value.
type NamedColor struct {
	Name string
	Hex  string // 6-digit lowercase hex, no #
}

// NamedColorList is the ordered list of supported color names, arranged in
// spectral order so swatch output reads as a natural gradient.
var NamedColorList = []NamedColor{
	{"red", "ff0000"},
	{"crimson", "dc143c"},
	{"orangered", "ff4500"},
	{"coral", "ff7f50"},
	{"orange", "ff8000"},
	{"gold", "ffd700"},
	{"yellow", "ffff00"},
	{"chartreuse", "7fff00"},
	{"green", "00ff00"},
	{"springgreen", "00ff7f"},
	{"aquamarine", "7fffd4"},
	{"teal", "008080"},
	{"cyan", "00ffff"},
	{"deepskyblue", "00bfff"},
	{"dodgerblue", "1e90ff"},
	{"royalblue", "4169e1"},
	{"blue", "0000ff"},
	{"navy", "000080"},
	{"indigo", "4b0082"},
	{"blueviolet", "8a2be2"},
	{"purple", "800080"},
	{"magenta", "ff00ff"},
	{"deeppink", "ff1493"},
	{"hotpink", "ff69b4"},
	{"violet", "ee82ee"},
	{"turquoise", "40e0d0"},
	{"brown", "a52a2a"},
	{"white", "ffffff"},
}

// NamedColors is the lookup map built from NamedColorList at init time.
var NamedColors = map[string]string{}

func init() {
	for _, c := range NamedColorList {
		NamedColors[c.Name] = c.Hex
	}
}

// ResolveColor returns the 6-digit hex string for a color name or hex input.
// If s is a known name, its hex value is returned. Otherwise s is returned
// as-is (to be validated by ParseColor).
func ResolveColor(s string) string {
	if hex, ok := NamedColors[strings.ToLower(s)]; ok {
		return hex
	}
	return s
}

// ParseColor parses a color name or 6-digit hex string (RRGGBB) into R, G, B bytes.
// Named colors (e.g. "blue", "hotpink") are resolved via the NamedColors table.
func ParseColor(s string) (r, g, b uint8, err error) {
	resolved := ResolveColor(s)
	hex := strings.TrimPrefix(resolved, "#")
	if len(hex) != 6 {
		// Distinguish "unknown name" from "bad hex" for a clearer message.
		if resolved == s {
			return 0, 0, 0, fmt.Errorf("unknown color name %q", s)
		}
		return 0, 0, 0, fmt.Errorf("expected 6-digit hex color or color name, got %q", s)
	}
	v, parseErr := strconv.ParseUint(hex, 16, 32)
	if parseErr != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", s, parseErr)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}
