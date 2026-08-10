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

import (
	"fmt"
	"math"
	"strings"
)

// DefaultMarginFraction insets the panel vertically by 1/20 of the output
// height at the top and bottom, matching the layer-shell backend's 5% margins.
const DefaultMarginFraction = 20

// Edge is the screen edge the drawer is anchored to.
//
// Only the two vertical edges exist. Top and bottom are not an anchor change:
// the drawer is a fixed-width column of stacked sections, and putting that on a
// horizontal edge means laying every section out along the other axis — a
// different panel, not a moved one. ParseEdge names them anyway so a user who
// tries gets told that rather than "unknown edge"; focusgrid.Horizontal is
// already written and tested for the day the content work lands.
type Edge int

const (
	// EdgeRight is the drawer's original and default position.
	EdgeRight Edge = iota
	// EdgeLeft mirrors it to the left-hand edge.
	EdgeLeft
)

// String returns the config spelling of the edge.
func (e Edge) String() string {
	if e == EdgeLeft {
		return "left"
	}
	return "right"
}

// ParseEdge reads an edge from its config spelling. An empty string is the
// default (right), since an absent setting is not an error.
func ParseEdge(s string) (Edge, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "right":
		return EdgeRight, nil
	case "left":
		return EdgeLeft, nil
	case "top", "bottom":
		return EdgeRight, fmt.Errorf(
			"edge %q needs a horizontal layout, which the drawer does not have yet; using right", s)
	default:
		return EdgeRight, fmt.Errorf("unknown edge %q (want left or right); using right", s)
	}
}

// Rect is a panel rectangle in output-relative pixels, with the origin at the
// output's top-left corner.
type Rect struct {
	X, Y, W, H int
}

// Panel returns the drawer's at-rest rectangle on an output of monW x monH:
// aligned to edge, drawerWidth wide, inset from the top and bottom by
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
func Panel(monW, monH, drawerWidth, marginFraction int, edge Edge) Rect {
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

	x := monW - w
	if edge == EdgeLeft {
		x = 0
	}
	return Rect{X: x, Y: margin, W: w, H: h}
}

// HiddenX is the panel's X coordinate when fully off-screen past edge: one
// panel width beyond the output on the left, the output's own width on the
// right. It is separate from SlideX so that SlideX stays plain interpolation
// and only one function knows which way "away" is.
func HiddenX(rest Rect, monW int, edge Edge) int {
	if edge == EdgeLeft {
		return -rest.W
	}
	return monW
}

// HasNeighbor reports whether any of others sits beyond self's edge and
// overlaps it vertically — i.e. whether sliding the panel off that edge would
// bleed onto another output.
//
// KWin does not clip a layer surface's overflow to its assigned output, so the
// backend fades in place instead of sliding when this is true. The rule lives
// here rather than in the backend because it is geometry with an overlap test
// in it, and because it had to grow an edge the day the drawer could move: the
// original asked only about the right.
//
// A monitor exactly abutting the edge counts — that is the adjacency this is
// about. One that merely touches corner-to-corner does not, since the vertical
// spans must genuinely overlap for any bleed to be visible.
func HasNeighbor(self Rect, others []Rect, edge Edge) bool {
	for _, o := range others {
		beyond := o.X >= self.X+self.W
		if edge == EdgeLeft {
			beyond = o.X+o.W <= self.X
		}
		if beyond && o.Y < self.Y+self.H && o.Y+o.H > self.Y {
			return true
		}
	}
	return false
}

// SlideX returns the panel's X coordinate at animation progress t, where t=0 is
// hiddenX (see HiddenX) and t=1 is at rest.
//
// t is clamped rather than extrapolated: a tick callback can be handed a
// slightly out-of-range value when a frame lands after the animation's nominal
// end, and extrapolating there would overshoot the rest position by a visible
// jump on the final frame.
func SlideX(rest Rect, hiddenX int, t float64) int {
	if t <= 0 || math.IsNaN(t) {
		return hiddenX
	}
	if t >= 1 {
		return rest.X
	}
	return int(math.Round(float64(hiddenX) + t*(float64(rest.X)-float64(hiddenX))))
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
