// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// viewhost.go — what a view needs to know about the surface it was built into.
//
// The full window's pages are views built against a host rather than against
// the window itself: whether this instance is the page on screen, which error
// bar is its surface's, and the name it logs its focus grid under. Every view
// with a refresh loop asks the first, so it can stop when it is not being
// looked at. Passing it in is what keeps a view from reaching for
// w.viewStack, which is the drawer's and answers nothing about the window.
//
// There used to be a fourth field, back: the drawer built the dashboard and the
// profile editor into its own stack with a back button to the main view. Both
// moved to the full window, which navigates by tabs, and the drawer-shaped
// layout each view kept for that case went with the field (2026-10-09).

// viewHost is the surface a view was built into.
type viewHost struct {
	// current reports whether this view is the page currently on screen. It
	// must be false when the surface itself is hidden, not merely when another
	// page is selected — a refresh loop gated on the page alone keeps running
	// against a closed window.
	current func() bool

	// errBar is the surface's own error strip, which every view appends to its
	// focus list so a controller can dismiss a failure. The bar is chrome
	// built with the surface ahead of its pages, so it always exists by the
	// time a view asks for it.
	errBar *errBarView

	// prefix marks this surface's grids in the focus dump ("full:"), so a
	// page's line cannot be mistaken for one of the drawer's, which log with
	// no prefix and are the baseline every diff is taken against.
	prefix string
}

// focusName is the name a view built into this surface logs its grid under.
func (h viewHost) focusName(view string) string { return h.prefix + view }
