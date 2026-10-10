package aura

// modes.go — Mode and Speed types, constants, and name parsers.

import "fmt"

// Mode corresponds to g-helper's AuraMode enum.
type Mode byte

// Mode byte values correspond to g-helper's AuraMode enum.
const (
	ModeStatic  Mode = 0
	ModeBreathe Mode = 1
	ModeCycle   Mode = 2
	ModeRainbow Mode = 3
	ModeStrobe  Mode = 10
)

// Speed corresponds to g-helper's AuraSpeed enum speed byte values.
type Speed byte

// Speed byte values correspond to g-helper's AuraSpeed enum.
const (
	SpeedSlow   Speed = 0xe1
	SpeedNormal Speed = 0xeb
	SpeedFast   Speed = 0xf5
)

// ModeInfo is what the protocol says about one mode: its wire name and which
// inputs its packet carries. Color is the primary colour, Color2 the second
// (Breathe alone sets the dual-colour flag), Speed the animation speed byte —
// a mode that does not animate ignores it.
type ModeInfo struct {
	Name                 string
	Mode                 Mode
	Color, Color2, Speed bool
}

// Modes is every mode this protocol implementation can send, in the order to
// offer them. A device's data may list a subset.
var Modes = []ModeInfo{
	{Name: "static", Mode: ModeStatic, Color: true},
	{Name: "breathe", Mode: ModeBreathe, Color: true, Color2: true, Speed: true},
	{Name: "cycle", Mode: ModeCycle, Speed: true},
	{Name: "rainbow", Mode: ModeRainbow, Speed: true},
	{Name: "strobe", Mode: ModeStrobe, Color: true, Speed: true},
}

// SpeedNames is every speed name SpeedFromString accepts, slowest first.
var SpeedNames = []string{"slow", "normal", "fast"}

// LookupMode returns the protocol's description of a mode name.
func LookupMode(name string) (ModeInfo, bool) {
	for _, m := range Modes {
		if m.Name == name {
			return m, true
		}
	}
	return ModeInfo{}, false
}

// ModeFromString parses a user-supplied mode name.
func ModeFromString(s string) (Mode, error) {
	switch s {
	case "static":
		return ModeStatic, nil
	case "breathe":
		return ModeBreathe, nil
	case "cycle":
		return ModeCycle, nil
	case "rainbow":
		return ModeRainbow, nil
	case "strobe":
		return ModeStrobe, nil
	}
	return 0, fmt.Errorf("unknown mode %q (valid: static breathe cycle rainbow strobe)", s)
}

// SpeedFromString parses a user-supplied speed name.
func SpeedFromString(s string) (Speed, error) {
	switch s {
	case "slow":
		return SpeedSlow, nil
	case "normal":
		return SpeedNormal, nil
	case "fast":
		return SpeedFast, nil
	}
	return 0, fmt.Errorf("unknown speed %q (valid: slow normal fast)", s)
}
