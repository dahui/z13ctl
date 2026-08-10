// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// popup.go — the in-surface popup layer: a GtkOverlay wrapped around the
// drawer's content, whose overlay children are the popup scrim, the popup
// surface (dropdown lists), and the anchored hint label.
//
// The drawer cannot use GTK's own popups. Every GtkPopover — and so every
// GtkDropDown, GtkMenuButton and tooltip — is a separate GdkSurface:
// gtk_popover_realize() calls gdk_surface_new_popup() unconditionally, and
// GTK4 removed GTK3's set_constrain_to, so there is no in-window mode to ask
// for. gamescope will not composite such a surface as an input-receiving
// window: its override-redirect plane is PID-matched to the focused game, and
// tagging the popup STEAM_OVERLAY lands it in the notification slot — painted
// fullscreen, receiving no input. Every shipping gamescope overlay (HHD,
// Decky, mangoapp) draws its popups inside its own surface; this layer is how
// this drawer does it. See internal/gui/CLAUDE.md for the full accounting.
//
// All sizing decisions live in internal/popupgeom, which is pure and tested;
// the get-child-position handler here only measures widgets and applies the
// returned rectangle — the same split panelgeom has with the overlay backend.

import (
	"github.com/dahui/voltaire/v2/internal/popupgeom"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const (
	popupGap    = 4  // vertical gap between the anchor and the popup, unscaled
	popupMargin = 8  // popup inset from the panel edges, unscaled
	popupMinH   = 96 // below this usable height, the popup overlaps its anchor
)

// popupLayer holds the GtkOverlay and its three permanent overlay children.
// One popup at a time; opening another closes the first.
type popupLayer struct {
	overlay *gtk.Overlay
	scrim   *gtk.Box            // click-away backdrop; also blocks scrolling under the popup
	surface *gtk.Box            // the popup box; carries .drawer so themes style it
	scroll  *gtk.ScrolledWindow // inside surface; absorbs a Shortened placement
	body    gtk.Widgetter       // current popup content, child of scroll
	hint    *gtk.Label          // anchored hint text (hint.go)

	open       bool
	anchor     gtk.Widgetter // widget the surface is placed against
	hintAnchor gtk.Widgetter // widget the hint is placed against (hint.go)
	onClose    func()
	stale      bool // a state sync was suppressed while open; refresh on close
}

// newPopupScroll returns the popup surface's scroller. Deliberately not
// newDrawerScroll: its SetMinContentHeight(240) would stop a popup being
// shorter than 240px, and a popup should be exactly as tall as its content
// when that fits. SetPropagateNaturalHeight is what makes the scroller report
// the body's natural height when measured, and absorb a Shortened placement
// with no re-measurement when it does not fit.
func newPopupScroll() *gtk.ScrolledWindow {
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalHeight(true)
	return scroll
}

// buildPopupLayer wraps content in the overlay and builds the three overlay
// children. Called once from buildContent; nothing is visible until openPopup.
func (w *Window) buildPopupLayer(content gtk.Widgetter) gtk.Widgetter {
	p := &popupLayer{}
	w.popup = p

	p.overlay = gtk.NewOverlay()
	p.overlay.SetChild(content)

	p.scrim = gtk.NewBox(gtk.OrientationVertical, 0)
	p.scrim.AddCSSClass("popup-scrim")
	p.scrim.SetVisible(false)
	// Capture phase, and not touch-only: the finding behind addTouchActivate
	// is that bubble-phase gestures fail for touch under gamescope's XWayland,
	// so the dismissal gesture must not rely on bubbling. (The gamescope
	// backdrop's own bubble-phase gesture is not a precedent — its touch
	// behaviour is an untested assumption.)
	click := gtk.NewGestureClick()
	click.SetPropagationPhase(gtk.PhaseCapture)
	click.ConnectPressed(func(int, float64, float64) { w.closePopup() })
	p.scrim.AddController(click)
	p.overlay.AddOverlay(p.scrim)

	p.surface = gtk.NewBox(gtk.OrientationVertical, 0)
	// .drawer is what makes a user's verbatim theme.css style the popup with
	// zero edits — background, border and text colour all come from the same
	// rules the panel itself uses. Never call SetMeasureOverlay on any of
	// these children: overlay children not contributing to measurement is
	// what keeps a popup from widening the 320px panel.
	p.surface.AddCSSClass("drawer")
	p.surface.AddCSSClass("popup-surface")
	p.surface.SetVisible(false)
	p.scroll = newPopupScroll()
	p.surface.Append(p.scroll)
	p.overlay.AddOverlay(p.surface)
	p.overlay.SetClipOverlay(p.surface, true)

	p.hint = gtk.NewLabel("")
	p.hint.AddCSSClass("drawer")
	p.hint.AddCSSClass("popup-hint")
	p.hint.SetWrap(true)
	p.hint.SetXAlign(0)
	p.hint.SetVisible(false)
	// Display-only: the hint must never take the click aimed at the control
	// beneath it.
	p.hint.SetCanTarget(false)
	p.overlay.AddOverlay(p.hint)
	p.overlay.SetClipOverlay(p.hint, true)

	// Identity by C object pointer: the child the signal hands over is a fresh
	// Go wrapper, never pointer-equal to the field it wraps.
	scrimN := coreglib.BaseObject(p.scrim).Native()
	surfaceN := coreglib.BaseObject(p.surface).Native()
	hintN := coreglib.BaseObject(p.hint).Native()
	p.overlay.ConnectGetChildPosition(func(child gtk.Widgetter) (*gdk.Rectangle, bool) {
		// Every return path MUST produce a non-nil rectangle with ok=true.
		// gotk4 v0.3.1's marshaller for this signal dereferences the returned
		// rectangle BEFORE it consults ok (gtk/v4/gtk_export.go,
		// _gotk4_gtk4_Overlay_ConnectGetChildPosition), so the natural
		// "(nil, false) — use the default position" answer is a segfault
		// inside a C callback, where the stack trace names nothing. A child
		// with nothing to show gets a zero-size rect at the origin instead.
		//
		// No setters in here: this runs during allocation, and a setter
		// re-queues allocation and loops.
		switch coreglib.BaseObject(child).Native() {
		case scrimN:
			if !p.open {
				return rectOf(popupgeom.Rect{}), true
			}
			return rectOf(popupgeom.Rect{W: p.overlay.Width(), H: p.overlay.Height()}), true
		case surfaceN:
			if !p.open {
				return rectOf(popupgeom.Rect{}), true
			}
			return rectOf(w.placeOverlayChild(&p.surface.Widget, p.anchor, true)), true
		case hintN:
			if !p.hint.IsVisible() || p.hintAnchor == nil {
				return rectOf(popupgeom.Rect{}), true
			}
			return rectOf(w.placeOverlayChild(&p.hint.Widget, p.hintAnchor, false)), true
		}
		return rectOf(popupgeom.Rect{}), true
	})

	return p.overlay
}

// rectOf converts a popupgeom.Rect to the *gdk.Rectangle the signal wants.
func rectOf(r popupgeom.Rect) *gdk.Rectangle {
	rect := gdk.NewRectangle(r.X, r.Y, r.W, r.H)
	return &rect
}

// placeOverlayChild computes the rectangle for one overlay child against its
// anchor. matchAnchorWidth is true for the popup surface — a dropdown is never
// narrower than its trigger — and false for the hint.
func (w *Window) placeOverlayChild(child *gtk.Widget, anchor gtk.Widgetter, matchAnchorWidth bool) popupgeom.Rect {
	p := w.popup
	ab := gtk.BaseWidget(anchor)
	ax, ay, ok := ab.TranslateCoordinates(p.overlay, 0, 0) //nolint:staticcheck // TranslateCoordinates is deprecated in GTK4 but avoids graphene import; same call ensureVisible uses
	if !ok {
		// The anchor is not in this widget tree (unrealized, mid-teardown).
		// Nothing sensible to place against; a zero rect keeps this frame
		// harmless and the next allocation retries.
		return popupgeom.Rect{}
	}

	scale := w.backend.Scale()
	gap, margin := int(popupGap*scale), int(popupMargin*scale)
	bounds := popupgeom.Rect{W: p.overlay.Width(), H: p.overlay.Height()}

	minW := 0
	if matchAnchorWidth {
		minW = ab.Width()
	}
	// Width at unconstrained height first, then height at the width the
	// placement will grant — mirroring popupgeom's own clamp, so a wrapping
	// label's height is measured at the width it actually wraps to.
	_, natW, _, _ := child.Measure(gtk.OrientationHorizontal, -1)
	wantW := natW
	if wantW < minW {
		wantW = minW
	}
	if avail := bounds.W - 2*margin; avail > 0 && wantW > avail {
		wantW = avail
	}
	_, natH, _, _ := child.Measure(gtk.OrientationVertical, wantW)

	return popupgeom.Place(popupgeom.Request{
		Anchor: popupgeom.Rect{X: int(ax), Y: int(ay), W: ab.Width(), H: ab.Height()},
		Bounds: bounds,
		NatW:   natW, NatH: natH,
		MinW: minW, MinH: int(popupMinH * scale),
		Gap: gap, Margin: margin,
	}).Rect
}

// openPopup shows body in the popup surface anchored to anchor, suspending the
// current focus list in favour of items. onClose runs when the popup closes,
// on every path — scrim tap, Escape, gamepad B, view switch, drawer hide.
func (w *Window) openPopup(anchor, body gtk.Widgetter, items []focusItem, onClose func()) {
	p := w.popup
	if p == nil {
		return
	}
	w.closePopup() // one popup at a time
	w.hideHint()   // the hint never draws over a dropdown list
	p.anchor = anchor
	p.onClose = onClose
	p.body = body
	p.scroll.SetChild(body)
	p.open = true
	p.scrim.SetVisible(true)
	p.surface.SetVisible(true)
	w.pushFocusList(items)
}

// closePopup dismisses the open popup. Idempotent — every dismissal path
// calls it, and several can fire for one gesture.
func (w *Window) closePopup() {
	p := w.popup
	if p == nil || !p.open {
		return
	}
	p.open = false
	p.scrim.SetVisible(false)
	p.surface.SetVisible(false)
	p.anchor = nil
	onClose := p.onClose
	p.onClose = nil
	w.popFocusList()
	if onClose != nil {
		onClose()
	}
	// Deliver the refresh any suppressed sync still owes the widgets.
	// refreshState does socket I/O, so never inline on the GTK thread.
	if p.stale {
		p.stale = false
		go w.refreshState()
	}
}

// popupOpen reports whether a popup is on screen.
func (w *Window) popupOpen() bool { return w.popup != nil && w.popup.open }

// syncsSuppressed reports whether widget syncs must be skipped because a
// popup is open, recording that a refresh is owed when it closes. Content is
// frozen while a popup is up so a state refresh — the 1s telemetry poll's
// sibling paths, a state-changed event, another client's edit — cannot tear
// the option list, the anchor row, or the suspended focus frame out from
// under the pointer. closePopup delivers the deferred refresh.
func (w *Window) syncsSuppressed() bool {
	if !w.popupOpen() {
		return false
	}
	w.popup.stale = true
	return true
}
