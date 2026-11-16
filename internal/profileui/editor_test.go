// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/devicefixture"
	"github.com/dahui/voltaire/v2/internal/limits"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

// On the Z13 every card is built and the hints read as they always have.
func TestEditorOnTheZ13(t *testing.T) {
	s := profileui.SectionsFor(limits.DefaultLimits())
	if s != (profileui.EditorSections{Power: true, Fans: true, Undervolt: true}) {
		t.Errorf("SectionsFor(Z13) = %+v, want every card", s)
	}
	if got, want := profileui.ResetAllHint(s), "Clear the fan curve, power limits and undervolt from this profile"; got != want {
		t.Errorf("ResetAllHint = %q, want %q", got, want)
	}
	if got, want := profileui.CustomButtonHint(s), "Custom profiles: power limits, fan curve and undervolt"; got != want {
		t.Errorf("CustomButtonHint = %q, want %q", got, want)
	}
}

// A device with one power limit and nothing else: a POWER card, no fan card,
// no undervolt, and hints that promise neither.
func TestEditorOnADeviceWithOneLimit(t *testing.T) {
	s := profileui.SectionsFor(limits.FromDevice(devicefixture.Synthetic()))
	if s != (profileui.EditorSections{Power: true}) {
		t.Errorf("SectionsFor(synthetic) = %+v, want POWER alone", s)
	}
	if got, want := profileui.ResetAllHint(s), "Clear the power limits from this profile"; got != want {
		t.Errorf("ResetAllHint = %q, want %q", got, want)
	}
	if got, want := profileui.CustomButtonHint(s), "Custom profiles: power limits"; got != want {
		t.Errorf("CustomButtonHint = %q, want %q", got, want)
	}
}

func TestEditorUndervoltNeedsThePowerCard(t *testing.T) {
	l := limits.FromDevice(devicefixture.Synthetic())
	l.HasUndervolt = true
	if s := profileui.SectionsFor(l); !s.Undervolt {
		t.Errorf("undervolt with power: %+v, want it built", s)
	}
	// The undervolt lives inside POWER; without power there is nowhere to put it.
	l.HasPower = false
	if s := profileui.SectionsFor(l); s.Undervolt || s.Any() {
		t.Errorf("no power card: %+v, want nothing", s)
	}
	if got := profileui.ResetAllHint(profileui.EditorSections{}); got != "Clear this profile's settings" {
		t.Errorf("empty ResetAllHint = %q", got)
	}
}

func TestUndervoltText(t *testing.T) {
	for _, tt := range []struct {
		val  int
		unit string
		want string
	}{
		{0, "", "0 (stock)"},
		{-20, "", "-20"},
		{-20, "mV", "-20 mV"},
		{0, "mV", "0 (stock)"},
	} {
		if got := profileui.UndervoltValueText(tt.val, tt.unit); got != tt.want {
			t.Errorf("UndervoltValueText(%d, %q) = %q, want %q", tt.val, tt.unit, got, tt.want)
		}
	}
}

func TestHeaderLineShowsOnlyWhatIsReported(t *testing.T) {
	if got := profileui.HeaderLine(&api.State{Temperature: 52, FanRPM: 2400}); got != "52°C · 2400 RPM" {
		t.Errorf("HeaderLine(temp, fan) = %q", got)
	}
	if got := profileui.HeaderLine(&api.State{}); got != "" {
		t.Errorf("HeaderLine(nothing) = %q, want empty (no 0°C)", got)
	}
	if got := profileui.HeaderLine(nil); got != "" {
		t.Errorf("HeaderLine(nil) = %q", got)
	}
}
