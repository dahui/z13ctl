// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// formrow.go — the desktop form row: one setting per line, its name in a label
// column, its control filling the rest.
//
// This is the shape the full window's controls take, where the drawer's take a
// title above a full-width widget. The difference is not decoration. A 320px
// touch column has room for one thing at a time and a thumb needs the whole
// width, so stacking is the only option there; a window has room to put twelve
// settings on screen at once, and what makes twelve settings *readable* is that
// their names line up and their controls line up. Every desktop settings panel
// on the machine is built this way for that reason.
//
// The sections that appear on both surfaces take a `desktop bool` and build one
// shape or the other. That is deliberately a branch inside each section rather
// than a second section type: what differs is the arrangement of the same
// widgets, and two types would mean two sync paths and two focus lists for one
// control (Jeff, 2026-08-14 — UX first, and duplicated *instances* are fine;
// duplicated *behaviour* is what actually costs).

import "github.com/diamondburned/gotk4/pkg/gtk/v4"

const (
	// formLabelWidth is the name column. Fixed so names align down a card —
	// which is the entire point of the layout, and is lost the moment one row
	// sizes to its own label.
	formLabelWidth = 104

	// formIndent is how far a dependent row is inset from its parent. Used by
	// the autoswitch targets, which are meaningless without the switch above
	// them and should read as belonging to it.
	formIndent = 16
)

// formRow lays out one setting: name, then control.
func formRow(name string, control gtk.Widgetter) *gtk.Box {
	return formRowIndented(name, control, 0)
}

// subFormRow lays out a setting that depends on the one above it.
func subFormRow(name string, control gtk.Widgetter) *gtk.Box {
	return formRowIndented(name, control, formIndent)
}

func formRowIndented(name string, control gtk.Widgetter, indent int) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)

	lbl := gtk.NewLabel(name)
	lbl.AddCSSClass("form-label")
	lbl.SetXAlign(0)
	lbl.SetHAlign(gtk.AlignStart)
	// Centred against the control rather than top-aligned: a row whose control
	// is a segmented button group is taller than its name, and a name pinned to
	// the top of it reads as a heading for the row rather than as its label.
	lbl.SetVAlign(gtk.AlignCenter)
	lbl.SetSizeRequest(formLabelWidth-indent, -1)
	lbl.SetMarginStart(indent)
	row.Append(lbl)

	w := gtk.BaseWidget(control)
	w.SetHExpand(true)
	row.Append(control)
	return row
}

// firstLabel returns a row's name label, for the one control whose name is not
// fixed (the refresh rate names its output on a multi-screen machine). Asking
// the built row beats returning the label from formRow: every other caller
// would then have to ignore a second return value it has no use for.
func firstLabel(row *gtk.Box) *gtk.Label {
	child := row.FirstChild()
	if child == nil {
		return nil
	}
	if lbl, ok := child.(*gtk.Label); ok {
		return lbl
	}
	return nil
}

// formValueLabel is the readout that sits after a slider — the number you would
// otherwise have to drag the handle to discover. `.scale-value` is the drawer's
// class for the same job, reused so a theme styles both.
func formValueLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCSSClass("scale-value")
	l.SetXAlign(1)
	// Wide enough for the longest readout either slider produces ("Medium"),
	// so the right-aligned numbers still line up under each other.
	l.SetSizeRequest(64, -1)
	return l
}

// formSlider pairs a scale with its readout. The scale draws no value of its
// own: GtkScale's built-in value floats above the handle and moves with it,
// which is a touch affordance (your thumb is already there) and on a desktop is
// a number that will not hold still long enough to read.
func formSlider(sc *gtk.Scale, value *gtk.Label) *gtk.Box {
	sc.SetDrawValue(false)
	sc.SetHExpand(true)
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.Append(sc)
	row.Append(value)
	return row
}

// sectionCard is one boxed list: a heading over a column of form rows.
func sectionCard(heading string, rows ...gtk.Widgetter) *gtk.Box {
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("section-card")
	card.AddCSSClass("dash-control")
	card.Append(sectionLabel(heading))
	for _, r := range rows {
		card.Append(r)
	}
	return card
}
