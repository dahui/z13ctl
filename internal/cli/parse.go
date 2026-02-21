// parse.go — input parsing helpers for color and brightness flags.
package cli

import (
	"fmt"
	"strconv"
	"strings"
)

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
