// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package focusgrid

// builder.go — declaring a layout instead of counting rows by hand.
//
// The drawer's focus lists were built by incrementing a `row` variable between
// appends, interleaved with the conditionals that decide whether a section
// exists at all. That works and it is what shipped, but it has three failure
// modes, all of which are invisible until a gamepad user hits them:
//
//   - Inserting a control means renumbering every row below it, by hand, in a
//     function where the numbering is spread across ~130 lines.
//   - A grid's height is written twice — once as `row+i/cols` in the loop and
//     once as the caller's guess at where the grid ended. The mode grid does
//     exactly this (`modeBase + i/3`, then `row = modeBase + 1`), which is
//     correct only while there are exactly six modes. A seventh silently
//     overlaps the row below it.
//   - Every coordinate assumes the panel stacks downward. A quickbar along the
//     top or bottom edge needs the transpose of all of them, and there is no
//     single place to apply it.
//
// A Builder takes the declaration — "three items on a line, then one, then a
// 3-wide grid of six" — and computes the coordinates. Orientation is applied
// once, at the end, as a transpose.
//
// It deliberately yields coordinates rather than Items: Visible is a live
// property of a widget the pure layer cannot see, so a Builder that returned
// Items would have to invent a value for it. The GTK layer zips these onto its
// own item structs.

// Orientation is the axis a layout stacks along.
type Orientation int

const (
	// Vertical stacks lines downward: the right-edge drawer, where a "line" is
	// a row and successive lines increase Row. This is the canonical space the
	// Builder computes in.
	Vertical Orientation = iota
	// Horizontal runs lines along the edge: a top- or bottom-edge quickbar,
	// where a "line" is a column and successive lines increase Col. It is
	// exactly the transpose of Vertical — not a separate set of rules — so the
	// two can never drift apart.
	Horizontal
)

// Coord is one item's position in the focus grid.
type Coord struct {
	Row     int
	Col     int
	Section string
}

// Builder accumulates a layout declaration. The zero value is not usable; call
// NewBuilder. Methods return the coordinates they appended so a caller can zip
// them onto widgets in the same expression, and every coordinate is also
// retained for Build.
type Builder struct {
	orient  Orientation
	section string
	line    int // next free line in canonical (vertical) space
	coords  []Coord
}

// NewBuilder returns a Builder that lays out for orient.
func NewBuilder(orient Orientation) *Builder {
	return &Builder{orient: orient}
}

// Section sets the section name applied to everything appended after it.
// Sections drive the shoulder-button jumps, so they are a property of the
// declaration rather than of each item.
func (b *Builder) Section(name string) *Builder {
	b.section = name
	return b
}

// Line appends n items side by side on one line and advances past it.
//
// n <= 0 appends nothing and consumes no line. That matters: the caller's
// conditionals decide whether a section exists, and a skipped section must not
// leave a gap that shifts everything below it relative to another build of the
// same view.
func (b *Builder) Line(n int) []Coord {
	return b.Grid(n, n)
}

// Grid appends n items wrapped at cols per line, advancing past every line it
// used — including a partial last one. That is the arithmetic the hand-written
// version got right in the loop and wrong in the follow-up assignment.
//
// cols <= 0 is treated as a single line of n, which is what a caller asking
// for "no wrapping" means; it cannot be a division by zero.
func (b *Builder) Grid(n, cols int) []Coord {
	if n <= 0 {
		return nil
	}
	if cols <= 0 {
		cols = n
	}
	out := make([]Coord, 0, n)
	for i := range n {
		out = append(out, Coord{
			Row:     b.line + i/cols,
			Col:     i % cols,
			Section: b.section,
		})
	}
	b.line += (n + cols - 1) / cols // ceil: a partial last line still consumes one
	b.coords = append(b.coords, out...)
	return out
}

// Build returns every coordinate appended, in declaration order, with the
// orientation applied.
func (b *Builder) Build() []Coord {
	out := make([]Coord, len(b.coords))
	copy(out, b.coords)
	if b.orient == Horizontal {
		for i := range out {
			out[i].Row, out[i].Col = out[i].Col, out[i].Row
		}
	}
	return out
}

// Lines reports how many lines the declaration has used so far, in canonical
// space. Useful for a caller that needs to place something relative to what it
// has already declared.
func (b *Builder) Lines() int { return b.line }
