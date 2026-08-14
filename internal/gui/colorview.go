// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// colorview.go — the HSL colour picker: a stack view on whichever surface
// opened it.
//
// It is a full view rather than a popup for the standing gamescope reason: it
// is large, and view switching is how the drawer handles anything that would
// otherwise want a window of its own. The colour maths lives in
// internal/colorconv, where it is testable; this file is the widgets.
//
// The view owns its widgets and its focus list. colorInput — the swatch and
// preset row that *opens* this view — stays in color.go, because it is part of
// the lighting section rather than of the picker.
//
// Two instances exist since the dashboard rail grew an RGB card: the drawer's
// and the full window's. It takes a viewHost like every other shared view, and
// the colour it edits reaches the right hardware through colorInput.owner
// rather than through Window — the picker itself knows nothing about which
// section opened it.

import (
	"fmt"
	"log/slog"

	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// colorView is the HSL picker page.
type colorView struct {
	w    *Window
	host viewHost

	root     *gtk.Box
	title    *gtk.Label
	backBtn  *gtk.Button
	presets  []*gtk.Button
	hue      *gtk.Scale
	sat      *gtk.Scale
	lit      *gtk.Scale
	preview  *gtk.Box
	hexLabel *gtk.Label

	// swatchProv styles the preview square. Separate from lightingView's own
	// provider, which drives the two zone swatches: this one is reloaded on
	// every slider tick while the drawer is on the picker.
	swatchProv *gtk.CSSProvider

	// editing is the colorInput this view is editing — nil until opened.
	editing *colorInput

	focusItems []focusItem
}

// newColorView builds the HSL color picker for a surface: preset buttons,
// hue/saturation/lightness sliders, and a preview swatch.
//
// Unlike the drawer's other views it always builds a header. Its back button is
// the only way out of a page the tab bar does not name, so a nil host.back —
// the test every other view reads as "this surface has a tab bar" — would leave
// the full window's picker with no exit at all.
func newColorView(w *Window, host viewHost) *colorView {
	c := &colorView{w: w, host: host}

	view := gtk.NewBox(gtk.OrientationVertical, 8)
	view.SetMarginStart(12)
	view.SetMarginEnd(12)
	c.root = view

	// Header: back button + dynamic title.
	c.title = gtk.NewLabel("COLOR")
	c.backBtn = gtk.NewButton()
	c.backBtn.SetIconName("go-previous-symbolic")
	c.backBtn.AddCSSClass("view-back-btn")
	c.backBtn.ConnectClicked(func() { c.back() })

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.SetMarginTop(10)
	header.SetMarginBottom(6)
	header.Append(c.backBtn)
	c.title.SetHAlign(gtk.AlignStart)
	c.title.AddCSSClass("drawer-title")
	header.Append(c.title)
	view.Append(header)

	// 8 preset buttons.
	presetsRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	for _, hex := range presetColors {
		h := hex
		btn := gtk.NewButton()
		btn.AddCSSClass("color-preset")
		btn.SetHExpand(true)
		p := gtk.NewCSSProvider()
		p.LoadFromString(fmt.Sprintf("button.color-preset { background: #%s; }", h))
		btn.StyleContext().AddProvider(p, gtk.STYLE_PROVIDER_PRIORITY_USER+5) //nolint:staticcheck // per-widget dynamic color
		btn.ConnectClicked(func() { c.presetClicked(h) })
		c.presets = append(c.presets, btn)
		presetsRow.Append(btn)
	}
	view.Append(presetsRow)

	// HSL sliders.
	c.hue = c.newScale(0, 360)
	c.sat = c.newScale(0, 100)
	c.lit = c.newScale(0, 100)

	view.Append(hslScaleBox("HUE", c.hue))
	view.Append(hslScaleBox("SATURATION", c.sat))
	view.Append(hslScaleBox("LIGHTNESS", c.lit))

	// Preview swatch + hex label.
	c.swatchProv = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), c.swatchProv,
		gtk.STYLE_PROVIDER_PRIORITY_USER+10,
	)

	c.preview = gtk.NewBox(gtk.OrientationHorizontal, 0)
	c.preview.AddCSSClass("color-swatch")
	c.preview.SetName("color-picker-preview")

	c.hexLabel = gtk.NewLabel("#FF0000")
	c.hexLabel.AddCSSClass("section-label")

	previewRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	previewRow.SetMarginTop(4)
	previewRow.Append(c.preview)
	previewRow.Append(c.hexLabel)
	view.Append(previewRow)

	c.buildFocusList()
	return c
}

// back leaves the picker. Nil-guarded for the same reason every host.back call
// is: a surface with no way out would be a trap, but a crash is worse.
func (c *colorView) back() {
	if c.host.back != nil {
		c.host.back()
	}
}

// newScale creates a Scale for an HSL component.
func (c *colorView) newScale(lo, hi float64) *gtk.Scale {
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, lo, hi, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(true)
	sc.SetFocusable(false)
	c.w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() { c.onChanged() })
	return sc
}

// hslScaleBox wraps a section label + scale into a box.
func hslScaleBox(label string, sc *gtk.Scale) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 2)
	box.Append(sectionLabel(label))
	box.Append(sc)
	return box
}

// colorPage is the stack child name the picker is installed under, on both
// surfaces.
const colorPage = "color"

// open points the picker at a colour input and initializes the sliders from its
// current hex. Making it visible is the surface's job.
func (c *colorView) open(ci *colorInput) {
	c.editing = ci
	c.title.SetLabel(ci.label)
	// Defensive: hex is normalized on ingest in syncLightingSection, so a failure
	// here means something skipped that path. Leaving the sliders alone beats
	// snapping them to black.
	if h, sat, l, ok := colorconv.HexToHSL(ci.hex); ok {
		c.setSliders(h, sat, l)
	} else {
		slog.Warn("color picker opened with an unparseable color", "hex", ci.hex)
	}
	c.updatePreview()
}

// showColorView navigates the drawer's view stack to the HSL color picker.
func (w *Window) showColorView(ci *colorInput) {
	if w.viewStack == nil {
		return
	}
	w.closePopup()
	if w.colorView == nil {
		w.colorView = newColorView(w, w.drawerHost(colorPage))
		w.viewStack.AddNamed(w.colorView.root, colorPage)
	}
	c := w.colorView
	c.open(ci)
	w.viewStack.SetVisibleChildName(colorPage)
	w.swapFocusList(c.focusItems)
}

// setSliders moves the three scales without emitting an apply.
//
// syncing is saved and restored rather than assigned false: everywhere else
// that suppresses signals does the same, and a bare assignment would clear the
// flag out from under an enclosing sync if this were ever reached from one.
func (c *colorView) setSliders(h, s, l float64) {
	prev := c.w.syncing
	c.w.syncing = true
	c.hue.SetValue(h)
	c.sat.SetValue(s)
	c.lit.SetValue(l)
	c.w.syncing = prev
}

// onChanged reads the current HSL slider values, converts to hex, and updates
// the editing color, swatches, and preview. Called by slider handlers.
func (c *colorView) onChanged() {
	if c.w.syncing || c.editing == nil {
		return
	}
	hex := colorconv.HSLToHex(c.hue.Value(), c.sat.Value(), c.lit.Value())
	c.editing.hex = hex
	c.editing.owner.updateSwatches()
	c.updatePreview()
	c.editing.owner.queueApply()
}

// presetClicked handles a preset button click in the color picker view.
func (c *colorView) presetClicked(hex string) {
	if c.editing == nil {
		return
	}
	c.editing.hex = hex
	c.editing.owner.updateSwatches()
	c.editing.owner.sendApply()
	// Update HSL sliders to reflect the preset. presetColors are compile-time
	// constants, so a failure here is a programming error, not bad input.
	if h, s, l, ok := colorconv.HexToHSL(hex); ok {
		c.setSliders(h, s, l)
	} else {
		slog.Error("preset color is not parseable", "hex", hex)
	}
	c.updatePreview()
}

// updatePreview updates the color picker view's preview swatch and hex label.
func (c *colorView) updatePreview() {
	if c.editing == nil || c.swatchProv == nil {
		return
	}
	hex := c.editing.hex
	c.swatchProv.LoadFromString(fmt.Sprintf(
		"#color-picker-preview { background-color: #%s; }", hex,
	))
	if c.hexLabel != nil {
		c.hexLabel.SetLabel("#" + hex)
	}
}

// buildFocusList builds the 2D focus grid for the HSL color picker view.
func (c *colorView) buildFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	// Back button.
	if c.backBtn != nil {
		coord := b.Section("nav").One()
		items = append(items, focusItem{
			widget: c.backBtn, row: coord.Row, col: coord.Col, section: coord.Section,
			onActivate: func() { c.back() },
		})
	}

	// Colour presets.
	b.Section("presets")
	for i, pc := range b.Line(len(c.presets)) {
		btn := c.presets[i]
		items = append(items, focusItem{
			widget: btn, row: pc.Row, col: pc.Col, section: pc.Section,
			onActivate: func() { btn.Activate() },
		})
	}

	// HSL sliders (editable), one per line.
	b.Section("sliders")
	for _, sc := range []*gtk.Scale{c.hue, c.sat, c.lit} {
		oL, oR, gV, sV := scaleAdjust(sc, 5)
		coord := b.One()
		items = append(items, focusItem{
			widget: sc, row: coord.Row, col: coord.Col, section: coord.Section,
			editable: true,
			onLeft:   oL, onRight: oR,
			getValue: gV, setValue: sV,
		})
	}

	items = append(items, c.host.errBar.focusItem())
	logFocusList(c.host.focusName(colorPage), items)
	c.focusItems = items
}
