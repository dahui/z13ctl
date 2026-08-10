// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package panelgeom

import (
	"math"
	"strings"
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
			got := Panel(tc.monW, tc.monH, tc.drawerWidth, tc.margin, EdgeRight)
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
			if got := Panel(tc.monW, tc.monH, tc.drawerWidth, tc.margin, EdgeRight); got != (Rect{}) {
				t.Errorf("Panel(%d, %d, %d, %d, EdgeRight) = %+v, want the zero Rect",
					tc.monW, tc.monH, tc.drawerWidth, tc.margin, got)
			}
		})
	}
}

func TestPanelDefaultMarginIsFivePercent(t *testing.T) {
	// Pins the value the layer-shell backend uses, so the two backends put the
	// drawer in the same place.
	const monH = 1600
	got := Panel(2560, monH, 320, DefaultMarginFraction, EdgeRight)
	if want := monH / 20; got.Y != want {
		t.Errorf("top margin = %d, want %d (5%% of %d)", got.Y, want, monH)
	}
}

func TestSlideXEndpoints(t *testing.T) {
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction, EdgeRight)

	if got := SlideX(rest, HiddenX(rest, monW, EdgeRight), 0); got != monW {
		t.Errorf("SlideX at t=0 = %d, want %d (fully off the right edge)", got, monW)
	}
	if got := SlideX(rest, HiddenX(rest, monW, EdgeRight), 1); got != rest.X {
		t.Errorf("SlideX at t=1 = %d, want the rest position %d", got, rest.X)
	}
}

func TestSlideXClampsOutOfRange(t *testing.T) {
	// A tick callback can produce a t slightly outside [0,1] when a frame lands
	// after the animation's nominal end. Extrapolating there would overshoot the
	// rest position and show a jump on the final frame.
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction, EdgeRight)

	for _, tt := range []float64{-1, -0.001, math.NaN()} {
		if got := SlideX(rest, HiddenX(rest, monW, EdgeRight), tt); got != monW {
			t.Errorf("SlideX at t=%v = %d, want %d", tt, got, monW)
		}
	}
	for _, tt := range []float64{1.001, 2, math.Inf(1)} {
		if got := SlideX(rest, HiddenX(rest, monW, EdgeRight), tt); got != rest.X {
			t.Errorf("SlideX at t=%v = %d, want %d", tt, got, rest.X)
		}
	}
}

func TestSlideXIsMonotonic(t *testing.T) {
	// The panel may never move backwards during a slide — a non-monotonic curve
	// reads as a stutter.
	const monW = 2560
	rest := Panel(monW, 1600, 320, DefaultMarginFraction, EdgeRight)

	prev := SlideX(rest, HiddenX(rest, monW, EdgeRight), 0)
	for i := 1; i <= 100; i++ {
		got := SlideX(rest, HiddenX(rest, monW, EdgeRight), float64(i)/100)
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
	rest := Panel(monW, 1600, 320, DefaultMarginFraction, EdgeRight)

	for i := 0; i <= 100; i++ {
		x := SlideX(rest, HiddenX(rest, monW, EdgeRight), float64(i)/100)
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

func TestParseEdge(t *testing.T) {
	cases := []struct {
		in      string
		want    Edge
		errWord string
	}{
		{"", EdgeRight, ""}, // absent is not an error, it is the default
		{"right", EdgeRight, ""},
		{"left", EdgeLeft, ""},
		{" LEFT ", EdgeLeft, ""}, // config files are hand-edited
		// Named so the message can say why, rather than "unknown edge": the
		// drawer is a fixed-width column, so a horizontal edge is a different
		// layout and not a different anchor.
		{"top", EdgeRight, "horizontal layout"},
		{"bottom", EdgeRight, "horizontal layout"},
		{"sideways", EdgeRight, "unknown edge"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseEdge(tc.in)
			// Every path returns a usable edge, so a caller that logs the error
			// and carries on gets a working drawer rather than a zero value it
			// has to second-guess.
			if got != tc.want {
				t.Errorf("ParseEdge(%q) = %v, want %v", tc.in, got, tc.want)
			}
			switch {
			case tc.errWord == "" && err != nil:
				t.Errorf("ParseEdge(%q) = %v, want no error", tc.in, err)
			case tc.errWord != "" && err == nil:
				t.Errorf("ParseEdge(%q) succeeded, want an error mentioning %q", tc.in, tc.errWord)
			case tc.errWord != "" && !strings.Contains(err.Error(), tc.errWord):
				t.Errorf("ParseEdge(%q) = %v, want it to mention %q", tc.in, err, tc.errWord)
			}
		})
	}

	// The spelling round-trips, so a config written from a parsed value reparses.
	for _, e := range []Edge{EdgeRight, EdgeLeft} {
		if got, err := ParseEdge(e.String()); err != nil || got != e {
			t.Errorf("round trip of %v gave %v, %v", e, got, err)
		}
	}
}

func TestPanelOnTheLeftEdge(t *testing.T) {
	const monW, monH = 2560, 1600
	left := Panel(monW, monH, 320, DefaultMarginFraction, EdgeLeft)
	right := Panel(monW, monH, 320, DefaultMarginFraction, EdgeRight)

	if left.X != 0 {
		t.Errorf("left panel X = %d, want 0", left.X)
	}
	// The edge moves the panel and changes nothing else — same size, same
	// vertical inset — so a mirrored drawer is the same drawer.
	if left.W != right.W || left.Y != right.Y || left.H != right.H {
		t.Errorf("left %+v and right %+v differ in more than X", left, right)
	}

	// A drawer wider than the output is clamped on both edges, so its controls
	// stay reachable either way.
	wide := Panel(200, monH, 320, DefaultMarginFraction, EdgeLeft)
	if wide.X != 0 || wide.W != 200 {
		t.Errorf("clamped left panel = %+v, want X=0 W=200", wide)
	}
}

func TestHiddenXAndSlideDirection(t *testing.T) {
	const monW = 2560
	for _, tc := range []struct {
		edge   Edge
		hidden int
	}{
		{EdgeRight, monW},
		{EdgeLeft, -320}, // one panel width past the left edge
	} {
		t.Run(tc.edge.String(), func(t *testing.T) {
			rest := Panel(monW, 1600, 320, DefaultMarginFraction, tc.edge)
			hidden := HiddenX(rest, monW, tc.edge)
			if hidden != tc.hidden {
				t.Fatalf("HiddenX = %d, want %d", hidden, tc.hidden)
			}
			if got := SlideX(rest, hidden, 0); got != hidden {
				t.Errorf("t=0 is at %d, want %d", got, hidden)
			}
			if got := SlideX(rest, hidden, 1); got != rest.X {
				t.Errorf("t=1 is at %d, want rest %d", got, rest.X)
			}
			// Fully hidden means no pixel of the panel is on screen. The
			// backends keep a 1px sliver deliberately; that is their business,
			// and this is the geometry they offset from.
			if tc.edge == EdgeLeft && hidden+rest.W > 0 {
				t.Errorf("left-hidden panel still occupies %d..%d", hidden, hidden+rest.W)
			}
			if tc.edge == EdgeRight && hidden < monW {
				t.Errorf("right-hidden panel starts at %d, before the output edge %d", hidden, monW)
			}
		})
	}
}

func TestHasNeighbor(t *testing.T) {
	// A 2560x1600 primary with its top-left at the origin.
	self := Rect{X: 0, Y: 0, W: 2560, H: 1600}
	rightOf := Rect{X: 2560, Y: 0, W: 1920, H: 1080}
	leftOf := Rect{X: -1920, Y: 0, W: 1920, H: 1080}
	above := Rect{X: 0, Y: -1080, W: 1920, H: 1080}
	// Sits to the right but its vertical span only touches self's bottom edge.
	cornerTouch := Rect{X: 2560, Y: 1600, W: 1920, H: 1080}

	cases := []struct {
		name   string
		others []Rect
		edge   Edge
		want   bool
	}{
		{"nothing at all", nil, EdgeRight, false},
		{"monitor to the right, sliding right", []Rect{rightOf}, EdgeRight, true},
		{"monitor to the right, sliding left", []Rect{rightOf}, EdgeLeft, false},
		{"monitor to the left, sliding left", []Rect{leftOf}, EdgeLeft, true},
		{"monitor to the left, sliding right", []Rect{leftOf}, EdgeRight, false},
		{"stacked vertically", []Rect{above}, EdgeRight, false},
		{"corner contact is not an overlap", []Rect{cornerTouch}, EdgeRight, false},
		{"one of several", []Rect{above, rightOf}, EdgeRight, true},
		// The list GDK hands the backend includes the monitor the drawer is on.
		{"self is not its own neighbour", []Rect{self}, EdgeRight, false},
		{"self is not its own neighbour, left", []Rect{self}, EdgeLeft, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasNeighbor(self, tc.others, tc.edge); got != tc.want {
				t.Errorf("HasNeighbor(%v) = %v, want %v", tc.edge, got, tc.want)
			}
		})
	}
}
