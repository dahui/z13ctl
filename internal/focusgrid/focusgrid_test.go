// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package focusgrid

import "testing"

// grid mirrors the shape of the drawer's main view: a couple of full-width rows,
// a two-column row, and a section boundary.
func grid() []Item {
	return []Item{
		{Row: 0, Col: 0, Section: "tabs", Visible: true},
		{Row: 0, Col: 1, Section: "tabs", Visible: true},
		{Row: 1, Col: 0, Section: "mode", Visible: true},
		{Row: 2, Col: 0, Section: "mode", Visible: true},
		{Row: 2, Col: 1, Section: "mode", Visible: true},
		{Row: 2, Col: 2, Section: "mode", Visible: true},
		{Row: 3, Col: 0, Section: "battery", Visible: true},
	}
}

func TestFirstVisible(t *testing.T) {
	if got := FirstVisible(grid()); got != 0 {
		t.Errorf("FirstVisible = %d, want 0", got)
	}

	hidden := grid()
	hidden[0].Visible = false
	hidden[1].Visible = false
	if got := FirstVisible(hidden); got != 2 {
		t.Errorf("FirstVisible with first row hidden = %d, want 2", got)
	}

	for i := range hidden {
		hidden[i].Visible = false
	}
	if got := FirstVisible(hidden); got != -1 {
		t.Errorf("FirstVisible with nothing visible = %d, want -1", got)
	}
	if got := FirstVisible(nil); got != -1 {
		t.Errorf("FirstVisible(nil) = %d, want -1", got)
	}
}

// Out-of-range indices must be returned unchanged rather than panicking. The GTK
// layer used to guard this in four of seven entry points; the contract lives here
// now, so it is asserted for every function.
func TestEveryFunctionToleratesAnOutOfRangeIndex(t *testing.T) {
	items := grid()
	for _, idx := range []int{-1, -100, len(items), len(items) + 50} {
		if got := MoveVertical(items, idx, 1); got != idx {
			t.Errorf("MoveVertical(idx=%d) = %d, want %d", idx, got, idx)
		}
		if got := MoveHorizontal(items, idx, 1); got != idx {
			t.Errorf("MoveHorizontal(idx=%d) = %d, want %d", idx, got, idx)
		}
		if got := JumpSection(items, idx, 1); got != idx {
			t.Errorf("JumpSection(idx=%d) = %d, want %d", idx, got, idx)
		}
	}
}

func TestEveryFunctionToleratesAnEmptyGrid(t *testing.T) {
	for _, items := range [][]Item{nil, {}} {
		if got := MoveVertical(items, 0, 1); got != 0 {
			t.Errorf("MoveVertical on empty = %d, want 0", got)
		}
		if got := MoveHorizontal(items, 0, 1); got != 0 {
			t.Errorf("MoveHorizontal on empty = %d, want 0", got)
		}
		if got := JumpSection(items, 0, 1); got != 0 {
			t.Errorf("JumpSection on empty = %d, want 0", got)
		}
	}
}

func TestMoveVerticalWalksRows(t *testing.T) {
	items := grid()
	// From row 0 col 0, down through every row and wrapping back to the start.
	want := []int{2, 3, 6, 0}
	idx := 0
	for step, expect := range want {
		idx = MoveVertical(items, idx, 1)
		if idx != expect {
			t.Fatalf("step %d: idx = %d (row %d), want %d", step, idx, items[idx].Row, expect)
		}
	}
}

func TestMoveVerticalWrapsBothWays(t *testing.T) {
	items := grid()
	if got := MoveVertical(items, 0, -1); items[got].Row != 3 {
		t.Errorf("up from the top row landed on row %d, want 3 (wrap)", items[got].Row)
	}
	if got := MoveVertical(items, 6, 1); items[got].Row != 0 {
		t.Errorf("down from the bottom row landed on row %d, want 0 (wrap)", items[got].Row)
	}
}

func TestMoveVerticalPreservesColumn(t *testing.T) {
	items := []Item{
		{Row: 0, Col: 0, Visible: true},
		{Row: 0, Col: 1, Visible: true},
		{Row: 0, Col: 2, Visible: true},
		{Row: 1, Col: 0, Visible: true},
		{Row: 1, Col: 1, Visible: true},
		{Row: 1, Col: 2, Visible: true},
	}
	// Column 2 in row 0 is index 2; down should land on column 2 of row 1.
	got := MoveVertical(items, 2, 1)
	if items[got].Col != 2 {
		t.Errorf("moved from col 2 to col %d, want col 2 preserved", items[got].Col)
	}
}

func TestMoveVerticalPicksNearestColumnWhenRowIsNarrower(t *testing.T) {
	items := []Item{
		{Row: 0, Col: 0, Visible: true},
		{Row: 0, Col: 1, Visible: true},
		{Row: 0, Col: 2, Visible: true},
		{Row: 1, Col: 0, Visible: true}, // narrower row
	}
	if got := MoveVertical(items, 2, 1); got != 3 {
		t.Errorf("from col 2 into a single-column row = %d, want 3", got)
	}
}

// Down-then-up must return to the starting item, or holding the D-pad drifts
// sideways across a grid of uneven rows.
func TestMoveVerticalIsReversible(t *testing.T) {
	items := grid()
	for start := range items {
		down := MoveVertical(items, start, 1)
		back := MoveVertical(items, down, -1)
		if items[back].Row != items[start].Row {
			t.Errorf("from %d: down to %d then up to %d, row %d != %d",
				start, down, back, items[back].Row, items[start].Row)
		}
	}
}

func TestMoveVerticalSkipsFullyHiddenRows(t *testing.T) {
	items := grid()
	// Hide all of row 2 — the advanced-TDP case, where a whole section collapses.
	for i := range items {
		if items[i].Row == 2 {
			items[i].Visible = false
		}
	}
	got := MoveVertical(items, 2, 1) // from row 1
	if items[got].Row != 3 {
		t.Errorf("landed on row %d, want 3 (row 2 is hidden)", items[got].Row)
	}
}

func TestMoveVerticalWithASingleVisibleRowStaysPut(t *testing.T) {
	items := grid()
	for i := range items {
		items[i].Visible = items[i].Row == 2
	}
	if got := MoveVertical(items, 3, 1); got != 3 {
		t.Errorf("MoveVertical with one visible row = %d, want 3 (unchanged)", got)
	}
}

// If focus is sitting on a widget that has since been hidden, navigation must
// escape rather than refuse to move — otherwise the gamepad is stuck.
func TestMoveVerticalEscapesAHiddenFocusedItem(t *testing.T) {
	items := grid()
	items[2].Visible = false // the only item in row 1
	got := MoveVertical(items, 2, 1)
	if !items[got].Visible {
		t.Errorf("landed on hidden index %d", got)
	}
}

// The original implementation returned early when the focused item's section was
// absent from the visible set, which happens exactly when that item has been
// hidden — leaving the shoulder buttons dead until something else moved focus.
func TestJumpSectionEscapesAHiddenFocusedItem(t *testing.T) {
	items := grid()
	items[0].Visible = false
	items[1].Visible = false // all of section "tabs" is now hidden

	got := JumpSection(items, 0, 1) // focused item is itself hidden
	if !items[got].Visible {
		t.Errorf("landed on hidden index %d", got)
	}
	if got == 0 {
		t.Error("JumpSection did not move away from the hidden focused item")
	}
}

// Same trap on the horizontal axis: a hidden focused item is not in its own row's
// visible list, so its position could not be found.
func TestMoveHorizontalEscapesAHiddenFocusedItem(t *testing.T) {
	items := grid()
	items[4].Visible = false // middle of row 2; focus sits on it

	got := MoveHorizontal(items, 4, 1)
	if !items[got].Visible {
		t.Errorf("landed on hidden index %d", got)
	}
	if items[got].Row != 2 {
		t.Errorf("left row 2 (landed on row %d); horizontal movement should stay in the row", items[got].Row)
	}
}

// Nothing visible at all: every function must return the index untouched rather
// than escaping to -1 and having the caller index with it.
func TestNavigationWithNothingVisibleStaysPut(t *testing.T) {
	items := grid()
	for i := range items {
		items[i].Visible = false
	}
	for name, fn := range map[string]func([]Item, int, int) int{
		"MoveVertical":   MoveVertical,
		"MoveHorizontal": MoveHorizontal,
		"JumpSection":    JumpSection,
	} {
		if got := fn(items, 3, 1); got != 3 {
			t.Errorf("%s with nothing visible = %d, want 3 (unchanged)", name, got)
		}
	}
}

func TestMoveHorizontalWalksAndWrapsWithinARow(t *testing.T) {
	items := grid()
	// Row 2 holds indices 3,4,5 at columns 0,1,2.
	idx := 3
	for _, want := range []int{4, 5, 3} {
		idx = MoveHorizontal(items, idx, 1)
		if idx != want {
			t.Fatalf("right = %d, want %d", idx, want)
		}
	}
	if got := MoveHorizontal(items, 3, -1); got != 5 {
		t.Errorf("left from the first column = %d, want 5 (wrap)", got)
	}
}

func TestMoveHorizontalOnASingleItemRowStaysPut(t *testing.T) {
	items := grid()
	if got := MoveHorizontal(items, 2, 1); got != 2 {
		t.Errorf("MoveHorizontal on a single-item row = %d, want 2", got)
	}
}

func TestMoveHorizontalSkipsHiddenItems(t *testing.T) {
	items := grid()
	items[4].Visible = false // middle of row 2
	if got := MoveHorizontal(items, 3, 1); got != 5 {
		t.Errorf("right past a hidden item = %d, want 5", got)
	}
}

// Column order, not slice order, decides horizontal movement — the focus lists are
// hand-built and need not be sorted.
func TestMoveHorizontalUsesColumnOrderNotSliceOrder(t *testing.T) {
	items := []Item{
		{Row: 0, Col: 2, Visible: true},
		{Row: 0, Col: 0, Visible: true},
		{Row: 0, Col: 1, Visible: true},
	}
	if got := MoveHorizontal(items, 1, 1); got != 2 {
		t.Errorf("from col 0 right = index %d (col %d), want index 2 (col 1)", got, items[got].Col)
	}
	if got := MoveHorizontal(items, 2, 1); got != 0 {
		t.Errorf("from col 1 right = index %d (col %d), want index 0 (col 2)", got, items[got].Col)
	}
}

func TestSectionsAreInVisualOrder(t *testing.T) {
	got := Sections(grid())
	want := []string{"tabs", "mode", "battery"}
	if len(got) != len(want) {
		t.Fatalf("Sections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sections = %v, want %v", got, want)
		}
	}
}

func TestSectionsOmitsFullyHiddenSections(t *testing.T) {
	items := grid()
	for i := range items {
		if items[i].Section == "mode" {
			items[i].Visible = false
		}
	}
	for _, s := range Sections(items) {
		if s == "mode" {
			t.Error("Sections included a fully hidden section")
		}
	}
}

func TestJumpSectionWalksAndWraps(t *testing.T) {
	items := grid()
	got := JumpSection(items, 0, 1) // tabs -> mode
	if items[got].Section != "mode" {
		t.Errorf("landed in %q, want mode", items[got].Section)
	}
	got = JumpSection(items, got, 1) // mode -> battery
	if items[got].Section != "battery" {
		t.Errorf("landed in %q, want battery", items[got].Section)
	}
	got = JumpSection(items, got, 1) // battery -> tabs (wrap)
	if items[got].Section != "tabs" {
		t.Errorf("landed in %q, want tabs (wrap)", items[got].Section)
	}
	got = JumpSection(items, 0, -1) // tabs -> battery (wrap backwards)
	if items[got].Section != "battery" {
		t.Errorf("landed in %q, want battery (reverse wrap)", items[got].Section)
	}
}

func TestJumpSectionLandsOnTheFirstVisibleItemOfTheSection(t *testing.T) {
	items := grid()
	items[3].Visible = false // first item of row 2 in section "mode"
	got := JumpSection(items, 0, 1)
	if got != 2 {
		t.Errorf("JumpSection = %d, want 2 (first visible item of mode)", got)
	}
	if !items[got].Visible {
		t.Errorf("landed on hidden index %d", got)
	}
}

func TestJumpSectionWithOneSectionStaysPut(t *testing.T) {
	items := []Item{
		{Row: 0, Col: 0, Section: "only", Visible: true},
		{Row: 1, Col: 0, Section: "only", Visible: true},
	}
	if got := JumpSection(items, 0, 1); got != 0 {
		t.Errorf("JumpSection with one section = %d, want 0", got)
	}
}

// The invariant that matters most: no sequence of presses may leave focus on a
// hidden item or outside the slice. Walks a mixed-visibility grid through a long
// pseudo-random-but-deterministic sequence of moves.
func TestNavigationNeverLandsOnAHiddenOrInvalidItem(t *testing.T) {
	items := grid()
	items[1].Visible = false
	items[4].Visible = false

	moves := []struct {
		name string
		fn   func([]Item, int, int) int
		dir  int
	}{
		{"down", MoveVertical, 1},
		{"up", MoveVertical, -1},
		{"right", MoveHorizontal, 1},
		{"left", MoveHorizontal, -1},
		{"nextSection", JumpSection, 1},
		{"prevSection", JumpSection, -1},
	}

	idx := FirstVisible(items)
	if idx < 0 {
		t.Fatal("no visible item to start from")
	}
	for step := 0; step < 500; step++ {
		m := moves[(step*7+step/3)%len(moves)]
		idx = m.fn(items, idx, m.dir)
		if idx < 0 || idx >= len(items) {
			t.Fatalf("step %d (%s): idx %d out of range", step, m.name, idx)
		}
		if !items[idx].Visible {
			t.Fatalf("step %d (%s): landed on hidden index %d", step, m.name, idx)
		}
	}
}

// TestMoveVerticalTieBreakIsColumnNotSliceOrder pins the rule stated on
// MoveVertical: with two equidistant candidates the lower column wins.
//
// It used to fall out of slice order — the first equidistant item found — which
// matched only because every focus list in the drawer happens to be appended left
// to right. The rule is what makes down-then-up return to where you started, so
// it has to hold for any input order, not just the convenient one.
func TestMoveVerticalTieBreakIsColumnNotSliceOrder(t *testing.T) {
	// Cursor at col 1; the target row has cols 0 and 2, both one away.
	orders := map[string][]Item{
		"target row listed high column first": {
			{Row: 0, Col: 1, Visible: true},
			{Row: 1, Col: 2, Visible: true},
			{Row: 1, Col: 0, Visible: true},
		},
		"target row listed low column first": {
			{Row: 0, Col: 1, Visible: true},
			{Row: 1, Col: 0, Visible: true},
			{Row: 1, Col: 2, Visible: true},
		},
	}
	for name, items := range orders {
		t.Run(name, func(t *testing.T) {
			got := MoveVertical(items, 0, 1)
			if items[got].Col != 0 {
				t.Errorf("tie went to col %d, want the lower column 0", items[got].Col)
			}
		})
	}
}

// TestMoveVerticalRoundTrips is the property the tie-break exists to provide:
// moving down and back up returns to the starting item, whatever order the rows
// were appended in.
func TestMoveVerticalRoundTrips(t *testing.T) {
	items := []Item{
		{Row: 0, Col: 2, Visible: true}, // 0
		{Row: 0, Col: 0, Visible: true}, // 1
		{Row: 1, Col: 2, Visible: true}, // 2
		{Row: 1, Col: 0, Visible: true}, // 3
	}
	for start := range items {
		down := MoveVertical(items, start, 1)
		back := MoveVertical(items, down, -1)
		if items[back].Col != items[start].Col {
			t.Errorf("from idx %d (col %d): down to col %d, up to col %d — not a round trip",
				start, items[start].Col, items[down].Col, items[back].Col)
		}
	}
}
