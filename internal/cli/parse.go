package cli

// parse.go — input parsing helpers for color and brightness flags.

import (
	"fmt"
	"strings"

	"github.com/dahui/z13ctl/internal/aura"
)

// ParseColor parses a color name or 6-digit hex string (RRGGBB) into R, G, B
// bytes. Forwards to aura.ParseColor, where the color table lives.
func ParseColor(s string) (r, g, b uint8, err error) {
	return aura.ParseColor(s)
}

// ParseBrightness parses a brightness level name or number into a 0–3 uint8.
// Accepted names: off (0), low (1), medium (2), high (3).
// Numeric strings "0"–"3" are also accepted.
func ParseBrightness(s string) (uint8, error) {
	switch strings.ToLower(s) {
	case "off", "0":
		return 0, nil
	case "low", "1":
		return 1, nil
	case "medium", "med", "2":
		return 2, nil
	case "high", "3":
		return 3, nil
	}
	return 0, fmt.Errorf("brightness must be off/low/medium/high (or 0–3), got %q", s)
}
