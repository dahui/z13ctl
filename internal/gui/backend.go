// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

import "github.com/diamondburned/gotk4/pkg/gtk/v4"

// Backend abstracts display-mode-specific window management.
// Implementations: layershell.Backend (Wayland compositors implementing
// zwlr_layer_shell_v1 — KWin, Hyprland, Sway), overlay.Backend (a fullscreen
// transparent click-through window for those that do not, GNOME/Mutter above
// all), and gamescope.Backend (X11 overlay for Steam Gaming Mode).
type Backend interface {
	// Configure sets up the window for this display mode. Must be called
	// before the window is realized.
	// isVisible reports whether the drawer is currently on-screen.
	// onDismiss is called when the backend wants to hide the drawer
	// (focus loss in layer-shell, backdrop click in gamescope).
	Configure(isVisible func() bool, onDismiss func())

	// WrapContent optionally wraps the drawer widget for this display mode.
	// Layer-shell returns it as-is; gamescope wraps it in a fullscreen
	// container with a click-to-dismiss backdrop; overlay wraps it in a
	// fullscreen GtkFixed so the drawer can be slid in from the right edge.
	WrapContent(drawer gtk.Widgetter) gtk.Widgetter

	// Show makes the drawer visible (animation, atom toggle, etc).
	Show()

	// Hide hides the drawer (animation, atom toggle, etc).
	Hide()

	// Scale returns the factor the drawer's CSS pixel sizes are multiplied by.
	// Layer-shell and overlay return 1.0 — GTK handles scaling there. Gamescope scales its
	// own CSS because GDK_SCALE would be applied twice, and anything drawn
	// directly rather than styled has to apply the same factor by hand or it
	// stays at its 1x size while everything around it grows. Valid after
	// Configure has realized the window.
	Scale() float64
}

// fullSurfaceHost is the optional half of Backend, implemented only by a
// backend that must host the full window *inside its own surface* rather than
// as a second toplevel.
//
// Gamescope is the only one, and the reason is narrow: exactly one window
// carries the STEAM_OVERLAY atom and gamescope's GetPossibleFocusWindows()
// skips windows flagged isOverlay, so a second gtk.Window is simply never
// composited. That is a fact about second *windows* — it says nothing about
// screen space. The backend's own window is already fullscreen, so the full
// window there is a different layout of a surface we already own. HHD works
// exactly this way: its sidebar and its larger settings view are one Electron
// surface re-laying-out its contents, which is also why its menus work where
// GtkDropDown does not.
//
// It is an optional interface rather than three methods on Backend because
// layer-shell and overlay have nothing to implement — they use the real
// toplevel, which is the better surface where it works (its own size, its own
// place in the window list). Two no-op implementations would suggest a choice
// where there is none. Window.fullHost is the single place that asks.
//
// The drawer's own view stack is deliberately *not* this seam: it lives inside
// the 320px panel WrapContent sizes, so a page added to it would be a 320px
// "full window". The swap has to happen above that panel, which is what
// SetFullChild does.
type fullSurfaceHost interface {
	// SetFullChild installs the full window's content into the backend's
	// surface. Called once, the first time the full window is opened; the
	// backend keeps it and shows or hides it on demand.
	SetFullChild(child gtk.Widgetter)

	// ShowFull swaps the surface between the quickbar layout (false) and the
	// full-window layout (true). A backend whose SetFullChild has not been
	// called must treat ShowFull(true) as a no-op rather than blanking itself.
	ShowFull(show bool)
}

// fullHost reports whether this backend hosts the full window inside its own
// surface, and returns the interface for doing so. A nil result means the
// ordinary path: build a real toplevel.
func (w *Window) fullHost() fullSurfaceHost {
	host, ok := w.backend.(fullSurfaceHost)
	if !ok {
		return nil
	}
	return host
}
