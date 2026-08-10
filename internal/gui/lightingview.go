// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// lightingview.go — the RGB section of the main view: zone tabs, effect modes,
// both colour rows, speed and brightness.
//
// It is one control in the registry rather than several, because its parts are
// not independently meaningful: syncModeVis already decides which of them the
// selected effect shows. The rules — which mode a state resolves to, which
// controls each mode has, the defaults — live in internal/lighting, where they
// are tested; this file is the widgets and the daemon calls.
//
// The section is optional. controls.Resolve drops it on a device with no
// lighting capability, so Window.lighting can be nil and every Window-level
// entry point here nil-guards it. That single guard replaced a dozen per-widget
// nil checks.

import (
	"log/slog"
	"strings"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/lighting"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// modeOrder defines the display order for lighting mode buttons.
var modeOrder = []string{
	"static", "breathe", "cycle",
	"rainbow", "strobe", "off",
}

// speeds lists the available lighting animation speeds.
var speeds = []string{"slow", "normal", "fast"}

// lightingView is the RGB block.
type lightingView struct {
	w *Window

	// tab is the zone being edited: "keyboard" or "lightbar". It is also the
	// device name sent to the daemon.
	tab string

	tabKB, tabLB *gtk.CheckButton
	modeButtons  map[string]*gtk.Button
	speedBtns    map[string]*gtk.Button

	color1, color2       *colorInput
	color1Box, color2Box *gtk.Box // label + row; visibility follows the mode
	speedBox             *gtk.Box
	brightBox            *gtk.Box
	brightScale          *gtk.Scale

	// swatchProv drives both current-colour squares. Display-wide, registered
	// once when the section is built — a device without lighting registers
	// nothing.
	swatchProv *gtk.CSSProvider

	// applyTimer debounces continuous inputs (the colour sliders, brightness).
	// Discrete inputs call sendApply directly.
	applyTimer *time.Timer
}

// buildLightingSection appends the whole RGB block: zone tabs, effect modes,
// both colour rows, speed and brightness.
func (w *Window) buildLightingSection(inner *gtk.Box) {
	l := &lightingView{
		w:           w,
		tab:         "keyboard",
		modeButtons: make(map[string]*gtk.Button),
		speedBtns:   make(map[string]*gtk.Button),
	}
	w.lighting = l

	// The swatch provider is separate from the theme so it can be reloaded
	// per-colour at runtime.
	l.swatchProv = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), l.swatchProv, gtk.STYLE_PROVIDER_PRIORITY_USER+10)

	inner.Append(l.buildTabRow())
	inner.Append(l.buildModeSection())

	// Initialize color inputs here so syncModeVis can reference them.
	l.color1 = w.newColorInput("FF0000", "color1-swatch", "COLOR 1")
	l.color2 = w.newColorInput("000000", "color2-swatch", "COLOR 2")
	l.updateSwatches()

	l.color1Box = colorSubBox("COLOR 1", l.color1.row)
	l.color2Box = colorSubBox("COLOR 2", l.color2.row)
	inner.Append(l.color1Box)
	inner.Append(l.color2Box)

	l.speedBox = l.buildSpeedBox()
	inner.Append(l.speedBox)
	l.brightBox = l.buildBrightnessBox()
	inner.Append(l.brightBox)
}

// buildTabRow creates the Keyboard / Lightbar tab radio buttons.
func (l *lightingView) buildTabRow() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)

	kb := gtk.NewCheckButtonWithLabel("Keyboard")
	kb.SetActive(true)
	lb := gtk.NewCheckButtonWithLabel("Lightbar")
	lb.SetGroup(kb)

	kb.AddCSSClass("tab-btn")
	lb.AddCSSClass("tab-btn")
	kb.SetHExpand(true)
	lb.SetHExpand(true)

	kb.ConnectToggled(func() {
		if kb.Active() {
			l.tab = "keyboard"
			l.sync()
		}
	})
	lb.ConnectToggled(func() {
		if lb.Active() {
			l.tab = "lightbar"
			l.sync()
		}
	})

	l.tabKB = kb
	l.tabLB = lb

	if l.w.gamescope {
		addTouchActivate(kb, func() { kb.SetActive(true) })
		addTouchActivate(lb, func() { lb.SetActive(true) })
	}

	row.Append(kb)
	row.Append(lb)
	return row
}

// buildModeSection creates the 3x2 grid of lighting mode buttons.
func (l *lightingView) buildModeSection() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("MODE"))

	grid := gtk.NewGrid()
	grid.SetColumnSpacing(4)
	grid.SetRowSpacing(4)
	grid.AddCSSClass("mode-grid")
	grid.AddCSSClass("btn-group")
	grid.SetColumnHomogeneous(true)

	for i, m := range modeOrder {
		mode := m
		btn := gtk.NewButtonWithLabel(strings.Title(mode)) //nolint:staticcheck // strings.Title is fine for ASCII-only mode/speed/profile labels
		btn.ConnectClicked(func() {
			setActiveButton(l.modeButtons, mode)
			l.syncModeVis()
			l.sendApply()
		})
		l.modeButtons[mode] = btn
		grid.Attach(btn, i%3, i/3, 1, 1)
	}

	box.Append(grid)
	return box
}

// buildSpeedBox creates the slow/normal/fast button row.
func (l *lightingView) buildSpeedBox() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("SPEED"))
	box.Append(l.w.buildButtonGroup(gtk.OrientationHorizontal, speeds, l.speedBtns, func(_ string) {
		l.sendApply()
	}))
	setActiveButton(l.speedBtns, "normal")
	return box
}

// buildBrightnessBox creates the brightness scale (0–3).
func (l *lightingView) buildBrightnessBox() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("BRIGHTNESS"))

	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, 0, 3, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(true)
	sc.SetValue(3)
	sc.SetFocusable(false)
	l.w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() { l.queueApply() })
	l.brightScale = sc
	box.Append(sc)
	return box
}

// syncModeVis shows/hides colour and speed sections based on the active mode.
// Safe to call at any time, including during a sync.
func (l *lightingView) syncModeVis() {
	c := lighting.ControlsFor(activeButton(l.modeButtons, lighting.DefaultMode))
	if l.color1Box != nil {
		l.color1Box.SetVisible(c.Color1)
	}
	if l.color2Box != nil {
		l.color2Box.SetVisible(c.Color2)
	}
	if l.speedBox != nil {
		l.speedBox.SetVisible(c.Speed)
	}
	if l.brightBox != nil {
		l.brightBox.SetVisible(c.Brightness)
	}
}

// sync updates mode, colours, speed and brightness from the daemon state for
// the active device tab.
func (l *lightingView) sync() {
	w := l.w
	prev := w.syncing
	w.syncing = true
	defer func() { w.syncing = prev }()

	ls := lighting.StateForZone(w.state, l.tab)
	setActiveButton(l.modeButtons, lighting.ResolveMode(ls))
	// Normalize on ingest. Daemon state is not guaranteed well-formed — the daemon has
	// had corrupt-state-file bugs — and an unparseable colour used to silently
	// become black in the picker and then be written back to the hardware on the
	// next apply. Keep the previous value instead.
	l.ingestColor(l.color1, ls.Color, "color")
	l.ingestColor(l.color2, ls.Color2, "color2")
	l.updateSwatches()
	setActiveButton(l.speedBtns, lighting.ResolveSpeed(ls))
	if l.brightScale != nil {
		l.brightScale.SetValue(float64(lighting.ResolveBrightness(ls)))
	}
	l.syncModeVis()
}

// ingestColor normalizes one colour from daemon state, keeping the previous
// value when it cannot be parsed.
func (l *lightingView) ingestColor(ci *colorInput, value, field string) {
	if ci == nil {
		return
	}
	if hex, ok := colorconv.Normalize(value); ok {
		ci.hex = hex
		return
	}
	if value != "" {
		slog.Warn("daemon sent an unparseable color, keeping previous",
			"zone", l.tab, "field", field, "value", value)
	}
}

// updateSwatches refreshes the shared CSS provider so both current-color
// swatches display the latest hex values.
func (l *lightingView) updateSwatches() {
	if l.swatchProv == nil {
		return
	}
	c1 := "FF0000"
	if l.color1 != nil {
		c1 = l.color1.hex
	}
	c2 := "000000"
	if l.color2 != nil {
		c2 = l.color2.hex
	}
	l.swatchProv.LoadFromString(
		"#color1-swatch { background-color: #" + c1 + "; }\n" +
			"#color2-swatch { background-color: #" + c2 + "; }")
}

// queueApply debounces rapid API calls from continuous inputs (colour sliders,
// brightness). Discrete inputs — mode buttons, speed buttons, preset clicks —
// call sendApply directly.
func (l *lightingView) queueApply() {
	if l.w.syncing {
		return
	}
	if l.applyTimer != nil {
		l.applyTimer.Stop()
	}
	l.applyTimer = time.AfterFunc(150*time.Millisecond, func() {
		glib.IdleAdd(func() bool {
			l.sendApply()
			return false
		})
	})
}

// sendApply sends the current lighting state to the daemon. Guarded by
// Window.syncing to prevent sending defaults during widget initialization.
func (l *lightingView) sendApply() {
	w := l.w
	if w.syncing {
		return
	}
	color1 := lighting.DefaultColor1
	if l.color1 != nil {
		color1 = l.color1.hex
	}
	color2 := lighting.DefaultColor2
	if l.color2 != nil {
		color2 = l.color2.hex
	}

	mode := activeButton(l.modeButtons, lighting.DefaultMode)
	speed := activeButton(l.speedBtns, lighting.DefaultSpeed)

	brightness := lighting.DefaultBrightness
	if l.brightScale != nil {
		brightness = int(l.brightScale.Value())
	}

	// Widget reads happen above, on the GTK thread; only the socket round-trip
	// runs in the goroutine. api commands carry a 10s deadline, so calling them
	// inline would freeze the drawer for that long against a wedged daemon.
	device := l.tab

	// "off" uses the daemon's dedicated off command so that Enabled=false
	// is persisted and survives a reboot.
	if mode == "off" {
		go func() {
			slog.Debug("sendApply: calling daemon off", "device", device)
			start := time.Now()
			if err := apiresult.Err(api.SendOff(device)); err != nil {
				w.reportError("Turn off "+device+" lighting", err)
				return
			}
			w.clearErrorAsync()
			slog.Debug("sendApply: off done", "elapsed", time.Since(start))
		}()
		return
	}

	go func() {
		slog.Debug("sendApply: calling daemon", "device", device, "mode", mode, "brightness", brightness)
		start := time.Now()
		if err := apiresult.Err(api.SendApply(device, color1, color2, mode, speed, brightness)); err != nil {
			w.reportError("Apply "+device+" lighting", err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendApply: done", "elapsed", time.Since(start))
	}()
}

// focusLightingSection appends the RGB block's focus items.
func (w *Window) focusLightingSection(b *focusgrid.Builder, items *[]focusItem) {
	l := w.lighting
	if l == nil {
		return
	}

	// Device tabs — horizontal row.
	b.Section("tabs")
	tabs := []*gtk.CheckButton{l.tabKB, l.tabLB}
	for i, tc := range b.Line(len(tabs)) {
		btn := tabs[i]
		*items = append(*items, focusItem{
			widget: btn, row: tc.Row, col: tc.Col, section: tc.Section,
			onActivate: func() { btn.SetActive(true) },
		})
	}

	// Mode buttons — a grid three wide. Grid owns how many rows that is, so
	// adding a seventh mode no longer collides with the section below.
	b.Section("mode")
	for i, mc := range b.Grid(len(modeOrder), 3) {
		btn := l.modeButtons[modeOrder[i]]
		*items = append(*items, focusItem{
			widget: btn, row: mc.Row, col: mc.Col, section: mc.Section,
			onActivate: func() { btn.Activate() },
		})
	}

	// Colour presets: a row of swatches, then the custom button below them.
	addColor := func(section string, ci *colorInput, box *gtk.Box) {
		if ci == nil {
			return
		}
		vis := boxVisible(box)
		b.Section(section)
		for i, pc := range b.Line(len(ci.presetBtns)) {
			btn := ci.presetBtns[i]
			*items = append(*items, focusItem{
				widget: btn, row: pc.Row, col: pc.Col, section: pc.Section,
				isVisible:  vis,
				onActivate: func() { btn.Activate() },
			})
		}
		cc := b.One()
		*items = append(*items, focusItem{
			widget: ci.customBtn, row: cc.Row, col: cc.Col, section: cc.Section,
			isVisible:  vis,
			onActivate: func() { w.showColorView(ci) },
		})
	}
	addColor("color1", l.color1, l.color1Box)
	addColor("color2", l.color2, l.color2Box)

	// Speed buttons — horizontal row.
	b.Section("speed")
	for i, sc := range b.Line(len(speeds)) {
		btn := l.speedBtns[speeds[i]]
		*items = append(*items, focusItem{
			widget: btn, row: sc.Row, col: sc.Col, section: sc.Section,
			isVisible:  boxVisible(l.speedBox),
			onActivate: func() { btn.Activate() },
		})
	}

	// Brightness slider.
	left, right, get, set := scaleAdjust(l.brightScale, 1)
	c := b.Section("brightness").One()
	*items = append(*items, focusItem{
		widget: l.brightScale, row: c.Row, col: c.Col, section: c.Section,
		isVisible: boxVisible(l.brightBox),
		editable:  true,
		onLeft:    left, onRight: right,
		getValue: get, setValue: set,
	})
}

// Window-level entry points. Each nil-guards the section, which controls.Resolve
// omits entirely on a device with no lighting capability.

func (w *Window) syncLightingSection() {
	if w.lighting != nil {
		w.lighting.sync()
	}
}

func (w *Window) syncModeVis() {
	if w.lighting != nil {
		w.lighting.syncModeVis()
	}
}

func (w *Window) updateSwatches() {
	if w.lighting != nil {
		w.lighting.updateSwatches()
	}
}

func (w *Window) sendApply() {
	if w.lighting != nil {
		w.lighting.sendApply()
	}
}

func (w *Window) queueApply() {
	if w.lighting != nil {
		w.lighting.queueApply()
	}
}
