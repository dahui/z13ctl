// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package panelgeom

import (
	"math"
	"testing"
)

// assertPanelValid asserts the invariants every non-degenerate panel must hold,
// rather than spot values. A panel that violates one of these is not merely
// mispositioned: it is unreachable, invisible, or makes GTK warn on every
// allocation.
func assertPanelValid(t *testing.T, r Rect, monW, monH int) {
	t.Helper()

	if r.W <= 0 || r.H <= 0 {
		t.Fatalf("panel has non-positive size: %+v", r)
	}
	if r.X < 0 || r.Y < 0 {
		t.Errorf("panel starts off the top/left of the output: %+v", r)
	}
	if r.X+r.W > monW {
		t.Errorf("panel overflows the right edge: %+v on %dx%d", r, monW, monH)
	}
	if r.Y+r.H > monH {
		t.Errorf("panel overflows the bottom edge: %+v on %dx%d", r, monW, monH)
	}
	// Right-aligned: the whole point of the drawer.
	if r.X+r.W != monW {
		t.Errorf("panel is not flush with the right edge: %+v on %dx%d", r, monW, monH)
	}
	// Vertical inset is symmetric.
	if top, bottom := r.Y, monH-(r.Y+r.H); top != bottom {
		t.Errorf("vertical margins are asymmetric: top=%d bottom=%d (%+v)", top, bottom, r)
	}
}

func TestPanel(t *testing.T) {
	tests := []struct {
		name                            string
		monW, monH, drawerWidth, margin int
	}{
		{"z13 native", 2560, 1600, 320, DefaultMarginFraction},
		{"1080p", 1920, 1080, 320, DefaultMarginFraction},
		{"4k", 3840, 2160, 320, DefaultMarginFraction},
		{"portrait", 1200, 1920, 320, DefaultMarginFraction},
		{"no inset", 1920, 1080, 320, 0},
		{"negative fraction is treated as no inset", 1920, 1080, 320, -5},
		{"tiny output", 400, 300, 320, DefaultMarginFraction},
		{"drawer exactly the output width", 320, 800, 320, DefaultMarginFraction},
		{"drawer wider than the output", 240, 800, 320, DefaultMarginFraction},
		{"inset consumes the whole output", 1920, 1080, 320, 2},
		{"inset larger than the output", 1920, 1080, 320, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Panel(tc.monW, tc.monH, tc.drawerWidth, tc.margin)
			assertPanelValid(t, got, tc.monW, tc.monH)

			if tc.drawerWidth <= tc.monW && got.W != tc.drawerWidth {
				t.Errorf("width %d, want the requested %d", got.W, tc.drawerWidth)
			}
			if tc.drawerWidth > tc.monW && got.W != tc.monW {
				t.Errorf("width %d, want it clamped to the output width %d", got.W, tc.monW)
			}
		})
	}
}

func TestPanelDegenerate(t *testing.T) {
	// GDK can report a zero geometry for a monitor that is being hotplugged,
	// and drawerWidth comes from a caller. None of these may produce a
	// negative-sized rect.
	tests := []struct {
		name                            string
		monW, monH, drawerWidth, margin int
	}{
		{"zero width", 0, 1080, 320, DefaultMarginFraction},
		{"zero height", 1920, 0, 320, DefaultMarginFraction},
		{"negative width", -1920, 1080, 320, DefaultMarginFraction},
		{"negative height", 1920, -1080, 320, DefaultMarginFraction},
		{"zero drawer width", 1920, 1080, 0, DefaultMarginFraction},
		{"negative drawer width", 1920, 1080, -320, DefaultMarginFraction},
		{"everything zero", 0, 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Panel(tc.monW, tc.monH, tc.drawerWidth, tc.margin); got != (Rect{}) {
				t.Errorf("Panel(%d, %d, %d, %d) = %+v, want the zero Rect",
					tc.monW, tc.monH, tc.drawerWidth, tc.margin, got)
			}
		})
	}
}

func TestPanelDefaultMarginIsFivePercent(t *testing.T) {
	// Pins the value the layer-shell backend uses, so the two backends put the
	// drawer in the same place.
	const monH = 1600
	got := Panel(2560, monH, 320, DefaultMarginFraction)
	if want := monH / 20; got.Y != want {
		t.Errorf("top margin = %d, want %d (5%% of %d)", got.Y, want, monH)
	}
}

func TestSlideXEndpoints(t *testing.T) {
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction)

	if got := SlideX(rest, monW, 0); got != monW {
		t.Errorf("SlideX at t=0 = %d, want %d (fully off the right edge)", got, monW)
	}
	if got := SlideX(rest, monW, 1); got != rest.X {
		t.Errorf("SlideX at t=1 = %d, want the rest position %d", got, rest.X)
	}
}

func TestSlideXClampsOutOfRange(t *testing.T) {
	// A tick callback can produce a t slightly outside [0,1] when a frame lands
	// after the animation's nominal end. Extrapolating there would overshoot the
	// rest position and show a jump on the final frame.
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction)

	for _, tt := range []float64{-1, -0.001, math.NaN()} {
		if got := SlideX(rest, monW, tt); got != monW {
			t.Errorf("SlideX at t=%v = %d, want %d", tt, got, monW)
		}
	}
	for _, tt := range []float64{1.001, 2, math.Inf(1)} {
		if got := SlideX(rest, monW, tt); got != rest.X {
			t.Errorf("SlideX at t=%v = %d, want %d", tt, got, rest.X)
		}
	}
}

func TestSlideXIsMonotonic(t *testing.T) {
	// The panel may never move backwards during a slide — a non-monotonic curve
	// reads as a stutter.
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction)

	prev := SlideX(rest, monW, 0)
	for i := 1; i <= 100; i++ {
		got := SlideX(rest, monW, float64(i)/100)
		if got > prev {
			t.Fatalf("SlideX moved right at t=%.2f: %d after %d", float64(i)/100, got, prev)
		}
		prev = got
	}
	if prev != rest.X {
		t.Errorf("slide ended at %d, want the rest position %d", prev, rest.X)
	}
}

func TestSlideXStaysOnScreen(t *testing.T) {
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction)

	for i := 0; i <= 100; i++ {
		x := SlideX(rest, monW, float64(i)/100)
		if x < rest.X || x > monW {
			t.Errorf("SlideX at t=%.2f = %d, outside [%d, %d]", float64(i)/100, x, rest.X, monW)
		}
	}
}

func TestSmoothstep(t *testing.T) {
	if got := Smoothstep(0); got != 0 {
		t.Errorf("Smoothstep(0) = %v, want 0", got)
	}
	if got := Smoothstep(1); got != 1 {
		t.Errorf("Smoothstep(1) = %v, want 1", got)
	}
	if got := Smoothstep(0.5); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("Smoothstep(0.5) = %v, want 0.5", got)
	}
}

func TestSmoothstepClampsOutOfRange(t *testing.T) {
	for _, tt := range []float64{-1, -0.001, math.NaN()} {
		if got := Smoothstep(tt); got != 0 {
			t.Errorf("Smoothstep(%v) = %v, want 0", tt, got)
		}
	}
	for _, tt := range []float64{1.001, 2, math.Inf(1)} {
		if got := Smoothstep(tt); got != 1 {
			t.Errorf("Smoothstep(%v) = %v, want 1", tt, got)
		}
	}
}

func TestSmoothstepIsMonotonicAndSymmetric(t *testing.T) {
	prev := Smoothstep(0)
	for i := 1; i <= 100; i++ {
		tt := float64(i) / 100
		got := Smoothstep(tt)
		if got < prev {
			t.Fatalf("Smoothstep decreased at t=%.2f: %v after %v", tt, got, prev)
		}
		prev = got

		// Symmetric about the midpoint: ease-in and ease-out match.
		if mirror := Smoothstep(1 - tt); math.Abs(got+mirror-1) > 1e-9 {
			t.Errorf("Smoothstep(%.2f)+Smoothstep(%.2f) = %v, want 1", tt, 1-tt, got+mirror)
		}
	}
}
