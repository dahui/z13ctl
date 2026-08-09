// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// focus.go — 2D grid gamepad focus navigation with modal slider editing.
//
// Items are arranged in a grid (row + col). D-pad up/down moves between rows
// (preserving column where possible), left/right moves within a row. A activates
// buttons/switches or enters edit mode for sliders (D-pad left/right adjusts,
// A commits, B cancels). Shoulder buttons jump between sections.
//
// Per-view focus lists: main, theme, and color views each have their own item
// list. swapFocusList switches between them on view change.

import (
	"log/slog"

	"github.com/dahui/z13gui/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// focusItem represents a single gamepad-navigable element.
type focusItem struct {
	widget     gtk.Widgetter  // widget to highlight with .gamepad-focus
	row        int            // visual row number
	col        int            // column within row
	section    string         // section name for shoulder-button jumping
	isVisible  func() bool    // false if parent section is hidden; nil = always visible
	onActivate func()         // A button: toggle/activate (non-editable items)
	editable   bool           // true for sliders — A enters edit mode instead of activating
	onLeft     func()         // D-pad left while editing: decrease value
	onRight    func()         // D-pad right while editing: increase value
	getValue   func() float64 // read current value (for cancel/restore)
	setValue   func(float64)  // restore value on cancel
}

// visible returns true if this item should be navigable.
func (fi *focusItem) visible() bool {
	if fi.isVisible != nil && !fi.isVisible() {
		return false
	}
	return gtk.BaseWidget(fi.widget).IsVisible()
}

// gridSnapshot flattens focusItems into the pure representation focusgrid works
// on, evaluating each item's visibility exactly once so a single press sees a
// consistent view even if a widget changes underneath it.
func (w *Window) gridSnapshot() []focusgrid.Item {
	items := make([]focusgrid.Item, len(w.focusItems))
	for i := range w.focusItems {
		fi := &w.focusItems[i]
		items[i] = focusgrid.Item{
			Row:     fi.row,
			Col:     fi.col,
			Section: fi.section,
			Visible: fi.visible(),
		}
	}
	return items
}

// navigate applies a focusgrid move. The grid functions return the index
// unchanged when a move is impossible and tolerate an out-of-range index, so
// there is nothing to guard here.
func (w *Window) navigate(move func([]focusgrid.Item, int, int) int, dir int) {
	if next := move(w.gridSnapshot(), w.focusIdx, dir); next != w.focusIdx {
		w.setFocusIdx(next)
	}
}

// moveVertical moves focus to the nearest visible item in the next (dir=+1) or
// previous (dir=-1) row, preserving column where possible.
func (w *Window) moveVertical(dir int) { w.navigate(focusgrid.MoveVertical, dir) }

// moveHorizontal moves focus within the current row, wrapping at its edges.
func (w *Window) moveHorizontal(dir int) { w.navigate(focusgrid.MoveHorizontal, dir) }

// jumpSection jumps to the first visible item of the adjacent section.
func (w *Window) jumpSection(dir int) { w.navigate(focusgrid.JumpSection, dir) }

// focusFirstVisible moves focus to the first visible item, if there is one.
func (w *Window) focusFirstVisible() {
	if idx := focusgrid.FirstVisible(w.gridSnapshot()); idx >= 0 {
		w.setFocusIdx(idx)
	}
}

// activateOrEdit handles the A button: if the focused item is editable (slider),
// enters edit mode. Otherwise calls onActivate directly.
func (w *Window) activateOrEdit() {
	if w.focusIdx >= len(w.focusItems) {
		return
	}
	fi := w.focusItems[w.focusIdx]
	if fi.editable {
		w.enterEditMode()
	} else if fi.onActivate != nil {
		fi.onActivate()
	}
}

// enterEditMode starts modal slider editing on the focused item.
func (w *Window) enterEditMode() {
	if w.focusIdx >= len(w.focusItems) {
		return
	}
	fi := w.focusItems[w.focusIdx]
	if fi.getValue != nil {
		w.editOriginalValue = fi.getValue()
	}
	w.focusEditing = true
	gtk.BaseWidget(fi.widget).AddCSSClass("gamepad-editing")
	slog.Debug("gamepad: edit mode entered", "section", fi.section)
}

// exitEditMode leaves slider editing. If commit is false, restores the
// original value before editing started.
func (w *Window) exitEditMode(commit bool) {
	if !w.focusEditing {
		return
	}
	w.focusEditing = false
	if w.focusIdx < len(w.focusItems) {
		fi := w.focusItems[w.focusIdx]
		gtk.BaseWidget(fi.widget).RemoveCSSClass("gamepad-editing")
		if !commit && fi.setValue != nil {
			fi.setValue(w.editOriginalValue)
		}
	}
	slog.Debug("gamepad: edit mode exited", "commit", commit)
}

// adjustFocus calls OnLeft (dir=-1) or OnRight (dir=+1) on the focused item.
func (w *Window) adjustFocus(dir int) {
	if w.focusIdx >= len(w.focusItems) {
		return
	}
	fi := w.focusItems[w.focusIdx]
	if dir < 0 && fi.onLeft != nil {
		fi.onLeft()
	} else if dir > 0 && fi.onRight != nil {
		fi.onRight()
	}
}

// setFocusIdx updates the focus index and moves the CSS highlight.
func (w *Window) setFocusIdx(idx int) {
	if w.focusIdx < len(w.focusItems) {
		gtk.BaseWidget(w.focusItems[w.focusIdx].widget).RemoveCSSClass("gamepad-focus")
	}
	w.focusIdx = idx
	if idx < len(w.focusItems) {
		gtk.BaseWidget(w.focusItems[idx].widget).AddCSSClass("gamepad-focus")
		w.ensureVisible(w.focusItems[idx].widget)
		slog.Debug("gamepad: focus", "idx", idx,
			"row", w.focusItems[idx].row, "col", w.focusItems[idx].col,
			"section", w.focusItems[idx].section)
	}
}

// showGamepadFocus enables the gamepad focus indicator.
func (w *Window) showGamepadFocus() {
	if w.gamepadActive {
		return
	}
	w.gamepadActive = true
	w.focusFirstVisible()
}

// hideGamepadFocus removes the gamepad focus indicator (e.g. on mouse movement).
func (w *Window) hideGamepadFocus() {
	if !w.gamepadActive {
		return
	}
	if w.focusEditing {
		w.exitEditMode(true)
	}
	w.gamepadActive = false
	if w.focusIdx < len(w.focusItems) {
		gtk.BaseWidget(w.focusItems[w.focusIdx].widget).RemoveCSSClass("gamepad-focus")
	}
}

// swapFocusList switches to a different focus item list (e.g. on view change).
// Clears the current highlight and resets focus to the first visible item.
func (w *Window) swapFocusList(items []focusItem) {
	if w.focusIdx < len(w.focusItems) {
		gtk.BaseWidget(w.focusItems[w.focusIdx].widget).RemoveCSSClass("gamepad-focus")
		gtk.BaseWidget(w.focusItems[w.focusIdx].widget).RemoveCSSClass("gamepad-editing")
	}
	w.focusEditing = false
	w.focusItems = items
	w.focusIdx = 0
	if w.gamepadActive {
		w.focusFirstVisible()
	}
}

// ensureVisible scrolls the active ScrolledWindow so that widget is in view.
// Translates the widget's position to viewport-relative coordinates, then
// converts to content-relative coordinates for ClampPage.
func (w *Window) ensureVisible(widget gtk.Widgetter) {
	var scroll *gtk.ScrolledWindow
	if w.viewStack != nil {
		switch w.viewStack.VisibleChildName() {
		case "main":
			scroll = w.mainScroll
		case "theme":
			scroll = w.themeScroll
		case "custom":
			scroll = w.customScroll
		default:
			return // color view has no scroll
		}
	}
	if scroll == nil {
		return
	}

	adj := scroll.VAdjustment()
	base := gtk.BaseWidget(widget)

	// TranslateCoordinates gives viewport-relative Y (GTK4 accounts for
	// scroll transforms). Add the current scroll offset to get the widget's
	// position in the full scroll content.
	_, viewY, ok := base.TranslateCoordinates(&scroll.Widget, 0, 0) //nolint:staticcheck // TranslateCoordinates is deprecated in GTK4 but avoids graphene import; still works in gotk4
	if !ok {
		return
	}
	contentY := adj.Value() + viewY
	h := float64(base.Height())
	const scrollPadding = 20 // breathing room below focused widget
	adj.ClampPage(contentY, contentY+h+scrollPadding)
}

// scaleAdjust returns onLeft/onRight/getValue/setValue functions for a slider.
func scaleAdjust(sc *gtk.Scale, step float64) (onLeft, onRight func(), getValue func() float64, setValue func(float64)) {
	adj := sc.Adjustment()
	onLeft = func() {
		v := adj.Value() - step
		if v < adj.Lower() {
			v = adj.Lower()
		}
		adj.SetValue(v)
	}
	onRight = func() {
		v := adj.Value() + step
		if v > adj.Upper() {
			v = adj.Upper()
		}
		adj.SetValue(v)
	}
	getValue = func() float64 { return adj.Value() }
	setValue = func(v float64) { adj.SetValue(v) }
	return
}
