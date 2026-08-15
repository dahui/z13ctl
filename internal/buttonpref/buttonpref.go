// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package buttonpref is which surface the Armoury Crate button opens.
//
// The daemon reports every press as gui-toggle and adds gui-open-full when it
// sees a second press inside the double-press window (internal/daemon/button.go).
// It says *that a press happened*, not what to show — which surface each press
// raises is the client's business, and this package is where that choice lives.
//
// There are two surfaces and two gestures, so there are exactly two arrangements
// and one of them is the other reversed. The preference is therefore a single
// value — which surface a *single* press opens — and the double press gets
// whichever is left (Jeff, 2026-08-14). Storing both would let them be set to
// the same surface, which is a state with no way back to the other one.
//
// It is a separate package for the reason every rule on the GUI side is:
// internal/gui needs cgo and GTK4 headers, so `make test` cannot compile it and
// anything left in there is permanently unverifiable. What lives here is the
// value, its parsing, its labels and the mapping; internal/gui shows a surface.
package buttonpref

import "strings"

// Surface is one of the two things the button can raise.
type Surface string

// The two surfaces. The values are what config.toml stores, so they are part of
// the on-disk format and must not be renamed — the GUI writes them back.
const (
	// Quickbar is the 320px overlay drawer.
	Quickbar Surface = "quickbar"

	// Window is the full window (its own toplevel on a desktop; the same
	// surface re-laid-out under gamescope).
	Window Surface = "window"
)

// Default is the arrangement voltaire has always had, and stays the default
// because the quickbar is the surface designed to be reached in a hurry: a
// press that lands on it costs nothing if it was a mistake, where one that
// raises a full window over a running game is disruptive.
func Default() Surface { return Quickbar }

// Parse reads a stored or configured value.
//
// It returns ok=false for anything unrecognized *and still returns a usable
// surface*, on the rule panelgeom.ParseEdge established: a bad config costs a
// warning, never a button that does nothing. Matching folds case and surrounding
// space because this is a hand-editable file.
func Parse(s string) (Surface, bool) {
	switch Surface(strings.ToLower(strings.TrimSpace(s))) {
	case Quickbar:
		return Quickbar, true
	case Window:
		return Window, true
	}
	return Default(), false
}

// Other is the surface this one is not — what a double press opens when a
// single press opens s.
func (s Surface) Other() Surface {
	if s == Window {
		return Quickbar
	}
	return Window
}

// Label is the surface's name in the UI.
func (s Surface) Label() string {
	if s == Window {
		return "Full window"
	}
	return "Quickbar"
}

// Options are the choices to offer, in display order: the historical default
// first, so the list reads as "the usual way, or the other way".
func Options() []Surface { return []Surface{Quickbar, Window} }

// The settings row's own text. It lives here rather than as a GTK literal for
// the same reason api.ToggleInfo.Description is device data: the page that
// renders it should not be the place a behaviour is described, or the
// description and the behaviour drift apart in separate files.
const (
	RowLabel = "Armoury Crate button"

	// RowDescription says only what the setting is *for*. What each choice
	// does is Summary's job, because that sentence has to name the current
	// selection to be worth reading at all.
	RowDescription = "Which surface a single press of the hardware button opens."
)

// Summary describes the arrangement a choice produces, naming both gestures.
//
// Both halves are stated even though one implies the other: a user reading
// "one press opens the quickbar" still has to be told what the second press
// does, and the double press is the part nobody discovers on their own.
func Summary(single Surface) string {
	return "One press opens the " + strings.ToLower(single.Label()) +
		"; a double press opens the " + strings.ToLower(single.Other().Label()) + "."
}

// OpenGesture names the press that raises want, given which surface a single
// press opens. It is a sentence-initial phrase, so a caller can put it in front
// of "to open the profile editor".
//
// It exists because a surface that tells the user where to go has to know how
// this machine is configured to get there: the double press is the half nobody
// discovers on their own, and it is the *other* gesture once the preference is
// reversed. Naming it as "double press" in a GTK literal would be right for the
// default and wrong for anyone who swapped them.
func OpenGesture(single, want Surface) string {
	if single == want {
		return "Press the Armoury Crate button"
	}
	return "Press the Armoury Crate button twice"
}
