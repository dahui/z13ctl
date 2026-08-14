// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package mainwin is the full window's page list and its opening geometry:
// which tabs exist, what each needs from the device, and how large the window
// may open on the screen it lands on.
//
// The full window is what a double press of the hardware button escalates to
// (api.EventGUIOpenFull). It is a different surface from the drawer, not a
// wider one: the drawer is a 320px column of stacked sections reached in a
// hurry, and this is a window with room to show a chart at a size worth
// reading. Both host the same view implementations — a second *instance*, not
// a second implementation, which is the property internal/gui's per-view
// structs were split out for.
//
// It is a separate package for the reason every rule on the GUI side is: the
// drawer needs cgo and GTK4 headers, so `make test` cannot compile it, and
// anything left in there is permanently unverifiable. What lives here is the
// resolution — the tab list, capability filtering, and the size clamp. What
// stays in internal/gui is the mapping from a tab ID to the view that fills it.
//
// # Capability filtering is delegated, deliberately
//
// Resolve asks internal/controls whether the device has what a tab needs,
// rather than testing api.DeviceInfo's sections itself. Two lists resolved
// against the same document by two copies of the same switch is how they come
// to disagree about what a machine can do — and the disagreement would show up
// as a tab that opens onto a view the drawer has already decided to hide.
//
// # What is deliberately not here yet
//
// The roadmap gives the full window four pages: dashboard, profile editor,
// settings, and quickbar customization. Three of them exist as views and are
// listed below. The fourth is not a stub, because a tab that opens onto an
// empty page is worse than an absent tab — it is the same trap as a device
// document declaring a capability nothing reads.
//
//   - quickbar customization needs a writer for gui.toml and a reorder
//     affordance that works on a controller. internal/controls already holds
//     the list as data, which was the prerequisite; the UI is its own piece of
//     work.
package mainwin

import (
	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/controls"
)

// Tab is one page of the full window.
type Tab struct {
	// ID is the stable name. It is the GTK stack child name and will appear in
	// the user's config once the window remembers its last page, so it is
	// never renamed.
	ID string

	// Title is the tab button's label.
	Title string

	// Requires lists every capability the device must have for this tab to be
	// worth opening. A tab whose view would be empty on this machine is not
	// shown at all — the same rule the drawer's sections follow, for the same
	// reason.
	Requires []controls.Capability
}

// The tab IDs. They match the drawer's stack child names where the same view
// fills both, so a reader grepping for "dashboard" finds one concept.
const (
	TabDashboard = "dashboard"
	TabProfiles  = "profiles"
	TabSettings  = "settings"
)

// defaultOrder is the window's pages, left to right.
//
// Telemetry leads because it is the page that justifies a large window at all:
// the drawer already shows every control at 320px, and the one thing it cannot
// show is a chart big enough to read. The roadmap says "dashboard (top)" for
// the same reason.
var defaultOrder = []Tab{
	{ID: TabDashboard, Title: "Telemetry",
		Requires: []controls.Capability{controls.CapTelemetry}},
	{ID: TabProfiles, Title: "Profiles",
		Requires: []controls.Capability{controls.CapProfiles}},
	// Settings last: firmware toggles are set-and-forget, so it is the page a
	// user visits least and the one it costs least to reach past the other
	// two. It requires toggles rather than any document section, since the
	// page is a rendering of that list and an empty one leaves nothing to
	// show — see controls.CapToggles.
	{ID: TabSettings, Title: "Settings",
		Requires: []controls.Capability{controls.CapToggles}},
}

// All returns every known tab in order. The returned slice shares nothing with
// the package's own defaults, which callers filter.
func All() []Tab {
	out := make([]Tab, len(defaultOrder))
	copy(out, defaultOrder)
	for i := range out {
		out[i].Requires = append([]controls.Capability(nil), defaultOrder[i].Requires...)
	}
	return out
}

// Resolve returns the tabs this device supports, in order.
//
// A nil document means the daemon did not answer, not that the machine has no
// capabilities, so everything is kept — the posture controls.Resolve and
// limits.FromDevice both take, and for the same reason: a window with one tab
// too many is worth having, while a window blanked by a daemon that was merely
// slow to start is not.
func Resolve(info *api.DeviceInfo) []Tab {
	all := All()
	out := make([]Tab, 0, len(all))
	for _, t := range all {
		if controls.SupportsAll(info, t.Requires) {
			out = append(out, t)
		}
	}
	return out
}

// Lookup returns the tab with the given ID.
func Lookup(id string) (Tab, bool) {
	for _, t := range All() {
		if t.ID == id {
			return t, true
		}
	}
	return Tab{}, false
}

// IDs returns every known tab ID in order.
func IDs() []string {
	out := make([]string, 0, len(defaultOrder))
	for _, t := range defaultOrder {
		out = append(out, t.ID)
	}
	return out
}

// Opening geometry.
//
// The preferred size is what the window asks for on a desktop monitor; the
// minimum is the point below which the charts stop being readable and the
// editor's sliders stop being draggable, so Fit never returns less than it
// even on a screen that cannot hold it. A window slightly larger than a tiny
// screen is something the compositor can deal with; a window shrunk to 200px
// is not something the user can.
const (
	// Raised twice at Jeff's request as the desktop design landed (900×640 →
	// 1000×700 → here): the denser layout left the smaller sizes feeling
	// cramped, and the two-column Profiles page places better when it is not
	// space-constrained. Still comfortably inside Fit's 90% margin on the
	// Z13's own panel.
	PrefWidth  = 1200
	PrefHeight = 800
	MinWidth   = 560
	MinHeight  = 420

	// screenMargin is the fraction of each axis left to the desktop, so the
	// window opens as a window rather than an unmarked fullscreen surface.
	screenMargin = 0.9
)

// Fit returns the size the full window should open at on a screen of the given
// size: the preferred size, reduced to fit with a margin, never below the
// minimum.
//
// The clamp is not decoration. Handhelds running gamescope report portrait
// panels (the OXP X2 is 1200x1920 rotated, the Ally 1920x1080 but scaled), and
// the preferred size is a desktop figure that would overhang a 1280x800 panel
// in one axis and not the other — so the axes are clamped independently rather
// than by one scale factor, which would shrink the axis that fitted perfectly
// well. A non-positive screen dimension means the caller could not measure the
// monitor, and the preferred size is the best answer available.
func Fit(screenW, screenH int) (w, h int) {
	return fitAxis(PrefWidth, MinWidth, screenW), fitAxis(PrefHeight, MinHeight, screenH)
}

func fitAxis(pref, floor, screen int) int {
	if screen <= 0 {
		return pref
	}
	avail := int(float64(screen) * screenMargin)
	size := pref
	if avail < size {
		size = avail
	}
	if size < floor {
		size = floor
	}
	return size
}
