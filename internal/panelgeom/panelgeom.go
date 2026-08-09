// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package panelgeom computes the drawer panel's rectangle and slide animation
// for the overlay backend, which draws the drawer inside a fullscreen window
// instead of letting a compositor anchor it.
//
// The layer-shell backend gets this geometry for free: it names the edges to
// anchor to and the compositor works out the rest. Compositors without
// zwlr_layer_shell_v1 (Mutter, notably) offer no such thing, and core Wayland
// does not let a client position its own window at all, so the overlay backend
// covers the output and places the panel within it — which means computing the
// rectangle by hand.
//
// It is a separate package because internal/gui needs cgo and GTK4 headers, so
// `make test` cannot even compile it; anything left in there is permanently
// unverifiable. This is arithmetic with clamping and degenerate cases, which is
// exactly the sort of thing worth pinning down.
package panelgeom

import "math"

// DefaultMarginFraction insets the panel vertically by 1/20 of the output
// height at the top and bottom, matching the layer-shell backend's 5% margins.
const DefaultMarginFraction = 20

// Rect is a panel rectangle in output-relative pixels, with the origin at the
// output's top-left corner.
type Rect struct {
	X, Y, W, H int
}

// Panel returns the drawer's at-rest rectangle on an output of monW x monH:
// right-aligned, drawerWidth wide, inset from the top and bottom by
// monH/marginFraction.
//
// Every input is treated as untrusted, because these come from monitor
// geometry that GDK reports and that a hotplug can change mid-animation. A
// degenerate output yields the zero Rect rather than a negative-sized panel
// that GTK would warn about on every allocation.
//
// The width clamp matters on small outputs: an unclamped drawerWidth wider than
// the output places X at a negative coordinate, which puts the drawer's right
// edge off-screen and its controls out of reach with no way to scroll to them.
func Panel(monW, monH, drawerWidth, marginFraction int) Rect {
	if monW <= 0 || monH <= 0 || drawerWidth <= 0 {
		return Rect{}
	}

	w := drawerWidth
	if w > monW {
		w = monW
	}

	// A non-positive fraction means "no inset" rather than a divide by zero.
	margin := 0
	if marginFraction > 0 {
		margin = monH / marginFraction
	}

	h := monH - 2*margin
	if h <= 0 {
		// The inset consumed the whole output; fall back to full height rather
		// than an invisible panel.
		margin, h = 0, monH
	}

	return Rect{X: monW - w, Y: margin, W: w, H: h}
}

// SlideX returns the panel's X coordinate at animation progress t, where t=0 is
// fully off the right edge of the output and t=1 is at rest.
//
// t is clamped rather than extrapolated: a tick callback can be handed a
// slightly out-of-range value when a frame lands after the animation's nominal
// end, and extrapolating there would overshoot the rest position by a visible
// jump on the final frame.
func SlideX(rest Rect, monW int, t float64) int {
	if t <= 0 || math.IsNaN(t) {
		return monW
	}
	if t >= 1 {
		return rest.X
	}
	return int(math.Round(float64(monW) + t*(float64(rest.X)-float64(monW))))
}

// Smoothstep eases t with the standard 3t²-2t³ curve, so the slide starts and
// ends at rest instead of stopping abruptly. Mirrors the easing the layer-shell
// backend applies to its margin animation.
func Smoothstep(t float64) float64 {
	if t <= 0 || math.IsNaN(t) {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return t * t * (3 - 2*t)
}
