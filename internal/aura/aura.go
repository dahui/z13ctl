// Package aura implements the ASUS Aura HID protocol for the ROG Flow Z13.
//
// Protocol reverse-engineered from g-helper (MIT):
//
//	app/USB/AsusHid.cs — device enumeration and I/O routing
//	app/USB/Aura.cs    — packet construction and command sequencing
//
// All writes go to Report ID 0x5d (auraID) as 64-byte output reports.
// The standard flow is: Init → SetPower → SetBrightness → SetMode → commit.
//
// File layout:
//
//	modes.go — Mode/Speed types, constants, ModeFromString, SpeedFromString
//	aura.go  — protocol implementation: Init, SetPower, SetBrightness, SetMode, Apply, TurnOff
package aura

import "fmt"

// Writer is the interface required by all Aura protocol functions.
// *hid.Device satisfies this interface.
type Writer interface {
	Write(data []byte) error
}

// auraID is the HID Report ID for all Aura output reports (0x5d).
const auraID = 0x5d

// z13Zones are the zone bytes for the 2025 ROG Flow Z13, in the order g-helper
// addresses them. Zone 0 is the keyboard backlight; zone 1 is the edge lightbar.
// Each zone requires its own 0xB3 SetMode packet — there is no "all zones" shortcut
// in the protocol; zone=0 in the packet means keyboard only.
var z13Zones = []uint8{0, 1}

// Init sends the ASUS Aura initialization sequence.
// The Z13-specific Dynamic Lighting Init packet (0xC0 0x03 0x01) is always sent
// because the 2025 ROG Flow Z13 requires it to activate the lightbar.
//
// Source: Aura.cs Init() — lines 288–295
func Init(d Writer) error {
	// Packet 1: wake/init
	if err := d.Write([]byte{auraID, 0xB9}); err != nil {
		return fmt.Errorf("init packet 1: %w", err)
	}

	// Packet 2: "]ASUS Tech.Inc." as raw ASCII bytes.
	// The ']' character = 0x5D = auraID, so it IS the report ID — do NOT prepend one.
	// Source: Aura.cs Init() uses Encoding.ASCII.GetBytes("]ASUS Tech.Inc.") directly.
	if err := d.Write([]byte("]ASUS Tech.Inc.")); err != nil {
		return fmt.Errorf("init packet 2: %w", err)
	}

	// Packet 3: mode config header
	if err := d.Write([]byte{auraID, 0x05, 0x20, 0x31, 0x00, 0x1A}); err != nil {
		return fmt.Errorf("init packet 3: %w", err)
	}

	// Packet 4: Z13 Dynamic Lighting Init (required for lightbar on Z13)
	if err := d.Write([]byte{auraID, 0xC0, 0x03, 0x01}); err != nil {
		return fmt.Errorf("init packet 4 (Z13 dyn lighting): %w", err)
	}

	return nil
}

// SetPower enables or disables the keyboard/lightbar/lid lighting.
// On the Z13, bar, lid, and logo flags are merged together (Aura.cs lines 437–443).
//
// Source: Aura.cs AuraPowerMessage() + ApplyPower() Z13 branch
func SetPower(d Writer, on bool) error {
	var keyb, bar, lid, rear byte
	if on {
		// Enable awake + boot states for all zones.
		// bar byte bits: bit0=Awake, bit1=Boot, bit2=Awake(dup), bit3=Sleep, bit4=Shutdown
		keyb = 0xFF
		bar = 0x1F
		lid = 0xFF
		rear = 0xFF
	}
	// 0xFF terminator byte is always present per AuraPowerMessage()
	return d.Write([]byte{auraID, 0xBD, 0x01, keyb, bar, lid, rear, 0xFF})
}

// SetBrightness sets keyboard backlight brightness (0=off, 1=low, 2=medium, 3=high).
//
// Source: Aura.cs DirectBrightness() / ApplyBrightness() — line 340
func SetBrightness(d Writer, level uint8) error {
	if level > 3 {
		level = 3
	}
	return d.Write([]byte{auraID, 0xBA, 0xC5, 0xC4, level})
}

// SetMode builds and sends the Aura color/mode packet for a specific zone,
// followed by the commit sequence. zone is a literal zone byte (0=keyboard,
// 1=lightbar); it is not a wildcard.
//
// For ModeBreathe, color2 (r2, g2, b2) provides the second color.
// For modes that don't use a second color, pass 0, 0, 0 for r2/g2/b2.
//
// Source: Aura.cs AuraMessage() — lines 266–284, plus MESSAGE_SET + MESSAGE_APPLY
func SetMode(d Writer, zone uint8, mode Mode, r, g, b, r2, g2, b2 uint8, speed Speed) error {
	// Random-color flag: 0xFF when primary color is all-zero (device picks color),
	// 0x01 for Breathe (enables dual-color), 0x00 otherwise.
	var randFlag byte
	if r == 0 && g == 0 && b == 0 {
		randFlag = 0xFF
	} else if mode == ModeBreathe {
		randFlag = 0x01
	}

	msg := []byte{
		auraID,
		0xB3,
		zone,
		byte(mode),
		r, g, b,
		byte(speed),
		0x00, // direction
		randFlag,
		r2, g2, b2,
	}
	if err := d.Write(msg); err != nil {
		return fmt.Errorf("SetMode write: %w", err)
	}

	return commit(d)
}

// commit sends MESSAGE_SET then MESSAGE_APPLY to latch the pending Aura command.
//
// Source: Aura.cs MESSAGE_SET (0xb5) and MESSAGE_APPLY (0xb4) — lines 70–71
func commit(d Writer) error {
	if err := d.Write([]byte{auraID, 0xB5, 0x00, 0x00, 0x00}); err != nil {
		return fmt.Errorf("commit MESSAGE_SET: %w", err)
	}
	if err := d.Write([]byte{auraID, 0xB4}); err != nil {
		return fmt.Errorf("commit MESSAGE_APPLY: %w", err)
	}
	return nil
}

// Apply performs the full setup: Init, power on, brightness, then set mode.
// Both Z13 zones are always addressed (keyboard=0, lightbar=1); each physical
// device only responds to the zone it owns and ignores the other.
// Use hid.FindDevice with "keyboard" or "lightbar" to target a single device.
// This is the primary entry point for setting lighting state.
func Apply(d Writer, mode Mode, r, g, b, r2, g2, b2 uint8, speed Speed, brightness uint8) error {
	if err := Init(d); err != nil {
		return err
	}
	if err := SetPower(d, true); err != nil {
		return err
	}
	if err := SetBrightness(d, brightness); err != nil {
		return err
	}
	for _, z := range z13Zones {
		if err := SetMode(d, z, mode, r, g, b, r2, g2, b2, speed); err != nil {
			return err
		}
	}
	return nil
}

// TurnOff disables all lighting zones.
func TurnOff(d Writer) error {
	if err := Init(d); err != nil {
		return err
	}
	if err := SetPower(d, false); err != nil {
		return err
	}
	return SetBrightness(d, 0)
}
