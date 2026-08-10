// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package focusgrid

import (
	"fmt"
	"testing"
)

// fmtCoords renders coordinates as "row:col:section" so a failure prints the
// whole layout rather than the first differing struct.
func fmtCoords(cs []Coord) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = fmt.Sprintf("%d:%d:%s", c.Row, c.Col, c.Section)
	}
	return out
}

func assertCoords(t *testing.T, got []Coord, want []string) {
	t.Helper()
	g := fmtCoords(got)
	if len(g) != len(want) {
		t.Fatalf("got %d coords, want %d\n got: %v\nwant: %v", len(g), len(want), g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Errorf("coord %d = %s, want %s\n got: %v\nwant: %v", i, g[i], want[i], g, want)
		}
	}
}

func TestLineAdvancesOneLine(t *testing.T) {
	b := NewBuilder(Vertical).Section("a")
	b.Line(3)
	b.Line(1)
	b.Section("b").Line(2)
	assertCoords(t, b.Build(), []string{
		"0:0:a", "0:1:a", "0:2:a",
		"1:0:a",
		"2:0:b", "2:1:b",
	})
}

// A grid that fills its last line exactly is the case the hand-written version
// gets right; it is here so the partial case below is a contrast, not a lone
// assertion.
func TestGridExactFit(t *testing.T) {
	b := NewBuilder(Vertical).Section("mode")
	b.Grid(6, 3)
	b.Section("after").Line(1)
	assertCoords(t, b.Build(), []string{
		"0:0:mode", "0:1:mode", "0:2:mode",
		"1:0:mode", "1:1:mode", "1:2:mode",
		"2:0:after",
	})
}

// The regression this type exists for. buildMainFocusList computes the grid's
// rows as `modeBase + i/3` but then advances with `row = modeBase + 1`, which
// is the grid's height only while it holds exactly six items. A seventh lands
// on row modeBase+2 while the next section is placed at modeBase+2 as well, so
// two unrelated controls share a row and D-pad down from the grid reaches the
// wrong one. Grid advances by the ceiling, so it cannot happen here.
func TestGridPartialLastLineStillConsumesIt(t *testing.T) {
	b := NewBuilder(Vertical).Section("mode")
	b.Grid(7, 3)
	b.Section("after").Line(1)
	assertCoords(t, b.Build(), []string{
		"0:0:mode", "0:1:mode", "0:2:mode",
		"1:0:mode", "1:1:mode", "1:2:mode",
		"2:0:mode",
		"3:0:after", // NOT 2:0 — the partial line is still a line
	})
	if b.Lines() != 4 {
		t.Errorf("Lines() = %d, want 4", b.Lines())
	}
}

func TestOneIsALineOfOne(t *testing.T) {
	b := NewBuilder(Vertical).Section("a")
	first := b.One()
	second := b.One()
	if first != (Coord{Row: 0, Col: 0, Section: "a"}) {
		t.Errorf("first = %+v", first)
	}
	if second != (Coord{Row: 1, Col: 0, Section: "a"}) {
		t.Errorf("second = %+v", second)
	}
	assertCoords(t, b.Build(), []string{"0:0:a", "1:0:a"})
}

// A section the caller skips must not leave a gap, or the same view built with
// and without an optional control disagrees about every row below it.
func TestEmptyDeclarationsConsumeNothing(t *testing.T) {
	b := NewBuilder(Vertical).Section("a")
	b.Line(1)
	b.Section("skipped").Line(0)
	b.Section("skipped2").Grid(0, 3)
	b.Section("b").Line(1)
	assertCoords(t, b.Build(), []string{"0:0:a", "1:0:b"})
}

// "No wrapping" is a legitimate request and must not divide by zero.
func TestGridNonPositiveColsIsOneLine(t *testing.T) {
	b := NewBuilder(Vertical).Section("a")
	b.Grid(4, 0)
	b.Section("b").Line(1)
	assertCoords(t, b.Build(), []string{
		"0:0:a", "0:1:a", "0:2:a", "0:3:a",
		"1:0:b",
	})
}

// Horizontal is defined as the transpose of Vertical rather than as its own set
// of rules, so the two cannot drift. Asserting it as a property over a
// non-trivial layout is stronger than asserting a hand-written expectation.
func TestHorizontalIsExactlyTheTransposeOfVertical(t *testing.T) {
	build := func(o Orientation) []Coord {
		b := NewBuilder(o).Section("profile")
		b.Line(3)
		b.Line(1)
		b.Section("mode").Grid(7, 3)
		b.Section("footer").Line(2)
		return b.Build()
	}
	vert, horiz := build(Vertical), build(Horizontal)
	if len(vert) != len(horiz) {
		t.Fatalf("length differs: %d vs %d", len(vert), len(horiz))
	}
	for i := range vert {
		want := Coord{Row: vert[i].Col, Col: vert[i].Row, Section: vert[i].Section}
		if horiz[i] != want {
			t.Errorf("coord %d: horizontal = %+v, want the transpose %+v", i, horiz[i], want)
		}
	}
}

// The builder's output has to be navigable by the functions it feeds — that is
// the only reason it produces coordinates at all.
func TestBuiltCoordsNavigate(t *testing.T) {
	b := NewBuilder(Vertical).Section("profile")
	b.Line(3) // row 0: three across
	b.Section("mode").Grid(6, 3)
	items := coordsToItems(b.Build())

	// Down from the middle of row 0 lands in the same column of row 1.
	if got := MoveVertical(items, 1, +1); items[got].Row != 1 || items[got].Col != 1 {
		t.Errorf("down from 0:1 → %+v, want row 1 col 1", items[got])
	}
	// Right wraps within a row.
	if got := MoveHorizontal(items, 2, +1); got != 0 {
		t.Errorf("right from 0:2 → %d, want wrap to 0", got)
	}
	// The sections are in visual order.
	if secs := Sections(items); len(secs) != 2 || secs[0] != "profile" || secs[1] != "mode" {
		t.Errorf("Sections() = %v, want [profile mode]", secs)
	}
}

// coordsToItems makes every coordinate visible, which is what the navigation
// tests want; the GTK layer supplies real visibility.
func coordsToItems(cs []Coord) []Item {
	items := make([]Item, len(cs))
	for i, c := range cs {
		items[i] = Item{Row: c.Row, Col: c.Col, Section: c.Section, Visible: true}
	}
	return items
}

// TestReproducesTheCustomViewLayout pins the custom profile view's coordinates
// as buildCustomFocusList produced them before the conversion, for the Z13 case
// where the basic slider, advanced checkbox, fan curve and undervolt scale all
// exist. Its conditionals are the interesting part: each optional control sat
// behind a nil check that also owned a `row++`, so a device missing one shifted
// everything below it — which the Builder reproduces exactly, since a skipped
// declaration consumes no line.
func TestReproducesTheCustomViewLayout(t *testing.T) {
	b := NewBuilder(Vertical)
	b.Section("nav")
	b.One() // back
	b.Section("profile")
	b.One()   // profile selector dropdown
	b.Line(3) // activate / new / save-as
	b.Line(2) // inline name entry: OK / cancel
	b.Section("tdp")
	b.One() // basic TDP slider
	b.One() // advanced checkbox
	b.One() // PL1
	b.One() // PL2
	b.One() // PL3
	b.Section("fan")
	b.One() // curve editor
	b.Section("undervolt")
	b.One()   // CPU CO slider
	b.Line(2) // save / reset
	b.Section("actions")
	b.Line(3) // save TDP / fan / both
	b.Line(2) // reset TDP / fan
	b.Line(1) // delete

	assertCoords(t, b.Build(), []string{
		"0:0:nav",
		"1:0:profile",
		"2:0:profile", "2:1:profile", "2:2:profile",
		"3:0:profile", "3:1:profile",
		"4:0:tdp", "5:0:tdp", "6:0:tdp", "7:0:tdp", "8:0:tdp",
		"9:0:fan",
		"10:0:undervolt", "11:0:undervolt", "11:1:undervolt",
		"12:0:actions", "12:1:actions", "12:2:actions",
		"13:0:actions", "13:1:actions",
		"14:0:actions",
	})
}

// TestReproducesTheThemeViewLayout pins the theme picker: a back button, then
// each theme's radio followed by that theme's accent dots wrapped at
// dotsPerRow (7). The old code advanced with `(n-1)/dotsPerRow + 1`, which is
// the same ceiling Grid applies — this fixes that agreement in a test rather
// than leaving it to two expressions in different files.
func TestReproducesTheThemeViewLayout(t *testing.T) {
	const dotsPerRow = 7
	b := NewBuilder(Vertical)
	b.Section("nav")
	b.One()
	b.Section("theme")
	b.One()               // theme 0 radio
	b.Grid(9, dotsPerRow) // 9 accents → two lines, second partial
	b.One()               // theme 1 radio
	b.Grid(0, dotsPerRow) // a theme with no accents consumes no line
	b.One()               // theme 2 radio

	assertCoords(t, b.Build(), []string{
		"0:0:nav",
		"1:0:theme",
		"2:0:theme", "2:1:theme", "2:2:theme", "2:3:theme", "2:4:theme", "2:5:theme", "2:6:theme",
		"3:0:theme", "3:1:theme",
		"4:0:theme",
		"5:0:theme",
	})
}

// TestReproducesTheColorViewLayout pins the HSL picker: back, a row of presets,
// then the three sliders one per line.
func TestReproducesTheColorViewLayout(t *testing.T) {
	b := NewBuilder(Vertical)
	b.Section("nav")
	b.One()
	b.Section("presets")
	b.Line(8)
	b.Section("sliders")
	b.One() // hue
	b.One() // saturation
	b.One() // lightness

	assertCoords(t, b.Build(), []string{
		"0:0:nav",
		"1:0:presets", "1:1:presets", "1:2:presets", "1:3:presets",
		"1:4:presets", "1:5:presets", "1:6:presets", "1:7:presets",
		"2:0:sliders", "3:0:sliders", "4:0:sliders",
	})
}

// TestReproducesTheMainViewLayout is the parity guard for the conversion in
// internal/gui: the Builder must produce byte-identical coordinates to the
// hand-numbered buildMainFocusList, for the Z13 case where every optional
// control is present. If this and the widget code ever disagree, gamepad
// navigation moves without anything looking wrong on screen.
//
// The error bar is deliberately absent: it is pinned at errBarRow (10000) so it
// sorts after every view's rows, which is a sentinel rather than a position in
// the flow, and the GTK layer appends it directly.
func TestReproducesTheMainViewLayout(t *testing.T) {
	b := NewBuilder(Vertical)
	b.Section("profile")
	b.Line(3) // quiet / balanced / performance
	b.Line(1) // Custom
	b.Section("autoswitch")
	b.Line(1) // enable switch
	b.Line(1) // on AC target
	b.Line(1) // on battery target
	b.Section("battery")
	b.Line(1) // charge limit slider
	b.Section("tabs")
	b.Line(2) // keyboard / lightbar
	b.Section("mode")
	b.Grid(6, 3) // modeOrder
	b.Section("color1")
	b.Line(8) // presets
	b.Line(1) // custom colour
	b.Section("color2")
	b.Line(8)
	b.Line(1)
	b.Section("speed")
	b.Line(3) // slow / normal / fast
	b.Section("brightness")
	b.Line(1)
	b.Section("footer")
	b.Line(3) // theme / overdrive / boot sound

	assertCoords(t, b.Build(), []string{
		"0:0:profile", "0:1:profile", "0:2:profile",
		"1:0:profile",
		"2:0:autoswitch", "3:0:autoswitch", "4:0:autoswitch",
		"5:0:battery",
		"6:0:tabs", "6:1:tabs",
		"7:0:mode", "7:1:mode", "7:2:mode",
		"8:0:mode", "8:1:mode", "8:2:mode",
		"9:0:color1", "9:1:color1", "9:2:color1", "9:3:color1",
		"9:4:color1", "9:5:color1", "9:6:color1", "9:7:color1",
		"10:0:color1",
		"11:0:color2", "11:1:color2", "11:2:color2", "11:3:color2",
		"11:4:color2", "11:5:color2", "11:6:color2", "11:7:color2",
		"12:0:color2",
		"13:0:speed", "13:1:speed", "13:2:speed",
		"14:0:brightness",
		"15:0:footer", "15:1:footer", "15:2:footer",
	})
}
