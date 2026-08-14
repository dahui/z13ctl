// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// errbar.go — the drawer's single user-facing error surface.
//
// Every daemon call in this package runs on a background goroutine and used to
// drop its error into slog and return, which made a failed operation look like a
// button that did nothing (see z13ctl issue #14, where "Save TDP" was silently
// rejected with "permission denied" for weeks). The bar lives outside the view
// stack, between it and the bottom bar, so one instance serves every view in all
// three backends.
//
// Deliberately a plain Box + Label + Button: popovers are not composited under
// gamescope, and gtk.Revealer smears during the slide animation. Buttons use a
// CAPTURE-phase gesture internally, so touch works in gamescope without the
// addTouchActivate workaround needed for CheckButton and Switch.
//
// The widgets live on errBarView rather than on Window. Window.reportError and
// friends stay as thin delegates: the ~50 call sites across the package say what
// they mean already, and a refactor whose point is to shrink the Window struct
// should not also churn every caller.
//
// There is one bar per *surface*, not one per process: the full window is a
// separate toplevel, and an error raised from a view inside it would otherwise
// be shown on the drawer's bar, which is hidden at the time. reportError fans
// out to every bar that has been built and each suppresses itself when its own
// surface is closed — the report is cheap, and a message written to a bar
// nobody can see costs nothing, whereas the one the user is looking at showing
// nothing costs them the reason their save failed.

import (
	"log/slog"

	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// errLabelMaxChars caps the error label's natural width request. The drawer is
// drawerWidth (320px) wide; at the 10px .error-text size this is comfortably
// inside the content area, so the bar never drives the drawer wider.
const errLabelMaxChars = 34

// errBarView is the error strip: hidden by default, shown by report, hidden by
// clear.
type errBarView struct {
	w *Window

	// visible reports whether this bar's own surface is on screen. A report
	// that lands after its surface closed is dropped rather than left waiting
	// to greet the next open — the stale-failure case hide()'s clearError
	// exists for.
	visible func() bool

	bar     *gtk.Box
	label   *gtk.Label
	dismiss *gtk.Button
}

// newErrorBar returns the hidden-by-default error strip for one surface.
func newErrorBar(w *Window, visible func() bool) *errBarView {
	e := &errBarView{w: w, visible: visible}

	e.bar = gtk.NewBox(gtk.OrientationHorizontal, 4)
	e.bar.AddCSSClass("error-bar")
	e.bar.SetVisible(false)

	e.label = gtk.NewLabel("")
	e.label.SetXAlign(0)
	e.label.SetHExpand(true)
	e.label.AddCSSClass("error-text")
	// Daemon messages embed sysfs paths, which are single unbreakable tokens.
	// A plain wrapping label reports the longest such token as its minimum width,
	// which widens the whole drawer to fit
	// "/sys/devices/platform/asus-nb-wmi/ppt_pl1_spl". WrapWordChar lets pango
	// break mid-token, and MaxWidthChars caps the natural width so the label
	// wraps into the drawer instead of stretching it.
	e.label.SetWrap(true)
	e.label.SetWrapMode(pango.WrapWordChar)
	e.label.SetMaxWidthChars(errLabelMaxChars)
	e.bar.Append(e.label)

	e.dismiss = gtk.NewButton()
	e.dismiss.SetIconName("window-close-symbolic")
	w.setHint(e.dismiss, "Dismiss")
	e.dismiss.AddCSSClass("error-dismiss")
	e.dismiss.SetVAlign(gtk.AlignStart)
	e.dismiss.ConnectClicked(func() { e.clear() })
	e.bar.Append(e.dismiss)

	return e
}

// errBarRow places the error bar after every other row in every view's focus
// grid. The bar is appended outside the view stack, so it has no natural row
// number shared with the view in front of it; a value beyond any real row keeps
// it last wherever it appears.
const errBarRow = 10000

// errBarFocusItem returns the gamepad entry for the dismiss button, for appending
// to each view's focus list.
//
// Without it a controller could not dismiss an error at all — the only ways out
// were to close the drawer or to complete an operation successfully, which is
// exactly what a user staring at a failure is unsure how to do. It is only
// navigable while the bar is showing.
func (e *errBarView) focusItem() focusItem {
	item := focusItem{
		row: errBarRow, col: 0, section: "error",
		isVisible:  func() bool { return e != nil && e.bar != nil && e.bar.IsVisible() },
		onActivate: func() { e.clear() },
	}
	if e != nil {
		item.widget = e.dismiss
	}
	return item
}

// report shows err in the bar. Safe from any goroutine; the widget work is
// marshalled onto the GTK main thread.
func (e *errBarView) report(op, msg string) {
	glib.IdleAdd(func() {
		if e.bar == nil || e.label == nil {
			return
		}
		// A call still in flight when the drawer closes lands here afterwards, and
		// showing the bar then means it is already up the next time the drawer
		// opens — the stale failure hide()'s clearError exists to prevent. The
		// journal still has it.
		if e.visible != nil && !e.visible() {
			slog.Debug("error suppressed: surface already closed", "op", op)
			return
		}
		// Most recent error wins; the bar shows one message at a time.
		e.label.SetLabel(msg)
		e.bar.SetVisible(true)
	})
}

// clear hides the bar. Must be called from the GTK main thread.
func (e *errBarView) clear() {
	if e.bar == nil {
		return
	}
	e.bar.SetVisible(false)
	if e.label != nil {
		e.label.SetLabel("")
	}
}

// reportError shows err in the error bar and logs it. Safe to call from any
// goroutine: every daemon call in this package runs in its own goroutine, so the
// widget work is marshalled onto the GTK main thread.
//
// op should name the operation the way the user thinks of it ("Save TDP"), not
// the function that failed.
func (w *Window) reportError(op string, err error) {
	if err == nil {
		return
	}
	slog.Warn("operation failed", "op", op, "err", err)
	for _, e := range w.errBars() {
		e.report(op, op+": "+err.Error())
	}
}

// errBars returns every error bar that has been built: the drawer's, and the
// full window's when it exists.
func (w *Window) errBars() []*errBarView {
	out := make([]*errBarView, 0, 2)
	if w.errView != nil {
		out = append(out, w.errView)
	}
	if w.mainWin != nil && w.mainWin.errView != nil {
		out = append(out, w.mainWin.errView)
	}
	return out
}

// clearError hides the error bar. Must be called from the GTK main thread.
// Called on each successful operation and from hide(), so a stale failure does
// not greet the user the next time the drawer opens.
func (w *Window) clearError() {
	for _, e := range w.errBars() {
		e.clear()
	}
}

// clearErrorAsync is clearError for callers on a background goroutine — the
// success path of the same calls that use reportError.
func (w *Window) clearErrorAsync() {
	glib.IdleAdd(func() { w.clearError() })
}
