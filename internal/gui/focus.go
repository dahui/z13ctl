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

	"github.com/dahui/voltaire/v2/internal/focusgrid"
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
//
// Insensitive widgets are skipped, not merely un-activatable. GTK's
// gtk_widget_activate() emits the activate signal without consulting
// sensitivity — GtkButton turns that straight into "clicked" — so the
// gamepad path would otherwise fire controls a pointer physically cannot,
// and every one of them is desensitized precisely because the daemon would
// refuse it: an empty profile's activate button, Save As with no custom
// profile to copy, Reset Fans under the high-TDP floor, Delete on the active
// profile. Reachable-but-refused is worse than unreachable, because the
// controller user gets an error bar where a pointer user gets a greyed
// control and a tooltip.
//
// Skipping here rather than in each onActivate is what keeps the rule in one
// place: the result feeds focusgrid.Item.Visible, so the pure navigation
// logic treats these exactly as it already treats hidden items.
func (fi *focusItem) visible() bool {
	if fi.isVisible != nil && !fi.isVisible() {
		return false
	}
	w := gtk.BaseWidget(fi.widget)
	return w.IsVisible() && w.IsSensitive()
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
	// Re-check rather than trusting that navigation skipped it: a state
	// refresh can desensitize the focused widget after focus landed on it —
	// autoswitch selecting a profile makes its Delete illegal mid-edit — and
	// nothing re-runs navigation on a sync.
	if !fi.visible() {
		return
	}
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
		// Focused-control help — the same hint map the pointer dwell reads,
		// shown immediately: a controller has no hover to dwell with.
		// showHint hides the label when the widget has no registered text.
		w.showHint(w.focusItems[idx].widget)
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
	w.hideHint()
	if w.focusIdx < len(w.focusItems) {
		gtk.BaseWidget(w.focusItems[w.focusIdx].widget).RemoveCSSClass("gamepad-focus")
	}
}

// focusFrame is one suspended focus list — the drawer's own list while a
// popup is open. The stack lives here rather than in focusgrid because a
// frame holds gtk.Widgetter references; the pure re-anchoring policy applied
// when a frame resumes is focusgrid.Restore.
type focusFrame struct {
	items []focusItem
	idx   int
}

// clearFocusHighlight removes the focus and editing markers from the focused
// item and leaves edit mode, ahead of replacing the focus list.
func (w *Window) clearFocusHighlight() {
	if w.focusIdx < len(w.focusItems) {
		base := gtk.BaseWidget(w.focusItems[w.focusIdx].widget)
		base.RemoveCSSClass("gamepad-focus")
		base.RemoveCSSClass("gamepad-editing")
	}
	w.focusEditing = false
}

// pushFocusList suspends the current focus list and makes items the active
// one — the popup's contents. Balanced by popFocusList via closePopup.
func (w *Window) pushFocusList(items []focusItem) {
	w.clearFocusHighlight()
	w.focusStack = append(w.focusStack, focusFrame{items: w.focusItems, idx: w.focusIdx})
	w.focusItems = items
	w.focusIdx = 0
	if w.gamepadActive {
		w.focusFirstVisible()
	}
}

// popFocusList resumes the most recently suspended focus list. The item that
// was focused when the popup opened may have been hidden or desensitized
// while it was open — selecting a profile can make the very button that
// opened the dropdown illegal — so focusgrid.Restore re-anchors rather than
// trusting the saved index.
func (w *Window) popFocusList() {
	if len(w.focusStack) == 0 {
		return
	}
	w.clearFocusHighlight()
	frame := w.focusStack[len(w.focusStack)-1]
	w.focusStack = w.focusStack[:len(w.focusStack)-1]
	w.focusItems = frame.items
	w.focusIdx = focusgrid.Restore(w.gridSnapshot(), frame.idx)
	if w.gamepadActive && w.focusIdx >= 0 && w.focusIdx < len(w.focusItems) {
		gtk.BaseWidget(w.focusItems[w.focusIdx].widget).AddCSSClass("gamepad-focus")
		w.ensureVisible(w.focusItems[w.focusIdx].widget)
	}
}

// swapFocusList switches to a different focus item list (e.g. on view change).
// Clears the current highlight and resets focus to the first visible item.
func (w *Window) swapFocusList(items []focusItem) {
	if len(w.focusStack) > 0 {
		// Every view switch and hide() closes popups before swapping, so a
		// populated stack here means a closePopup call was missed somewhere.
		slog.Debug("swapFocusList: discarding suspended focus frames", "depth", len(w.focusStack))
		w.focusStack = nil
	}
	w.clearFocusHighlight()
	w.focusItems = items
	w.focusIdx = 0
	if w.gamepadActive {
		w.focusFirstVisible()
	}
}

// activeScroll returns the scroller of the view currently on screen, or nil
// when that view does not scroll (the colour picker). An open popup's own
// scroller wins: it is what gamepad navigation and ensureVisible must move
// while the popup has focus, and the scrim keeps the view behind it still.
func (w *Window) activeScroll() *gtk.ScrolledWindow {
	if w.popupOpen() {
		return w.popup.scroll
	}
	if w.viewStack == nil {
		return nil
	}
	switch w.viewStack.VisibleChildName() {
	case "main":
		return w.mainScroll
	case "theme":
		return w.themeScroll
	case "custom":
		return w.customScroll
	}
	return nil
}

// ensureVisible scrolls the active ScrolledWindow so that widget is in view.
// Translates the widget's position to viewport-relative coordinates, then
// converts to content-relative coordinates for ClampPage.
func (w *Window) ensureVisible(widget gtk.Widgetter) {
	scroll := w.activeScroll()
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

// wheelScrollsView makes the mouse wheel scroll the drawer when the pointer
// happens to be over a slider, instead of changing that slider's value.
//
// GtkRange consumes scroll events to adjust itself, so a wheel flick that
// passed over the TDP, brightness or battery sliders silently changed a
// hardware setting rather than scrolling — on the Z13, scrolling past PL3
// moved it from 90W to 30W. The drawer is a tall scrolling panel of sliders,
// so the pointer is over one most of the time, and no other control in it
// answers the wheel at all.
//
// A capture-phase controller sees the event before GtkRange's own (bubble)
// handler, so consuming it there is what keeps the range from acting; the
// scroll is then applied to the enclosing scroller by hand. Returning false
// when there is nothing to scroll leaves the colour picker's sliders — the
// one view with no scroller — responding to the wheel exactly as before.
func (w *Window) wheelScrollsView(sc *gtk.Scale) {
	ctl := gtk.NewEventControllerScroll(gtk.EventControllerScrollVertical)
	ctl.SetPropagationPhase(gtk.PhaseCapture)
	ctl.ConnectScroll(func(_, dy float64) bool {
		scroll := w.activeScroll()
		if scroll == nil {
			return false
		}
		adj := scroll.VAdjustment()
		// A fraction of the visible height per notch, rather than the
		// adjustment's step increment: a ScrolledWindow's step is a couple of
		// pixels, which made a wheel flick move the view almost not at all.
		// Page-relative also keeps the feel identical under gamescope, where
		// every dimension is scaled.
		v := adj.Value() + dy*adj.PageSize()*0.12
		if top := adj.Upper() - adj.PageSize(); v > top {
			v = top
		}
		if v < adj.Lower() {
			v = adj.Lower()
		}
		adj.SetValue(v)
		return true
	})
	sc.AddController(ctl)
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
