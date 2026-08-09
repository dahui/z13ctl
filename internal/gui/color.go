// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// color.go — color input widget: swatch + preset buttons + custom button.
// The Custom button navigates to the HSL color picker view (stack-based,
// works in both KDE and gamescope modes).

import (
	"fmt"
	"log/slog"

	"github.com/dahui/z13gui/internal/colorconv"
	"github.com/dahui/z13gui/internal/lighting"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// presetColors are the 8 quick-select colors shown as square buttons.
var presetColors = []string{
	"FF0000", // red
	"FF6600", // orange
	"FFFF00", // yellow
	"00FF00", // green
	"00FFFF", // cyan
	"0000FF", // blue
	"FF00FF", // magenta
	"FFFFFF", // white
}

// colorInput holds the current-color swatch, preset buttons, and a Custom
// button that navigates to the HSL color picker view.
type colorInput struct {
	row        *gtk.Box      // entire color input container
	swatch     *gtk.Box      // current-color square (CSS ID driven)
	presetBtns []*gtk.Button // individual preset color buttons
	customBtn  *gtk.Button   // "Custom" button (navigates to color view)
	hex        string        // current RRGGBB uppercase
	label      string        // display label ("COLOR 1" / "COLOR 2")
}

// newColorInput creates a color input widget with swatch, preset buttons,
// and a Custom button that navigates to the HSL color picker view.
func (w *Window) newColorInput(initialHex, swatchName, label string) *colorInput {
	hex, ok := colorconv.Normalize(initialHex)
	if !ok {
		slog.Warn("color input created with an unparseable default", "hex", initialHex)
		hex = lighting.DefaultColor1
	}
	ci := &colorInput{hex: hex, label: label}

	// Current-color swatch (non-interactive colored square).
	ci.swatch = gtk.NewBox(gtk.OrientationHorizontal, 0)
	ci.swatch.SetName(swatchName)
	ci.swatch.AddCSSClass("color-swatch")

	// Preset buttons, each expanding equally to fill the row width.
	presetsRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	for _, hex := range presetColors {
		h := hex
		btn := gtk.NewButton()
		btn.AddCSSClass("color-preset")
		btn.SetHExpand(true)
		p := gtk.NewCSSProvider()
		p.LoadFromString(fmt.Sprintf("button.color-preset { background: #%s; }", h))
		btn.StyleContext().AddProvider(p, gtk.STYLE_PROVIDER_PRIORITY_USER+5) //nolint:staticcheck // per-widget dynamic color
		btn.ConnectClicked(func() {
			ci.hex = h
			w.updateSwatches()
			w.sendApply()
		})
		ci.presetBtns = append(ci.presetBtns, btn)
		presetsRow.Append(btn)
	}

	ci.row = gtk.NewBox(gtk.OrientationVertical, 4)
	ci.row.Append(presetsRow)

	// Custom button navigates to the HSL color picker view.
	ci.customBtn = gtk.NewButton()
	ci.customBtn.SetLabel("Custom")
	ci.customBtn.SetHExpand(true)
	ci.customBtn.ConnectClicked(func() { w.showColorView(ci) })
	controlsRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	controlsRow.Append(ci.swatch)
	controlsRow.Append(ci.customBtn)
	ci.row.Append(controlsRow)

	return ci
}

// updateSwatches refreshes the shared CSS provider so both current-color
// swatches display the latest hex values.
func (w *Window) updateSwatches() {
	if w.swatchProvider == nil {
		return
	}
	c1 := "FF0000"
	if w.color1 != nil {
		c1 = w.color1.hex
	}
	c2 := "000000"
	if w.color2 != nil {
		c2 = w.color2.hex
	}
	w.swatchProvider.LoadFromString(fmt.Sprintf(
		"#color1-swatch { background-color: #%s; }\n#color2-swatch { background-color: #%s; }",
		c1, c2,
	))
}

// showColorView navigates the view stack to the HSL color picker
// and initializes the sliders from the given colorInput's current hex.
func (w *Window) showColorView(ci *colorInput) {
	if w.viewStack == nil {
		return
	}
	if w.colorHue == nil {
		w.viewStack.AddNamed(w.buildColorPickerView(), "color")
		w.buildColorFocusList()
	}
	w.editingColor = ci
	w.colorViewTitle.SetLabel(ci.label)
	// Defensive: hex is normalized on ingest in syncLightingSection, so a failure
	// here means something skipped that path. Leaving the sliders alone beats
	// snapping them to black.
	if h, sat, l, ok := colorconv.HexToHSL(ci.hex); ok {
		// Save and restore rather than assign false: everywhere else that suppresses
		// signals does the same, and a bare assignment here would clear the flag out
		// from under an enclosing sync if this were ever reached from one.
		prev := w.syncing
		w.syncing = true
		w.colorHue.SetValue(h)
		w.colorSat.SetValue(sat)
		w.colorLit.SetValue(l)
		w.syncing = prev
	} else {
		slog.Warn("color picker opened with an unparseable color", "hex", ci.hex)
	}
	w.updateColorPreview()
	w.viewStack.SetVisibleChildName("color")
	w.swapFocusList(w.colorFocusItems)
}

// onHSLChanged reads the current HSL slider values, converts to hex, and
// updates the editing color, swatches, and preview. Called by slider handlers.
func (w *Window) onHSLChanged() {
	if w.syncing || w.editingColor == nil {
		return
	}
	h := w.colorHue.Value()
	s := w.colorSat.Value()
	l := w.colorLit.Value()
	hex := colorconv.HSLToHex(h, s, l)
	w.editingColor.hex = hex
	w.updateSwatches()
	w.updateColorPreview()
	w.queueApply()
}

// colorPickerPresetClicked handles a preset button click in the color picker view.
func (w *Window) colorPickerPresetClicked(hex string) {
	if w.editingColor == nil {
		return
	}
	w.editingColor.hex = hex
	w.updateSwatches()
	w.sendApply()
	// Update HSL sliders to reflect the preset. presetColors are compile-time
	// constants, so a failure here is a programming error, not bad input.
	if h, s, l, ok := colorconv.HexToHSL(hex); ok {
		prev := w.syncing
		w.syncing = true
		w.colorHue.SetValue(h)
		w.colorSat.SetValue(s)
		w.colorLit.SetValue(l)
		w.syncing = prev
	} else {
		slog.Error("preset color is not parseable", "hex", hex)
	}
	w.updateColorPreview()
}

// updateColorPreview updates the color picker view's preview swatch and hex label.
func (w *Window) updateColorPreview() {
	if w.editingColor == nil || w.colorSwatchProv == nil {
		return
	}
	hex := w.editingColor.hex
	w.colorSwatchProv.LoadFromString(fmt.Sprintf(
		"#color-picker-preview { background-color: #%s; }", hex,
	))
	if w.colorHexLabel != nil {
		w.colorHexLabel.SetLabel("#" + hex)
	}
}
