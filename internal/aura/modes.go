// modes.go — Mode and Speed types, constants, and name parsers.
package aura

import "fmt"

// Mode corresponds to g-helper's AuraMode enum.
type Mode byte

const (
	ModeStatic  Mode = 0
	ModeBreathe Mode = 1
	ModeCycle   Mode = 2
	ModeRainbow Mode = 3
	ModeStar    Mode = 4
	ModeRain    Mode = 5
	ModeStrobe  Mode = 10
	ModeComet   Mode = 11
	ModeFlash   Mode = 12
)

// Speed corresponds to g-helper's AuraSpeed enum speed byte values.
type Speed byte

const (
	SpeedSlow   Speed = 0xe1
	SpeedNormal Speed = 0xeb
	SpeedFast   Speed = 0xf5
)

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
	case "star":
		return ModeStar, nil
	case "rain":
		return ModeRain, nil
	case "strobe":
		return ModeStrobe, nil
	case "comet":
		return ModeComet, nil
	case "flash":
		return ModeFlash, nil
	}
	return 0, fmt.Errorf("unknown mode %q (valid: static breathe cycle rainbow star rain strobe comet flash)", s)
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
