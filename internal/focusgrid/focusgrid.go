// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package focusgrid is the gamepad focus navigation for the drawer: given a set
// of items laid out in rows, columns and sections, it answers "which item does
// this D-pad press move to".
//
// It exists as a separate package because internal/gui needs CGO and GTK4 headers
// and so cannot be unit tested, while this is the input path every gamepad user in
// Steam Gaming Mode depends on. It is pure index arithmetic over a snapshot: the
// GTK layer evaluates each item's visibility once, calls a function here, and
// applies the returned index. No widgets, no GTK types.
//
// Every function takes the current index and returns the new one. They return
// idx unchanged rather than a sentinel when a move is impossible, so callers can
// apply the result unconditionally.
//
// # Bounds
//
// All functions tolerate an out-of-range idx by returning it unchanged. The GTK
// side previously guarded this in four of its seven entry points and not the other
// three, which was a latent panic and left the intended contract ambiguous. It is
// stated here once instead.
package focusgrid

import "sort"

// Item is one navigable element, flattened from the widget tree. Visible is
// evaluated by the caller before navigating, so a single press sees a consistent
// snapshot even if widget visibility changes underneath it.
type Item struct {
	Row     int    // visual row
	Col     int    // column within the row
	Section string // grouping for shoulder-button jumps
	Visible bool
}

// inRange reports whether idx addresses an item.
func inRange(items []Item, idx int) bool {
	return idx >= 0 && idx < len(items)
}

// FirstVisible returns the index of the first visible item, or -1 if none are.
// Used when focus is first shown and after switching views.
func FirstVisible(items []Item) int {
	for i := range items {
		if items[i].Visible {
			return i
		}
	}
	return -1
}

// visibleRows returns the sorted, unique row numbers that contain at least one
// visible item.
func visibleRows(items []Item) []int {
	seen := make(map[int]bool)
	var rows []int
	for i := range items {
		if items[i].Visible && !seen[items[i].Row] {
			seen[items[i].Row] = true
			rows = append(rows, items[i].Row)
		}
	}
	sort.Ints(rows)
	return rows
}

// MoveVertical moves focus to the next (dir=+1) or previous (dir=-1) row that has
// a visible item, wrapping at the ends. Within the target row it picks the item
// whose column is nearest the current one, so travelling down a column of
// differently-shaped rows stays roughly in line.
//
// Ties go to the lower column: with the cursor between two equidistant items,
// moving down twice and back up twice returns to where it started.
func MoveVertical(items []Item, idx, dir int) int {
	if !inRange(items, idx) {
		return idx
	}
	rows := visibleRows(items)
	if len(rows) <= 1 {
		return idx // nowhere else to go
	}
	cur := items[idx]

	curPos := -1
	for i, r := range rows {
		if r == cur.Row {
			curPos = i
			break
		}
	}
	if curPos == -1 {
		// The focused item is itself hidden — its row is not in the visible set.
		// Fall back to the first visible item rather than refusing to move, which
		// would otherwise trap focus on an invisible widget.
		if first := FirstVisible(items); first >= 0 {
			return first
		}
		return idx
	}

	targetRow := rows[((curPos+dir)%len(rows)+len(rows))%len(rows)]

	best := -1
	bestDist := 0
	for i := range items {
		if items[i].Row != targetRow || !items[i].Visible {
			continue
		}
		d := cur.Col - items[i].Col
		if d < 0 {
			d = -d
		}
		// The tie-break is on column, not on position in the slice. Taking the
		// first equidistant item found made the result depend on the order the
		// caller happened to append its rows in: every list built today runs
		// left to right, so this matched, but nothing enforced it and the rule
		// documented above quietly did not hold for any other order.
		switch {
		case best == -1, d < bestDist:
			best, bestDist = i, d
		case d == bestDist && items[i].Col < items[best].Col:
			best = i
		}
	}
	if best == -1 {
		return idx
	}
	return best
}

// MoveHorizontal moves focus to the next (dir=+1) or previous (dir=-1) visible
// item in the same row, ordered by column and wrapping at the row's edges.
func MoveHorizontal(items []Item, idx, dir int) int {
	if !inRange(items, idx) {
		return idx
	}
	row := items[idx].Row

	var rowItems []int
	for i := range items {
		if items[i].Row == row && items[i].Visible {
			rowItems = append(rowItems, i)
		}
	}
	if len(rowItems) <= 1 {
		return idx
	}
	sort.Slice(rowItems, func(a, b int) bool {
		return items[rowItems[a]].Col < items[rowItems[b]].Col
	})

	pos := -1
	for i, ri := range rowItems {
		if ri == idx {
			pos = i
			break
		}
	}
	if pos == -1 {
		// Focused item is hidden; enter the row at its first item.
		return rowItems[0]
	}
	next := ((pos+dir)%len(rowItems) + len(rowItems)) % len(rowItems)
	return rowItems[next]
}

// Sections returns the section names in visual order — by row, then by the order
// items appear within a row — deduplicated.
func Sections(items []Item) []string {
	var out []string
	seen := make(map[string]bool)
	for _, r := range visibleRows(items) {
		for i := range items {
			if items[i].Row == r && items[i].Visible && !seen[items[i].Section] {
				seen[items[i].Section] = true
				out = append(out, items[i].Section)
			}
		}
	}
	return out
}

// JumpSection moves focus to the first visible item of the next (dir=+1) or
// previous (dir=-1) section, wrapping. Driven by the shoulder buttons.
func JumpSection(items []Item, idx, dir int) int {
	if !inRange(items, idx) {
		return idx
	}
	sections := Sections(items)
	if len(sections) <= 1 {
		return idx
	}
	cur := items[idx].Section

	curPos := -1
	for i, s := range sections {
		if s == cur {
			curPos = i
			break
		}
	}
	if curPos == -1 {
		// Focused item is hidden, so its section may not be in the visible set.
		if first := FirstVisible(items); first >= 0 {
			return first
		}
		return idx
	}

	target := sections[((curPos+dir)%len(sections)+len(sections))%len(sections)]
	for i := range items {
		if items[i].Section == target && items[i].Visible {
			return i
		}
	}
	return idx
}
