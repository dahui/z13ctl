// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package popupgeom

import "testing"

// drawer-shaped bounds used by most cases: the panel is 320 wide and the
// popup area is the whole of it.
var bounds = Rect{X: 0, Y: 0, W: 320, H: 600}

// assertPlacementValid asserts the invariants every non-degenerate placement
// must hold. A popup outside its bounds is clipped by SetClipOverlay into
// something partial, and in the overlay backend it falls outside the input
// region — visible but dead. A zero-height one is invisible.
func assertPlacementValid(t *testing.T, p Placement, b Rect) {
	t.Helper()

	r := p.Rect
	if r.W <= 0 || r.H <= 0 {
		t.Fatalf("placement has non-positive size: %+v", r)
	}
	if r.X < b.X || r.Y < b.Y {
		t.Errorf("placement starts outside the bounds: %+v in %+v", r, b)
	}
	if r.X+r.W > b.X+b.W {
		t.Errorf("placement overflows the right edge: %+v in %+v", r, b)
	}
	if r.Y+r.H > b.Y+b.H {
		t.Errorf("placement overflows the bottom edge: %+v in %+v", r, b)
	}
}

func TestPlace(t *testing.T) {
	tests := []struct {
		name string
		req  Request

		wantAbove     bool
		wantShortened bool
		// Optional spot checks; -1 means "not asserted".
		wantX, wantY, wantW, wantH int
	}{
		{
			name: "fits below at natural height",
			req: Request{
				Anchor: Rect{X: 8, Y: 100, W: 304, H: 32},
				Bounds: bounds,
				NatW:   200, NatH: 200, MinW: 304, MinH: 120,
				Gap: 4, Margin: 8,
			},
			wantX: 8, wantY: 136, wantW: 304, wantH: 200,
		},
		{
			name: "overflows below, flips above",
			req: Request{
				Anchor: Rect{X: 8, Y: 500, W: 304, H: 32},
				Bounds: bounds,
				NatW:   200, NatH: 200, MinW: 304, MinH: 120,
				Gap: 4, Margin: 8,
			},
			wantAbove: true,
			wantX:     8, wantY: 296, wantW: 304, wantH: 200,
		},
		{
			name: "overflows both sides, shortened into the larger",
			req: Request{
				Anchor: Rect{X: 8, Y: 120, W: 304, H: 32},
				Bounds: Rect{X: 0, Y: 0, W: 320, H: 300},
				NatW:   200, NatH: 400, MinW: 304, MinH: 60,
				Gap: 4, Margin: 8,
			},
			// Below has 292-156=136, above has 108; below wins.
			wantShortened: true,
			wantX:         8, wantY: 156, wantW: 304, wantH: 136,
		},
		{
			name: "both sides under MinH still yields a usable height",
			req: Request{
				// The anchor covers nearly the whole bounds, leaving 8px on
				// each side. The popup takes MinH and overlaps the anchor.
				Anchor: Rect{X: 0, Y: 20, W: 320, H: 260},
				Bounds: Rect{X: 0, Y: 0, W: 320, H: 300},
				NatW:   200, NatH: 200, MinW: 304, MinH: 120,
				Gap: 4, Margin: 8,
			},
			wantShortened: true,
			wantH:         120, wantX: -1, wantY: -1, wantW: -1,
		},
		{
			name: "narrow popup widened to MinW",
			req: Request{
				// A dropdown is never narrower than its trigger.
				Anchor: Rect{X: 8, Y: 100, W: 304, H: 32},
				Bounds: bounds,
				NatW:   100, NatH: 100, MinW: 304, MinH: 60,
				Gap: 4, Margin: 8,
			},
			wantW: 304, wantX: 8, wantY: 136, wantH: 100,
		},
		{
			name: "wide popup clamped to the bounds",
			req: Request{
				// An unbreakable long profile name must not widen the popup
				// past the panel.
				Anchor: Rect{X: 8, Y: 100, W: 304, H: 32},
				Bounds: bounds,
				NatW:   900, NatH: 100, MinW: 304, MinH: 60,
				Gap: 4, Margin: 8,
			},
			wantW: 304, wantX: 8, wantY: 136, wantH: 100,
		},
		{
			name: "align start",
			req: Request{
				Anchor: Rect{X: 100, Y: 50, W: 60, H: 24},
				Bounds: bounds,
				NatW:   120, NatH: 100, MinH: 60,
				Gap: 4, Margin: 8, Align: AlignStart,
			},
			wantX: 100, wantY: 78, wantW: 120, wantH: 100,
		},
		{
			name: "align center",
			req: Request{
				Anchor: Rect{X: 100, Y: 50, W: 60, H: 24},
				Bounds: bounds,
				NatW:   120, NatH: 100, MinH: 60,
				Gap: 4, Margin: 8, Align: AlignCenter,
			},
			wantX: 70, wantY: 78, wantW: 120, wantH: 100,
		},
		{
			name: "align end",
			req: Request{
				Anchor: Rect{X: 100, Y: 50, W: 60, H: 24},
				Bounds: bounds,
				NatW:   120, NatH: 100, MinH: 60,
				Gap: 4, Margin: 8, Align: AlignEnd,
			},
			wantX: 40, wantY: 78, wantW: 120, wantH: 100,
		},
		{
			name: "alignment clamped back into the bounds",
			req: Request{
				// Aligned to start of an anchor near the right edge, the popup
				// would overflow; it is pushed left instead.
				Anchor: Rect{X: 280, Y: 50, W: 30, H: 24},
				Bounds: bounds,
				NatW:   120, NatH: 100, MinH: 60,
				Gap: 4, Margin: 8, Align: AlignStart,
			},
			wantX: 192, wantY: 78, wantW: 120, wantH: 100,
		},
		{
			name: "anchor scrolled partly above the bounds",
			req: Request{
				Anchor: Rect{X: 8, Y: -20, W: 304, H: 32},
				Bounds: bounds,
				NatW:   200, NatH: 200, MinW: 304, MinH: 60,
				Gap: 4, Margin: 8,
			},
			wantX: 8, wantY: 16, wantW: 304, wantH: 200,
		},
		{
			name: "anchor wholly below the bounds",
			req: Request{
				Anchor: Rect{X: 8, Y: 700, W: 304, H: 32},
				Bounds: bounds,
				NatW:   200, NatH: 200, MinW: 304, MinH: 60,
				Gap: 4, Margin: 8,
			},
			// All the room reads as above; the rect is clamped back inside.
			wantAbove: true,
			wantX:     8, wantY: 392, wantW: 304, wantH: 200,
		},
		{
			name: "negative gap and margin are treated as zero",
			req: Request{
				Anchor: Rect{X: 8, Y: 100, W: 304, H: 32},
				Bounds: bounds,
				NatW:   200, NatH: 200, MinW: 304, MinH: 60,
				Gap: -4, Margin: -8,
			},
			wantX: 8, wantY: 132, wantW: 304, wantH: 200,
		},
		{
			name: "margin consuming the bounds is dropped",
			req: Request{
				Anchor: Rect{X: 8, Y: 100, W: 304, H: 32},
				Bounds: Rect{X: 0, Y: 0, W: 320, H: 300},
				NatW:   200, NatH: 100, MinW: 0, MinH: 60,
				Gap: 4, Margin: 200,
			},
			wantX: -1, wantY: -1, wantW: -1, wantH: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Place(tc.req)
			assertPlacementValid(t, got, tc.req.Bounds)

			if got.Above != tc.wantAbove {
				t.Errorf("Above = %v, want %v (%+v)", got.Above, tc.wantAbove, got)
			}
			if got.Shortened != tc.wantShortened {
				t.Errorf("Shortened = %v, want %v (%+v)", got.Shortened, tc.wantShortened, got)
			}
			if tc.wantX != -1 && got.Rect.X != tc.wantX {
				t.Errorf("X = %d, want %d (%+v)", got.Rect.X, tc.wantX, got)
			}
			if tc.wantY != -1 && got.Rect.Y != tc.wantY {
				t.Errorf("Y = %d, want %d (%+v)", got.Rect.Y, tc.wantY, got)
			}
			if tc.wantW != -1 && got.Rect.W != tc.wantW {
				t.Errorf("W = %d, want %d (%+v)", got.Rect.W, tc.wantW, got)
			}
			if tc.wantH != -1 && got.Rect.H != tc.wantH {
				t.Errorf("H = %d, want %d (%+v)", got.Rect.H, tc.wantH, got)
			}
		})
	}
}

func TestPlaceDegenerate(t *testing.T) {
	// Bounds come from an allocation that can be zero during construction, and
	// the sizes from measuring a body that may be empty. None of these may
	// produce a rect GTK would warn about.
	anchor := Rect{X: 8, Y: 100, W: 304, H: 32}
	tests := []struct {
		name string
		req  Request
	}{
		{"zero bounds", Request{Anchor: anchor, NatW: 100, NatH: 100}},
		{"negative bounds", Request{Anchor: anchor, Bounds: Rect{W: -320, H: -600}, NatW: 100, NatH: 100}},
		{"zero natural width", Request{Anchor: anchor, Bounds: bounds, NatW: 0, NatH: 100}},
		{"zero natural height", Request{Anchor: anchor, Bounds: bounds, NatW: 100, NatH: 0}},
		{"negative natural size", Request{Anchor: anchor, Bounds: bounds, NatW: -1, NatH: -1}},
		{"everything zero", Request{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Place(tc.req); got != (Placement{}) {
				t.Errorf("Place(%+v) = %+v, want the zero Placement", tc.req, got)
			}
		})
	}
}

// TestPlaceNeverEscapesBounds sweeps anchors across and beyond the panel — the
// scrolled-out cases a table cannot enumerate — and asserts the one property
// everything else depends on: the rect stays inside the bounds. The overlay
// backend's input region and popupgeom's clamp must agree, or a popup is
// visible but dead.
func TestPlaceNeverEscapesBounds(t *testing.T) {
	for ay := -200; ay <= 800; ay += 50 {
		for ax := -100; ax <= 400; ax += 50 {
			for _, align := range []Align{AlignStart, AlignCenter, AlignEnd} {
				got := Place(Request{
					Anchor: Rect{X: ax, Y: ay, W: 304, H: 32},
					Bounds: bounds,
					NatW:   200, NatH: 250, MinW: 150, MinH: 60,
					Gap: 4, Margin: 8, Align: align,
				})
				assertPlacementValid(t, got, bounds)
			}
		}
	}
}
