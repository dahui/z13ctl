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
