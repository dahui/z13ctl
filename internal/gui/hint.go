// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// hint.go — anchored help text, replacing GTK tooltips throughout the drawer.
//
// GtkTooltipWindow is a GtkNative with its own GdkSurface, so under gamescope
// every tooltip is invisible — and the drawer's touch and controller users
// have no hover to raise one anyway. The hint is instead the popup layer's
// third overlay child (popup.go): a can-target=false label placed against the
// hinted widget by popupgeom, shown after a pointer dwell or on gamepad
// focus, identical on all three backends. What matters as much as the
// gamescope win is that KDE and gamescope no longer diverge — one help path,
// exercised everywhere.
//
// Hints carry descriptions of usable controls. Refusal reasons for
// desensitized controls do NOT go here — the focus grid skips insensitive
// widgets, so a focus-triggered hint can never fire on them; those live in
// .block-note labels (see blockNote).
//
// Entries in w.hints are never removed. Every hinted widget is built once and
// lives for the process lifetime; the popup layer's transient option buttons
// deliberately carry no hints. If a transient widget were ever hinted, its
// map entry would outlive it and a recycled pointer could show the wrong
// text — keep hints on permanent widgets only.

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// hintDwellMS is the hover delay before a hint appears, matching the feel of
// the GTK tooltips it replaces.
const hintDwellMS = 500

// setHint registers help text for widget and arms the pointer dwell that
// shows it. Call once per widget, at build time.
func (w *Window) setHint(widget gtk.Widgetter, text string) {
	if w.hints == nil {
		w.hints = make(map[uintptr]string)
	}
	w.hints[coreglib.BaseObject(widget).Native()] = text

	motion := gtk.NewEventControllerMotion()
	motion.ConnectEnter(func(_, _ float64) {
		// The generation counter is the established cancel pattern here: a
		// leave (or another enter) bumps it, and a fired timeout that finds
		// itself stale does nothing.
		w.hintGen++
		gen := w.hintGen
		glib.TimeoutAdd(hintDwellMS, func() bool {
			if gen == w.hintGen {
				w.showHint(widget)
			}
			return false
		})
	})
	motion.ConnectLeave(func() {
		w.hintGen++
		w.hideHint()
	})
	base := gtk.BaseWidget(widget)
	base.AddController(motion)

	// A widget desensitized (or hidden) while the pointer is inside it stops
	// receiving events, so its own Leave never arrives and the hint hangs over
	// the view until the pointer happens to enter another hinted widget. Found
	// on hardware: clicking Activate greys it out, and the stranded hint then
	// sat directly over the .block-note explaining why — the one moment that
	// note matters. Drop the hint when its anchor stops being hoverable.
	drop := func() {
		if base.IsSensitive() && base.IsVisible() {
			return
		}
		for _, p := range w.popupLayers() {
			if p.hintAnchor != nil &&
				coreglib.BaseObject(p.hintAnchor).Native() == coreglib.BaseObject(widget).Native() {
				w.hintGen++ // also cancels a dwell timer still in flight
				w.hideHint()
				return
			}
		}
	}
	base.Connect("notify::sensitive", drop)
	base.Connect("notify::visible", drop)
}

// showHint places the hint label against anchor with its registered text.
// No-op while a popup is open — the hint never draws over a dropdown list —
// and a widget with no registered text hides any hint showing.
//
// An unusable anchor shows nothing: a hint describes what a control does, and
// for a control the daemon would refuse, the .block-note beneath it carries
// the reason instead. (The gamepad path cannot reach one anyway —
// focusItem.visible skips insensitive widgets.)
func (w *Window) showHint(anchor gtk.Widgetter) {
	p := w.activePopup()
	if p == nil || p.open {
		return
	}
	if b := gtk.BaseWidget(anchor); !b.IsSensitive() || !b.IsVisible() {
		w.hideHint()
		return
	}
	text := w.hints[coreglib.BaseObject(anchor).Native()]
	if text == "" {
		w.hideHint()
		return
	}
	p.hintAnchor = anchor
	p.hint.SetText(text)
	p.hint.SetVisible(true)
}

// hideHint hides the hint label on every surface. Idempotent — a hint shown
// on the layer that was active at dwell time must not survive a surface
// switch, so this sweeps rather than asking which layer is active now.
func (w *Window) hideHint() {
	for _, p := range w.popupLayers() {
		p.hintAnchor = nil
		p.hint.SetVisible(false)
	}
}

// pruneHintAt hides an anchored hint once the pointer is outside its anchor.
// x and y are relative to the layer's overlay, whose own motion controller
// calls this — coordinates and anchor translation then share one widget tree
// on every surface, where the old gtkWin-relative wiring could only ever
// serve the drawer.
//
// The anchor's own Leave is the normal path; this is the backstop for the
// cases where it never arrives, both of which were found on hardware: a widget
// desensitized under a stationary pointer (Activate, the moment it is
// clicked), and a crossing swallowed while the popup scrim was covering the
// anchor. A stranded hint is not merely untidy — it sits over the .block-note
// that explains the control it is anchored to.
func (w *Window) pruneHintAt(p *popupLayer, x, y float64) {
	if p == nil || p.hintAnchor == nil || !p.hint.IsVisible() {
		return
	}
	b := gtk.BaseWidget(p.hintAnchor)
	ax, ay, ok := b.TranslateCoordinates(p.overlay, 0, 0) //nolint:staticcheck // deprecated in GTK4 but avoids a graphene import; as ensureVisible does
	if !ok || x < ax || y < ay || x > ax+float64(b.Width()) || y > ay+float64(b.Height()) {
		w.hintGen++ // also cancels a dwell timer still in flight
		w.hideHint()
	}
}
