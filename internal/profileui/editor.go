// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// editor.go — which cards the profile editor builds, and the words that
// describe what a custom profile holds. Both follow the device: a machine with
// no fan curve gets no FAN CURVE card, and no hint promises one.

import (
	"fmt"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/limits"
)

// EditorSections says which cards the profile editor builds for a device.
type EditorSections struct {
	Power     bool // the POWER card: the device has settable power limits
	Fans      bool // the FAN CURVE card
	Undervolt bool // the undervolt block inside POWER's advanced view
}

// SectionsFor reads the editor's cards from the device's limits. The
// undervolt also needs ryzen_smu present at runtime (State.UndervoltAvailable);
// that is a property of the running machine, checked where the block is shown,
// while this says whether the device has the capability at all.
func SectionsFor(l limits.Limits) EditorSections {
	// The undervolt lives inside POWER's advanced view, so without power
	// limits there is nowhere to put it. (POWER's Advanced toggle is offered
	// whenever the card is: even a single limit has a forced range above the
	// basic slider's cap.)
	return EditorSections{
		Power:     l.HasPower,
		Fans:      l.HasFans,
		Undervolt: l.HasPower && l.HasUndervolt,
	}
}

// Any reports whether the editor has anything to edit.
func (s EditorSections) Any() bool { return s.Power || s.Fans || s.Undervolt }

// ResetAllHint is the Reset All button's tooltip, naming what it clears.
func ResetAllHint(s EditorSections) string {
	parts := s.held(true)
	if len(parts) == 0 {
		return "Clear this profile's settings"
	}
	return "Clear the " + joinAnd(parts) + " from this profile"
}

// CustomButtonHint is the tooltip of the control that opens the custom
// profiles, naming what one can hold.
func CustomButtonHint(s EditorSections) string {
	parts := s.held(false)
	if len(parts) == 0 {
		return "Custom profiles"
	}
	return "Custom profiles: " + joinAnd(parts)
}

// held lists what a custom profile holds on this device. The two hints have
// always named them in different orders — Reset All leads with the fan curve —
// and keep doing so.
func (s EditorSections) held(fansFirst bool) []string {
	var out []string
	if s.Fans && fansFirst {
		out = append(out, "fan curve")
	}
	if s.Power {
		out = append(out, "power limits")
	}
	if s.Fans && !fansFirst {
		out = append(out, "fan curve")
	}
	if s.Undervolt {
		out = append(out, "undervolt")
	}
	return out
}

// joinAnd joins "a", "a and b", "a, b and c".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// UndervoltNote is the caution above the undervolt slider. It names no
// profile: every custom profile can carry an offset, not one called Custom.
func UndervoltNote() string {
	return "Undervolt offsets are only active while a custom profile is selected. " +
		"Switching to a firmware profile resets them to 0. Unstable values may cause crashes."
}

// UndervoltValueText formats an offset for the slider's readout. The name is
// on the same line, so the value stands bare ("-20", "0 (stock)"), with the
// device's unit where it has one ("-20 mV").
func UndervoltValueText(val int, unit string) string {
	if val == 0 {
		return "0 (stock)"
	}
	if unit == "" {
		return fmt.Sprintf("%d", val)
	}
	return fmt.Sprintf("%d %s", val, unit)
}

// HeaderLine is the drawer header's telemetry line: the power source when it
// is known, then temperature and fan speed, each only when the device reports
// it. A temperature of 0 is no reading (0°C is not a plausible package
// temperature), so a device without one shows no "0°C".
func HeaderLine(s *api.State) string {
	if s == nil {
		return ""
	}
	var parts []string
	if p := SourceLabel(s); p != "" {
		parts = append(parts, p)
	}
	if s.Temperature != 0 {
		parts = append(parts, fmt.Sprintf("%d°C", s.Temperature))
	}
	if f := FanRPMText(s); f != "" {
		parts = append(parts, f)
	}
	return strings.Join(parts, " · ")
}
