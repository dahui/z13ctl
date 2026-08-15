// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// color.go — color input widget: swatch + preset buttons + custom button.
// The Custom button opens the HSL colour picker (colorpopup.go) in the active
// surface's popup layer.

import (
	"fmt"
	"log/slog"

	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/lighting"
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
// button that opens the HSL colour picker.
type colorInput struct {
	// owner is the RGB block this input belongs to. It is what routes a preset
	// click — and, through the colour picker, an HSL slider — back to the *right*
	// section now that two surfaces each build one. Reaching Window.lighting
	// instead applied the drawer's zone to whatever the window was editing.
	owner *lightingView

	row        *gtk.Box      // entire color input container
	swatch     *gtk.Box      // current-color square (CSS ID driven)
	presetBtns []*gtk.Button // individual preset color buttons
	customBtn  *gtk.Button   // "Custom" button (navigates to color view)
	hex        string        // current RRGGBB uppercase
	label      string        // display label ("COLOR 1" / "COLOR 2")
}

// newColorInput creates a color input widget with swatch, preset buttons,
// and a Custom button that opens the HSL colour picker.
func (l *lightingView) newColorInput(initialHex, swatchName, label string) *colorInput {
	hex, ok := colorconv.Normalize(initialHex)
	if !ok {
		slog.Warn("color input created with an unparseable default", "hex", initialHex)
		hex = lighting.DefaultColor1
	}
	ci := &colorInput{owner: l, hex: hex, label: label}

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
			l.updateSwatches()
			l.sendApply()
		})
		ci.presetBtns = append(ci.presetBtns, btn)
		presetsRow.Append(btn)
	}

	// Custom button opens the HSL colour picker over this surface. The ellipsis
	// is the desktop convention for a control that opens something rather than
	// acting; the drawer keeps the bare word, where the row is 320px wide and
	// the ellipsis is clutter at that size.
	ci.customBtn = gtk.NewButton()
	ci.customBtn.SetLabel("Custom")
	ci.customBtn.SetHExpand(true)
	ci.customBtn.ConnectClicked(func() { l.openCustom(ci) })

	if l.cfg.desktop {
		// One line: the eight presets, the current colour, then Custom…. The
		// drawer stacks these because 320px cannot hold them side by side; a
		// window can, and a colour row that wraps to two lines makes the RGB
		// card twice as tall as every other row on the page for no gain.
		ci.customBtn.SetLabel("Custom…")
		ci.customBtn.SetHExpand(false)
		ci.row = gtk.NewBox(gtk.OrientationHorizontal, 8)
		presetsRow.SetHExpand(true)
		ci.row.Append(presetsRow)
		// A wider gap before the current-colour square than between the presets
		// themselves. On one line the swatch is the same size and shape as the
		// eight beside it, so without the gap it reads as a ninth preset — one
		// that does nothing when clicked. The drawer has no such problem: there
		// it sits on the row below.
		ci.swatch.SetMarginStart(8)
		ci.row.Append(ci.swatch)
		ci.row.Append(ci.customBtn)
		return ci
	}

	ci.row = gtk.NewBox(gtk.OrientationVertical, 4)
	ci.row.Append(presetsRow)
	controlsRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	controlsRow.Append(ci.swatch)
	controlsRow.Append(ci.customBtn)
	ci.row.Append(controlsRow)

	return ci
}
