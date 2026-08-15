// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// colorpopup.go — the HSL colour picker, drawn in the in-surface popup layer
// anchored to the Custom… button that opened it.
//
// It used to be a stack page on each surface: click Custom, the surface
// navigated away to a picker, and you came back through a back button. That is
// the right answer in a 320px column, where a page *is* the drawer's way of
// handling anything that would otherwise want a window of its own. It is the
// wrong answer in a window (Jeff, 2026-08-14): leaving the page you were
// configuring in order to choose a colour takes away the thing you were looking
// at, and the tab highlight has to go blank to stay honest about a sub-page
// that is not a tab. The popup layer already solves this shape for dropdowns —
// an overlay child of the surface's own content, which is why it composites
// under gamescope where a GtkPopover does not — so the picker is one too.
//
// # One picker, not one per surface
//
// openPopup installs the body into whichever layer is active (activePopup), and
// the colour being edited reaches the right zone through colorInput.owner, so a
// second instance would buy nothing — and would reintroduce the display-wide
// CSS id collision the swatch prefixes exist to avoid, since this file paints
// four selectors by id. The body is therefore a Window-level singleton that is
// re-parented between layers, which is what closePopup's scroll detach is for.
//
// # The troughs are the picker
//
// Each slider paints its own trough from the other two values: hue as a ramp
// through the spectrum at the current saturation and lightness, saturation
// grey→colour, lightness black→colour→white. That is the difference between a
// colour picker and three anonymous sliders — you can see where a drag is going
// before you make it — and it costs one CSS provider reload per change, the
// same mechanism the preview swatch already used. Drawing them at the *current*
// values rather than at fixed reference values is deliberate: a slider that
// previews the result is worth more than one that is always colourful, and it
// is what every serious picker does. The handle stays visible when a ramp goes
// black, so nothing is lost at the extremes.

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// CSS ids painted by the picker's own provider. Unique per process because
// there is exactly one picker; the lighting sections' swatches need prefixes
// only because there are two of those.
const (
	previewID = "color-picker-preview"
	hueID     = "color-picker-hue"
	satID     = "color-picker-sat"
	litID     = "color-picker-lit"
)

const (
	// colorPopupWidth is the width the picker asks its layer for. The full
	// window grants it; the drawer clamps it to the panel less the popup
	// margins, which is about what the picker page had there, so nothing is lost
	// on the narrow surface. 380 leaves roughly 240px of hue travel — the number
	// this constant is really about, since the two label columns and the gaps
	// come off it before the sliders get any.
	colorPopupWidth = 380

	// colorPopupNameW / colorPopupValueW are the picker's own label columns.
	// Narrower than formLabelWidth: a form row is sized for a window's card,
	// and this row has to survive being clamped into a 320px panel with a
	// usable slider left over.
	//
	// Both are scaled at construction. They are set with SetSizeRequest, which
	// is in logical pixels, while the type inside them comes from `.scale-name`
	// / `.scale-value` — restated at 10*s in gamescope's scaledCSS. Leaving
	// these unscaled would grow the text by half and hold the column still, so
	// "Saturation" would ellipsize under gamescope and nowhere else. Anything
	// else here that pairs a Go dimension with a CSS one needs the same.
	colorPopupNameW  = 64
	colorPopupValueW = 44

	// colorTroughH is the trough height the ramps are drawn in. GTK's default
	// trough is a few pixels tall, which is enough for a fill and not enough to
	// read a gradient off.
	colorTroughH = 12
)

// colorPopup is the HSL picker's widgets and the colour input it is editing.
type colorPopup struct {
	w *Window

	root     *gtk.Box
	presets  []*gtk.Button
	hue      *gtk.Scale
	sat      *gtk.Scale
	lit      *gtk.Scale
	hueVal   *gtk.Label
	satVal   *gtk.Label
	litVal   *gtk.Label
	preview  *gtk.Box
	hexLabel *gtk.Label

	// prov paints the preview swatch and the three trough ramps. Display-wide,
	// reloaded on every change — the pattern the swatch provider established.
	prov *gtk.CSSProvider

	// editing is the colour input this picker is pointed at; nil until opened.
	editing *colorInput

	focusItems []focusItem
}

// ensureColorPopup builds the picker on first use. Split from openColorPopup so
// the focus dump can bring it into existence without opening it — the picker is
// otherwise reachable only by clicking Custom, which is exactly the gap
// VOLTAIRE_GUI_DUMP_FOCUS exists to close.
func (w *Window) ensureColorPopup() *colorPopup {
	if w.colorPopup == nil {
		w.colorPopup = newColorPopup(w)
	}
	return w.colorPopup
}

// openColorPopup points the picker at a colour input and opens it against that
// input's Custom button, in whichever surface's popup layer is active.
func (w *Window) openColorPopup(ci *colorInput) {
	if ci == nil {
		return
	}
	c := w.ensureColorPopup()
	c.open(ci)
	// The width is asked of the layer rather than requested on the body — see
	// popupLayer.minW for why that distinction is the difference between a
	// picker and a clipped one. The pending debounced apply is flushed on close
	// so the LEDs are not still catching up 150ms after the picker is gone and,
	// more to the point, so the refresh closePopup may owe a suppressed sync
	// cannot read daemon state from before the last drag and put the previous
	// colour back on the swatch.
	w.openPopupSized(ci.customBtn, c.root, c.focusItems,
		int(colorPopupWidth*w.backend.Scale()), func() {
			if ci.owner != nil {
				ci.owner.flushApply()
			}
		})
}

// newColorPopup builds the picker body: presets, three ramped sliders, and the
// result bar.
func newColorPopup(w *Window) *colorPopup {
	c := &colorPopup{w: w}

	c.prov = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), c.prov, gtk.STYLE_PROVIDER_PRIORITY_USER+10)

	root := gtk.NewBox(gtk.OrientationVertical, 8)
	c.root = root

	// The eight presets, as on the colour row this popup opens from. They are
	// duplicated deliberately: the row is behind the scrim, so it is visible but
	// not clickable, and reaching a preset should not mean dismissing the picker
	// first.
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
	root.Append(presetsRow)

	c.hue, c.hueVal = c.newSliderRow(root, "Hue", hueID, 0, 360)
	c.sat, c.satVal = c.newSliderRow(root, "Saturation", satID, 0, 100)
	c.lit, c.litVal = c.newSliderRow(root, "Lightness", litID, 0, 100)

	// Result bar: the colour across the width, its hex right-aligned in the
	// same column as the three slider readouts above it.
	c.preview = gtk.NewBox(gtk.OrientationHorizontal, 0)
	c.preview.AddCSSClass("color-swatch")
	c.preview.SetName(previewID)
	c.preview.SetHExpand(true)

	c.hexLabel = gtk.NewLabel("#FF0000")
	c.hexLabel.AddCSSClass("scale-value")
	c.hexLabel.SetXAlign(1)
	c.hexLabel.SetSizeRequest(int(colorPopupValueW*w.backend.Scale()), -1)

	resultRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	resultRow.Append(c.preview)
	resultRow.Append(c.hexLabel)
	root.Append(resultRow)

	c.buildFocusList()
	return c
}

// newSliderRow appends one labelled slider with its readout and returns both.
func (c *colorPopup) newSliderRow(parent *gtk.Box, name, id string, lo, hi float64) (*gtk.Scale, *gtk.Label) {
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, lo, hi, 1)
	sc.SetName(id)
	sc.SetDigits(0)
	// The value is in the readout beside the slider, not floating over the
	// handle: GtkScale's own value travels with the thumb, and the number you
	// want while dragging is the one that holds still.
	sc.SetDrawValue(false)
	sc.SetFocusable(false)
	sc.SetHExpand(true)
	// Deliberately not wheelScrollsView. That guard exists because a wheel
	// flick over a slider in a scrolling view silently changed a hardware
	// setting; the popup does not scroll, so here the wheel has nothing to do
	// but adjust the value — which is what the picker page did before it.
	sc.ConnectValueChanged(func() { c.onChanged() })

	scale := c.w.backend.Scale()

	lbl := gtk.NewLabel(name)
	lbl.AddCSSClass("scale-name")
	lbl.SetXAlign(0)
	lbl.SetVAlign(gtk.AlignCenter)
	lbl.SetSizeRequest(int(colorPopupNameW*scale), -1)

	val := gtk.NewLabel("")
	val.AddCSSClass("scale-value")
	val.SetXAlign(1)
	val.SetVAlign(gtk.AlignCenter)
	val.SetSizeRequest(int(colorPopupValueW*scale), -1)

	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.Append(lbl)
	row.Append(sc)
	row.Append(val)
	parent.Append(row)
	return sc, val
}

// open points the picker at a colour input and initializes it from that
// input's current hex.
func (c *colorPopup) open(ci *colorInput) {
	c.editing = ci
	// Defensive: hex is normalized on ingest in lightingView.sync, so a failure
	// here means something skipped that path. Leaving the sliders where they are
	// beats snapping them to black.
	if h, s, l, ok := colorconv.HexToHSL(ci.hex); ok {
		c.setSliders(h, s, l)
	} else {
		slog.Warn("color picker opened with an unparseable color", "hex", ci.hex)
	}
	c.repaint()
}

// setSliders moves the three scales without emitting an apply.
//
// syncing is saved and restored rather than assigned false: everywhere else
// that suppresses signals does the same, and a bare assignment would clear the
// flag out from under an enclosing sync if this were ever reached from one.
func (c *colorPopup) setSliders(h, s, l float64) {
	prev := c.w.syncing
	c.w.syncing = true
	c.hue.SetValue(h)
	c.sat.SetValue(s)
	c.lit.SetValue(l)
	c.w.syncing = prev
}

// onChanged converts the slider values to hex, applies them to the colour input
// and repaints. Called by all three sliders.
func (c *colorPopup) onChanged() {
	if c.w.syncing || c.editing == nil {
		return
	}
	c.editing.hex = colorconv.HSLToHex(c.hue.Value(), c.sat.Value(), c.lit.Value())
	c.editing.owner.updateSwatches()
	c.repaint()
	c.editing.owner.queueApply()
}

// presetClicked adopts a preset colour and moves the sliders to match.
func (c *colorPopup) presetClicked(hex string) {
	if c.editing == nil {
		return
	}
	c.editing.hex = hex
	c.editing.owner.updateSwatches()
	c.editing.owner.sendApply()
	// presetColors are compile-time constants, so a failure here is a
	// programming error rather than bad input.
	if h, s, l, ok := colorconv.HexToHSL(hex); ok {
		c.setSliders(h, s, l)
	} else {
		slog.Error("preset color is not parseable", "hex", hex)
	}
	c.repaint()
}

// repaint refreshes the readouts, the result bar and the three trough ramps
// from the current slider values.
func (c *colorPopup) repaint() {
	if c.editing == nil || c.prov == nil {
		return
	}
	h, s, l := c.hue.Value(), c.sat.Value(), c.lit.Value()

	c.hueVal.SetLabel(fmt.Sprintf("%.0f°", h))
	c.satVal.SetLabel(fmt.Sprintf("%.0f%%", s))
	c.litVal.SetLabel(fmt.Sprintf("%.0f%%", l))
	c.hexLabel.SetLabel("#" + c.editing.hex)

	// The hue ramp is sampled every 60° and closes on the same colour it opens
	// with, so the two ends of the slider meet rather than stepping.
	hueStops := make([]string, 0, 7)
	for i := 0; i <= 6; i++ {
		hueStops = append(hueStops, "#"+colorconv.HSLToHex(float64(i)*60, s, l))
	}
	// Lightness gets a midpoint so the pure colour sits where it belongs
	// instead of being interpolated between black and white.
	litStops := []string{
		"#" + colorconv.HSLToHex(h, s, 0),
		"#" + colorconv.HSLToHex(h, s, 50),
		"#" + colorconv.HSLToHex(h, s, 100),
	}
	satStops := []string{
		"#" + colorconv.HSLToHex(h, 0, l),
		"#" + colorconv.HSLToHex(h, 100, l),
	}

	var b strings.Builder
	fmt.Fprintf(&b, "#%s { background-color: #%s; }\n", previewID, c.editing.hex)
	troughH := int(colorTroughH * c.w.backend.Scale())
	for _, r := range []struct {
		id    string
		stops []string
	}{
		{hueID, hueStops}, {satID, satStops}, {litID, litStops},
	} {
		// The theme paints the trough with a flat `background` and the filled
		// portion with `highlight`. This provider loads above it, so the ramp
		// replaces the former and the latter is cleared — a solid accent bar
		// growing from the left would hide the half of the ramp behind it.
		// border: none because the GTK theme's own trough border survives the
		// voltaire sheet — which sets `background` and nothing else — and is
		// invisible only at GTK's default trough height. At the height a ramp
		// needs to be readable it draws a 1px frame in the *desktop* theme's
		// accent, which is not a colour this drawer ever chose.
		fmt.Fprintf(&b, "#%s trough { background-image: linear-gradient(to right, %s); min-height: %dpx; border: none; }\n",
			r.id, strings.Join(r.stops, ", "), troughH)
		fmt.Fprintf(&b, "#%s trough highlight { background-image: none; background-color: transparent; }\n", r.id)
	}
	c.prov.LoadFromString(b.String())
}

// buildFocusList builds the picker's gamepad grid: the preset row, then the
// three sliders one per line. It carries no back item and no error-bar item —
// a popup is left with B or the scrim, and its focus list is suspended over the
// surface's own, which already has the bar.
func (c *colorPopup) buildFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	b.Section("presets")
	for i, pc := range b.Line(len(c.presets)) {
		btn := c.presets[i]
		items = append(items, focusItem{
			widget: btn, row: pc.Row, col: pc.Col, section: pc.Section,
			onActivate: func() { btn.Activate() },
		})
	}

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

	logFocusList("color", items)
	c.focusItems = items
}
