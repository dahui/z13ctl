package cli

// parse.go — input parsing and validation helpers shared by cmd/ and the
// daemon: colors, brightness, fan curves, TDP flag resolution, and custom
// profile names. Everything here is pure — no sysfs, no device access — which
// is what lets both the CLI and the daemon validate a request identically
// before either touches hardware.

import (
	"github.com/dahui/voltaire/v2/internal/aura"
)

// ParseColor parses a color name or 6-digit hex string (RRGGBB) into R, G, B
// bytes. Forwards to aura.ParseColor, where the color table lives.
func ParseColor(s string) (r, g, b uint8, err error) {
	return aura.ParseColor(s)
}
