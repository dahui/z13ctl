// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// color.go — color input widget: swatch + preset buttons + custom button.
// The Custom button navigates to the HSL color picker view (stack-based,
// works in both KDE and gamescope modes).

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
// button that navigates to the HSL color picker view.
type colorInput struct {
	// owner is the RGB block this input belongs to. It is what routes a preset
	// click — and, through colorView, an HSL slider — back to the *right*
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
// and a Custom button that navigates to the HSL color picker view.
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

	ci.row = gtk.NewBox(gtk.OrientationVertical, 4)
	ci.row.Append(presetsRow)

	// Custom button navigates to the HSL color picker view.
	ci.customBtn = gtk.NewButton()
	ci.customBtn.SetLabel("Custom")
	ci.customBtn.SetHExpand(true)
	ci.customBtn.ConnectClicked(func() { l.openCustom(ci) })
	controlsRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	controlsRow.Append(ci.swatch)
	controlsRow.Append(ci.customBtn)
	ci.row.Append(controlsRow)

	return ci
}
