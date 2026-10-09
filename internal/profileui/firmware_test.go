// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"slices"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

// z13 is what a client with no device document falls back to.
var z13 = profileui.FirmwareFrom(nil)

// The selector offers what the device says, in its order, with its labels —
// not the Z13's three.
func TestFirmwareFromTheDevice(t *testing.T) {
	fw := profileui.FirmwareFrom(&api.DeviceInfo{Profiles: &api.ProfileInfo{
		Names:   []string{"low-power", "balanced", "balanced-performance", "performance"},
		Entries: []api.ProfileEntry{{Name: "low-power", Label: "Eco"}},
	}})
	if got := fw.Names(); !slices.Equal(got, []string{"low-power", "balanced", "balanced-performance", "performance"}) {
		t.Errorf("Names = %v, want the device's four in its order", got)
	}
	if got := fw.Label("low-power"); got != "Eco" {
		t.Errorf("device label = %q, want Eco", got)
	}
	// No entry for it: the kernel vocabulary's spelling, not "Balanced-performance".
	if got := fw.Label("balanced-performance"); got != "Balanced Performance" {
		t.Errorf("fallback label = %q, want Balanced Performance", got)
	}
	rows := profileui.StockRows(nil, fw)
	if len(rows) != 4 || rows[0].Name != "low-power" || rows[0].Label != "Eco" {
		t.Errorf("StockRows = %+v, want the device's rows", rows)
	}
	targets := profileui.TargetOptions(nil, fw)
	if !slices.Contains(targets, "balanced-performance") || slices.Contains(targets, "quiet") {
		t.Errorf("TargetOptions = %v, want the device's names and not the Z13's", targets)
	}
}

// A daemon older than entries sends names alone.
func TestFirmwareFromNamesOnly(t *testing.T) {
	fw := profileui.FirmwareFrom(&api.DeviceInfo{Profiles: &api.ProfileInfo{Names: []string{"quiet", "max-power"}}})
	if got := fw.Label("max-power"); got != "Max Power" {
		t.Errorf("label = %q, want Max Power", got)
	}
}

func TestFirmwareFallbackIsTheZ13(t *testing.T) {
	if got := z13.Names(); !slices.Equal(got, []string{"quiet", "balanced", "performance"}) {
		t.Errorf("fallback = %v", got)
	}
}

func TestLabelUsesKernelSpellings(t *testing.T) {
	for name, want := range map[string]string{
		"low-power": "Low Power", "quiet": "Quiet", "custom": "Custom", "my-profile": "my-profile",
	} {
		if got := profileui.Label(name); got != want {
			t.Errorf("Label(%q) = %q, want %q", name, got, want)
		}
	}
}
