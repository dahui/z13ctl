// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package popupgeom places an in-surface popup — a dropdown list or an anchored
// hint — relative to the widget that opened it, inside the drawer panel.
//
// The drawer cannot use GTK's own popups: every GtkPopover (and so every
// GtkDropDown and tooltip) is a separate GdkSurface, which gamescope will not
// composite as an input-receiving window. Popups are therefore ordinary overlay
// children of the panel's GtkOverlay, and something has to compute their
// rectangle: anchored below the trigger, flipped above it when there is no room,
// shortened into the larger side when there is no room either way, and always
// inside the panel.
//
// It is a separate package because internal/gui needs cgo and GTK4 headers, so
// `make test` cannot even compile it; anything left in there is permanently
// unverifiable. The GTK side measures widgets and applies the returned Rect in
// its get-child-position handler; every decision is made here. Mirrors the
// panelgeom ↔ overlay backend split.
package popupgeom

// Rect is a rectangle in panel-relative pixels, with the origin at the panel's
// top-left corner.
type Rect struct {
	X, Y, W, H int
}

// Align says how the popup's horizontal edge relates to the anchor's before
// clamping into bounds.
type Align int

// Alignment of the popup against its anchor.
const (
	AlignStart  Align = iota // left edges flush
	AlignCenter              // centered on the anchor
	AlignEnd                 // right edges flush
)

// Request describes one placement. All rectangles are panel-relative.
//
// Every input is treated as untrusted: the anchor comes from a widget that may
// be scrolled partly or wholly out of view, the sizes from GTK measurement, and
// the gaps from scaled theme constants. No combination may produce a rect
// outside Bounds.
type Request struct {
	Anchor Rect // the trigger widget's rect
	Bounds Rect // the area popups may occupy (normally the whole panel)

	NatW, NatH int // the popup body's natural size, from gtk_widget_measure
	MinW, MinH int // floors: dropdowns pass the trigger width as MinW

	Gap    int // vertical gap between anchor and popup
	Margin int // inset from the bounds edges

	Align Align
}

// Placement is where the popup goes.
type Placement struct {
	Rect Rect

	// Above is true when the popup opens above the anchor instead of below it.
	Above bool

	// Shortened is true when Rect.H is less than the height asked for. The
	// caller's ScrolledWindow absorbs the difference; nothing here needs to be
	// re-measured or re-requested.
	Shortened bool
}

// Place computes the popup rectangle for one request.
//
// The popup opens below the anchor when its natural height fits there, above it
// when it only fits above, and into whichever side has more room — shortened —
// when it fits neither. When neither side offers even MinH the popup takes MinH
// anyway and overlaps the anchor, because a sliver-height list is unusable and a
// zero-height one is invisible; both are worse than covering the trigger.
//
// Degenerate bounds or a popup with no size yield the zero Placement, as
// panelgeom.Panel yields the zero Rect: there is nothing to show, and a
// negative-sized allocation would make GTK warn every frame.
func Place(r Request) Placement {
	b := r.Bounds
	if b.W <= 0 || b.H <= 0 || r.NatW <= 0 || r.NatH <= 0 {
		return Placement{}
	}

	// Negative gaps and margins are treated as zero rather than allowed to
	// enlarge the usable area past the bounds.
	gap, margin := r.Gap, r.Margin
	if gap < 0 {
		gap = 0
	}
	if margin < 0 {
		margin = 0
	}

	// The inner box popups may occupy. If the margin consumes the bounds
	// entirely, drop it rather than invert the box.
	innerX, innerY := b.X+margin, b.Y+margin
	innerW, innerH := b.W-2*margin, b.H-2*margin
	if innerW <= 0 || innerH <= 0 {
		innerX, innerY, innerW, innerH = b.X, b.Y, b.W, b.H
	}

	// Width: at least MinW (a dropdown never narrower than its trigger), at
	// most the inner width (a user-typed profile name must not widen the popup
	// past the panel — ellipsization handles the text, this handles the box).
	w := r.NatW
	if w < r.MinW {
		w = r.MinW
	}
	if w > innerW {
		w = innerW
	}

	// Horizontal position from the alignment, then clamped into the inner box.
	// The clamp runs high-edge first so that when both clamps apply (popup as
	// wide as the inner box) the left edge wins and X is deterministic.
	var x int
	switch r.Align {
	case AlignCenter:
		x = r.Anchor.X + (r.Anchor.W-w)/2
	case AlignEnd:
		x = r.Anchor.X + r.Anchor.W - w
	default:
		x = r.Anchor.X
	}
	if x+w > innerX+innerW {
		x = innerX + innerW - w
	}
	if x < innerX {
		x = innerX
	}

	// Vertical space on each side of the anchor, clamped to [0, innerH] so an
	// anchor scrolled outside the bounds reads as "all the room is on the far
	// side" rather than as more room than the panel has.
	spaceBelow := (innerY + innerH) - (r.Anchor.Y + r.Anchor.H + gap)
	spaceAbove := r.Anchor.Y - gap - innerY
	if spaceBelow < 0 {
		spaceBelow = 0
	}
	if spaceBelow > innerH {
		spaceBelow = innerH
	}
	if spaceAbove < 0 {
		spaceAbove = 0
	}
	if spaceAbove > innerH {
		spaceAbove = innerH
	}

	minH := r.MinH
	if minH < 1 {
		minH = 1
	}

	h := r.NatH
	above := false
	shortened := false
	switch {
	case spaceBelow >= h:
		// Fits below at natural height.
	case spaceAbove >= h:
		above = true
	default:
		// Fits neither side whole: shorten into whichever has more room. Ties
		// go below, the reading direction.
		space := spaceBelow
		if spaceAbove > spaceBelow {
			above, space = true, spaceAbove
		}
		if space < minH {
			// Neither side is usable. Take minH and overlap the anchor; the
			// clamp below keeps the rect inside the bounds.
			space = minH
			if space > innerH {
				space = innerH
			}
		}
		if h > space {
			h, shortened = space, true
		}
	}

	var y int
	if above {
		y = r.Anchor.Y - gap - h
	} else {
		y = r.Anchor.Y + r.Anchor.H + gap
	}
	if y+h > innerY+innerH {
		y = innerY + innerH - h
	}
	if y < innerY {
		y = innerY
	}

	return Placement{
		Rect:      Rect{X: x, Y: y, W: w, H: h},
		Above:     above,
		Shortened: shortened,
	}
}
