package cli

// colors.go — named color display for --color / --color2 flags.
//
// The color table and parsing moved to internal/aura with the lighting driver
// extraction — drivers parse api.LightingState colors too, so the definition
// of "blue" lives with the protocol. This file keeps the presentation side
// (swatches, display labels) and forwards the lookups cmd/ still consumes.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/v2/internal/aura"
)

// NamedColors is the name → hex lookup table (aura's, aliased).
var NamedColors = aura.NamedColors

// ResolveColor forwards to aura.ResolveColor.
func ResolveColor(s string) string { return aura.ResolveColor(s) }

// ColorDisplay returns a human-readable label for a color string.
// If s is a known name it returns the name; otherwise it returns "#XXXXXX".
func ColorDisplay(s string) string {
	lower := strings.ToLower(s)
	if _, ok := NamedColors[lower]; ok {
		return lower
	}
	hex := strings.ToUpper(strings.TrimPrefix(s, "#"))
	return "#" + hex
}

// hexToRGB converts a 6-digit lowercase hex string to R, G, B bytes.
// Returns 0, 0, 0 for malformed input (callers always pass validated hex).
func hexToRGB(hex string) (r, g, b uint8) {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

// swatchLine returns a formatted line with an ANSI true-color background swatch,
// the color name, and its hex value.
func swatchLine(c aura.NamedColor) string {
	r, g, b := hexToRGB(c.Hex)
	swatch := fmt.Sprintf("\033[48;2;%d;%d;%dm      \033[0m", r, g, b)
	return fmt.Sprintf("  %s  %-12s  #%s", swatch, c.Name, strings.ToUpper(c.Hex))
}

// PrintColorList prints all named colors with ANSI true-color swatches.
func PrintColorList() {
	fmt.Println("\nSupported color names:")
	for _, c := range aura.NamedColorList {
		fmt.Println(swatchLine(c))
	}
	fmt.Println()
}
