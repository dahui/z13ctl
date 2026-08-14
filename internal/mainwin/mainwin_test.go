// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package mainwin_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/mainwin"
)

// z13 is the capability document the one shipping device produces: every
// section present.
func z13() *api.DeviceInfo {
	return &api.DeviceInfo{
		ID:        "asus-rog-flow-z13-2025",
		Model:     "GZ302",
		Fans:      &api.FanInfo{Points: 8, TempMin: 30, TempMax: 100, PWMMax: 255},
		Power:     &api.PowerInfo{TDPMin: 5, TDPMaxSafe: 75, TDPMaxForced: 93},
		Profiles:  &api.ProfileInfo{Names: []string{"quiet", "balanced", "performance"}},
		Lighting:  &api.LightingInfo{Zones: []string{"keyboard", "lightbar"}},
		Battery:   &api.BatteryInfo{ChargeLimit: true, Health: true},
		Telemetry: &api.TelemetryInfo{HistorySeconds: 300},
		Undervolt: &api.UndervoltInfo{Min: -40, Max: 0},
	}
}

func ids(tabs []mainwin.Tab) []string {
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, t.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTelemetryLeads pins the order. It is a decision, not an accident: the
// drawer already shows every control at 320px, and the chart is the only thing
// a large window offers that the drawer cannot.
func TestTelemetryLeads(t *testing.T) {
	t.Parallel()
	got := ids(mainwin.Resolve(z13()))
	want := []string{mainwin.TabDashboard, mainwin.TabProfiles}
	if !equal(got, want) {
		t.Errorf("Resolve(z13) = %v, want %v", got, want)
	}
}

// TestNilDocumentKeepsEverything is the fallback posture: a daemon that did not
// answer must not blank the window, because "unknown" is not "unsupported".
// Same rule as controls.Resolve and limits.FromDevice.
func TestNilDocumentKeepsEverything(t *testing.T) {
	t.Parallel()
	got := ids(mainwin.Resolve(nil))
	if !equal(got, mainwin.IDs()) {
		t.Errorf("Resolve(nil) = %v, want every tab %v", got, mainwin.IDs())
	}
}

// TestATabWithNothingToShowIsDropped is the honesty rule one layer up from the
// device document: a machine reporting no telemetry source gets no telemetry
// tab, rather than a tab onto an empty page.
func TestATabWithNothingToShowIsDropped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mut  func(*api.DeviceInfo)
		want []string
	}{
		{"no telemetry", func(d *api.DeviceInfo) { d.Telemetry = nil },
			[]string{mainwin.TabProfiles}},
		{"no profiles", func(d *api.DeviceInfo) { d.Profiles = nil },
			[]string{mainwin.TabDashboard}},
		{"neither", func(d *api.DeviceInfo) { d.Telemetry, d.Profiles = nil, nil },
			nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := z13()
			tc.mut(dev)
			if got := ids(mainwin.Resolve(dev)); !equal(got, tc.want) {
				t.Errorf("Resolve = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveDoesNotMutateTheDefaults guards the copy in All: Resolve is called
// on every window open, and a caller that reordered or trimmed the returned
// slice in place would change what every later call answers.
func TestResolveDoesNotMutateTheDefaults(t *testing.T) {
	t.Parallel()
	first := mainwin.All()
	first[0].Title = "clobbered"
	first[0].Requires = nil
	second := mainwin.All()
	if second[0].Title == "clobbered" {
		t.Error("All() shares its Tab values with the package defaults")
	}
	if len(second[0].Requires) == 0 {
		t.Error("All() shares its Requires slice with the package defaults")
	}
}

// TestEveryTabIsLookupable keeps the ID list and the table in step: an ID that
// IDs reports but Lookup cannot find is a stack child name with no tab behind
// it.
func TestEveryTabIsLookupable(t *testing.T) {
	t.Parallel()
	for _, id := range mainwin.IDs() {
		tab, ok := mainwin.Lookup(id)
		if !ok {
			t.Errorf("Lookup(%q) = not found, but IDs() reports it", id)
			continue
		}
		if tab.Title == "" {
			t.Errorf("tab %q has no title", id)
		}
		if len(tab.Requires) == 0 {
			t.Errorf("tab %q requires nothing, so it can never be filtered out", id)
		}
	}
	if _, ok := mainwin.Lookup("nope"); ok {
		t.Error("Lookup(nope) = found")
	}
}

// TestFitNeverExceedsTheScreen is the property that matters: whatever the
// panel, the window opens inside it. A window taller than the screen on a
// handheld has its bottom row of buttons somewhere the user cannot reach.
func TestFitNeverExceedsTheScreen(t *testing.T) {
	t.Parallel()
	screens := [][2]int{
		{3840, 2160}, // desktop 4K
		{2560, 1600}, // the Z13's own panel
		{1920, 1080},
		{1280, 800},  // Steam Deck class
		{1200, 1920}, // portrait handheld panel, unrotated
		{800, 1280},
		{640, 480},
	}
	for _, s := range screens {
		w, h := mainwin.Fit(s[0], s[1])
		if w > s[0] || h > s[1] {
			t.Errorf("Fit(%d,%d) = %dx%d, which does not fit", s[0], s[1], w, h)
		}
		if w < mainwin.MinWidth || h < mainwin.MinHeight {
			t.Errorf("Fit(%d,%d) = %dx%d, below the %dx%d minimum",
				s[0], s[1], w, h, mainwin.MinWidth, mainwin.MinHeight)
		}
	}
}

// TestFitClampsAxesIndependently is why Fit is not one scale factor. A portrait
// panel is narrow and tall: scaling both axes by the width's shortfall would
// shrink a height that fitted perfectly well, and the chart would lose rows for
// no reason.
func TestFitClampsAxesIndependently(t *testing.T) {
	t.Parallel()
	// Narrow but very tall: width must come down, height must not.
	w, h := mainwin.Fit(800, 1920)
	if w >= mainwin.PrefWidth {
		t.Errorf("Fit(800,1920) width = %d, want less than the preferred %d", w, mainwin.PrefWidth)
	}
	if h != mainwin.PrefHeight {
		t.Errorf("Fit(800,1920) height = %d, want the preferred %d — the screen is tall enough",
			h, mainwin.PrefHeight)
	}
}

// TestFitWithoutAScreenAsksForThePreferredSize: a caller that could not measure
// the monitor gets the desktop figure, not the minimum. Guessing small would
// make every unmeasurable session open a cramped window.
func TestFitWithoutAScreenAsksForThePreferredSize(t *testing.T) {
	t.Parallel()
	for _, s := range [][2]int{{0, 0}, {-1, -1}, {0, 1080}} {
		w, h := mainwin.Fit(s[0], s[1])
		if s[0] <= 0 && w != mainwin.PrefWidth {
			t.Errorf("Fit(%d,%d) width = %d, want %d", s[0], s[1], w, mainwin.PrefWidth)
		}
		if s[1] <= 0 && h != mainwin.PrefHeight {
			t.Errorf("Fit(%d,%d) height = %d, want %d", s[0], s[1], h, mainwin.PrefHeight)
		}
	}
}

// TestTheMinimumFitsTheSmallestScreenWeClaimToSupport ties the two constants
// together. If a minimum is ever raised past a real panel, Fit starts returning
// a window that overhangs it — the test above would then fail on that screen,
// so this states the dependency where the constants are.
func TestTheMinimumFitsTheSmallestScreenWeClaimToSupport(t *testing.T) {
	t.Parallel()
	const smallestW, smallestH = 640, 480
	if mainwin.MinWidth > smallestW || mainwin.MinHeight > smallestH {
		t.Errorf("minimum %dx%d does not fit a %dx%d screen",
			mainwin.MinWidth, mainwin.MinHeight, smallestW, smallestH)
	}
}
