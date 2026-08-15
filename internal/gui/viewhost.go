// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// viewhost.go — what a view needs to know about the surface it was built into.
//
// The drawer and the full window host the same view *implementations*, a
// second instance rather than a second copy of the code. Exactly two things
// differ between the two surfaces, and both used to be written into the views
// as drawer facts:
//
//   - how to leave. The drawer's views carry a back button that returns to the
//     main view; the full window has a tab bar naming every page and nothing
//     to go back to, so it supplies no back and the button is not built.
//   - whether this instance is the page on screen. Every view with a refresh
//     loop asks, so it can stop when it is not being looked at. The drawer's
//     answer is "the drawer is open and its stack is showing me"; the full
//     window's is its own.
//
// Passing them in is what keeps a view from reaching for w.viewStack, which is
// the drawer's and answers nothing about the window.

// viewHost is the surface a view was built into.
type viewHost struct {
	// back leaves this view, or nil when the surface has no back affordance.
	// A nil back also suppresses the view's own header, since the button is
	// the only thing in it that does anything.
	back func()

	// current reports whether this view is the page currently on screen. It
	// must be false when the surface itself is hidden, not merely when another
	// page is selected — a refresh loop gated on the page alone keeps running
	// against a closed drawer.
	current func() bool

	// errBar is the surface's own error strip, which every view appends to its
	// focus list so a controller can dismiss a failure. Views are built lazily
	// and the bar is chrome built with the surface, so it always exists by the
	// time a view asks for it.
	errBar *errBarView

	// prefix distinguishes this surface in the focus dump. Two surfaces host
	// the same views, so without it both instances log under one name and the
	// fingerprint the dump exists to be cannot tell them apart. Empty for the
	// drawer, which is the baseline every diff is taken against.
	prefix string
}

// focusName is the name a view built into this surface logs its grid under.
func (h viewHost) focusName(view string) string { return h.prefix + view }

// No constructor produces a non-nil back any more.
//
// The drawer used to build two views into its stack — the dashboard and the
// custom profile editor — and drawerHost gave each a back button to the main
// view. Both have since moved to the full window, which navigates by tabs and
// supplies no back, so every viewHost in the tree now carries back == nil.
//
// The field stays, and so do the `if host.back != nil` branches in
// dashboardView, settingsView and customView, and customView.hosted() — which
// is `host.back == nil` and is therefore constant true. They are unreachable
// rather than wrong, and removing them is a deletion of the drawer-shaped
// layout inside the window's own editor: a separate pass with its own
// verification, not a side effect of moving a view. This comment is here so the
// next reader does not mistake them for a live second surface.
