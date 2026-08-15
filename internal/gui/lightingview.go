// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// lightingview.go — the RGB section: zone tabs, effect modes, both colour rows,
// speed and brightness.
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
//
// # Two instances, and what had to stop being Window-level for that
//
// The drawer's main view and the full window's dashboard rail each build one
// (Jeff, 2026-08-14: the dashboard is a general-use surface, not a monitor).
// Three things were written as if there could only ever be one, and each would
// have failed silently rather than loudly:
//
//   - the swatch CSS *ids*. Both current-colour squares are painted by a
//     display-wide provider keyed on "#color1-swatch"/"#color2-swatch", so two
//     instances editing different zones would have fought over one selector and
//     both squares would have shown whichever wrote last. Each instance now
//     carries an id prefix.
//   - Window.updateSwatches/sendApply/queueApply. A colour preset or an HSL
//     slider has to reach *its own* section, and those wrappers reached
//     w.lighting — so the window's picker would have applied the drawer's zone.
//     colorInput carries its owner instead, and the wrappers are gone.
//
// Which picker "Custom" opens was the third of those, carried as an onCustom
// callback while each surface hosted a picker page of its own. It is not a
// question any more: there is one picker for the process (colorpopup.go), drawn
// in whichever surface's popup layer is active, so the section opens it
// directly.

import (
	"fmt"
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

// lightingConfig is everything that differs between the two instances.
type lightingConfig struct {
	// swatchPrefix namespaces this instance's current-colour swatch ids. Empty
	// for the drawer, whose ids are the historical ones.
	swatchPrefix string

	// zone fixes this block to one lighting zone and drops the zone selector
	// with it. Empty is the drawer's shape: one block that re-targets between
	// "keyboard" and "lightbar" through a pair of tab buttons, because 320px has
	// room for one set of controls at a time. The window has room for both, so
	// it builds two blocks and each names its zone in its card heading — a mode
	// switch is a cost you pay to save space, and there is no space to save
	// here (Jeff, 2026-08-14).
	zone string

	// desktop selects the window's form rows over the drawer's stacked blocks:
	// every part on one line with its name, the six effects in one row rather
	// than a 3×2 grid, and each colour's presets, swatch and Custom button side
	// by side instead of stacked.
	desktop bool
}

// lightingView is the RGB block.
type lightingView struct {
	w   *Window
	cfg lightingConfig

	// tab is the zone being edited: "keyboard" or "lightbar". It is also the
	// device name sent to the daemon.
	tab string

	tabKB, tabLB *gtk.CheckButton
	modeButtons  map[string]*gtk.Button
	speedBtns    map[string]*gtk.Button

	// The six blocks, in display order. They are fields rather than only a
	// return value because the two surfaces arrange them differently: the
	// drawer stacks all six in its scrolling column, while the dashboard's card
	// deals them into three columns by what they do.
	zoneRow              *gtk.Box
	modeBox              *gtk.Box
	color1, color2       *colorInput
	color1Box, color2Box *gtk.Box // label + row; visibility follows the mode
	speedBox             *gtk.Box
	brightBox            *gtk.Box
	brightScale          *gtk.Scale
	brightValue          *gtk.Label // window only: the named readout beside the slider

	// swatchProv drives both current-colour squares. Display-wide, registered
	// once when the section is built — a device without lighting registers
	// nothing.
	swatchProv *gtk.CSSProvider

	// applyTimer debounces continuous inputs (the colour sliders, brightness).
	// Discrete inputs call sendApply directly.
	applyTimer *time.Timer
}

// buildLightingSection appends the drawer main view's RGB block to inner and
// registers it as w.lighting, which the control registry's focus half reads.
//
// It appends the blocks straight to the caller's box rather than wrapping them,
// which is what keeps the drawer's spacing exactly what it always was: `inner`
// carries the 8px rhythm every other section is laid out on.
func (w *Window) buildLightingSection(inner *gtk.Box) {
	l := w.newLightingSection(lightingConfig{})
	w.lighting = l
	for _, b := range l.blocks() {
		inner.Append(b)
	}
}

// blocks is the section's widgets in display order, for a caller that wants
// them stacked. The zone row is absent on a zone-fixed block, and is dropped
// from the slice rather than appended as a nil: a nil *gtk.Box in a
// gtk.Widgetter is a non-nil interface, so it would reach GTK and crash.
func (l *lightingView) blocks() []gtk.Widgetter {
	out := make([]gtk.Widgetter, 0, 6)
	if l.zoneRow != nil {
		out = append(out, l.zoneRow)
	}
	return append(out, l.modeBox, l.color1Box, l.color2Box, l.speedBox, l.brightBox)
}

// newLightingSection builds an RGB block. Its widgets are fields on the
// returned view rather than a container, because the caller decides how they
// are arranged — stacked down the drawer's scrolling column, or dealt into the
// dashboard card's three columns.
func (w *Window) newLightingSection(cfg lightingConfig) *lightingView {
	l := &lightingView{
		w:           w,
		cfg:         cfg,
		tab:         "keyboard",
		modeButtons: make(map[string]*gtk.Button),
		speedBtns:   make(map[string]*gtk.Button),
	}
	if cfg.zone != "" {
		l.tab = cfg.zone
	}

	// The swatch provider is separate from the theme so it can be reloaded
	// per-colour at runtime. One per instance: it is registered display-wide, so
	// two instances sharing a selector would repaint each other's squares.
	l.swatchProv = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), l.swatchProv, gtk.STYLE_PROVIDER_PRIORITY_USER+10)

	// No selector on a block that is already one zone: the card heading says
	// which, and a one-of-two control with only one legal answer is furniture.
	if cfg.zone == "" {
		l.zoneRow = l.buildTabRow()
	}
	l.modeBox = l.buildModeSection()

	// Initialize color inputs here so syncModeVis can reference them.
	l.color1 = l.newColorInput("FF0000", l.swatchID(1), "COLOR 1")
	l.color2 = l.newColorInput("000000", l.swatchID(2), "COLOR 2")
	l.updateSwatches()

	if cfg.desktop {
		l.color1Box = formRow("Colour 1", l.color1.row)
		l.color2Box = formRow("Colour 2", l.color2.row)
	} else {
		l.color1Box = colorSubBox("COLOR 1", l.color1.row)
		l.color2Box = colorSubBox("COLOR 2", l.color2.row)
	}
	l.speedBox = l.buildSpeedBox()
	l.brightBox = l.buildBrightnessBox()

	return l
}

// modeColumns is how wide the effect grid is. One row of six in the window,
// where the eye reads a line of options at a glance and a 3×2 block reads as a
// keypad; two rows of three in the 320px drawer, where six across would be
// 50px per thumb.
func (l *lightingView) modeColumns() int {
	if l.cfg.desktop {
		return len(modeOrder)
	}
	return 3
}

// swatchID is this instance's CSS id for the nth current-colour square.
func (l *lightingView) swatchID(n int) string {
	return fmt.Sprintf("%scolor%d-swatch", l.cfg.swatchPrefix, n)
}

// buildTabRow creates the Keyboard / Lightbar zone selector — filled tab
// buttons in the drawer, and in the window the pair of radio buttons GTK
// renders them as once the desktop stylesheet strips the fill, which is the
// right desktop control for one-of-two anyway.
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
	if l.cfg.desktop {
		return formRow("Zone", row)
	}
	return row
}

// buildModeSection creates the 3x2 grid of lighting mode buttons.
func (l *lightingView) buildModeSection() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	if !l.cfg.desktop {
		box.Append(sectionLabel("MODE"))
	}

	grid := gtk.NewGrid()
	grid.SetColumnSpacing(4)
	grid.SetRowSpacing(4)
	grid.AddCSSClass("mode-grid")
	grid.AddCSSClass("btn-group")
	grid.SetColumnHomogeneous(true)

	cols := l.modeColumns()
	for i, m := range modeOrder {
		mode := m
		btn := gtk.NewButtonWithLabel(strings.Title(mode)) //nolint:staticcheck // strings.Title is fine for ASCII-only mode/speed/profile labels
		btn.ConnectClicked(func() {
			setActiveButton(l.modeButtons, mode)
			l.syncModeVis()
			l.sendApply()
		})
		l.modeButtons[mode] = btn
		grid.Attach(btn, i%cols, i/cols, 1, 1)
	}

	if l.cfg.desktop {
		return formRow("Effect", grid)
	}
	box.Append(grid)
	return box
}

// buildSpeedBox creates the slow/normal/fast button row.
func (l *lightingView) buildSpeedBox() *gtk.Box {
	group := l.w.buildButtonGroup(gtk.OrientationHorizontal, speeds, l.speedBtns, func(_ string) {
		l.sendApply()
	})
	setActiveButton(l.speedBtns, "normal")
	if l.cfg.desktop {
		return formRow("Speed", group)
	}
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("SPEED"))
	box.Append(group)
	return box
}

// buildBrightnessBox creates the brightness scale (0–3).
func (l *lightingView) buildBrightnessBox() *gtk.Box {
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, 0, 3, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(!l.cfg.desktop)
	sc.SetValue(3)
	sc.SetFocusable(false)
	l.w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() {
		l.syncBrightnessLabel()
		l.queueApply()
	})
	l.brightScale = sc

	if l.cfg.desktop {
		// Named rather than numbered. 0–3 is what the hardware takes and what
		// the drawer's slider shows, but "2" is not a brightness anyone asked
		// for — and 0 is *off*, which a bare number never says.
		l.brightValue = formValueLabel("")
		l.syncBrightnessLabel()
		return formRow("Brightness", formSlider(sc, l.brightValue))
	}

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("BRIGHTNESS"))
	box.Append(sc)
	return box
}

// brightnessNames are the window's labels for the four hardware levels.
var brightnessNames = []string{"Off", "Low", "Medium", "High"}

// syncBrightnessLabel keeps the named readout on the slider. A no-op in the
// drawer, which has no such label.
func (l *lightingView) syncBrightnessLabel() {
	if l.brightValue == nil || l.brightScale == nil {
		return
	}
	i := int(l.brightScale.Value())
	if i < 0 || i >= len(brightnessNames) {
		return
	}
	l.brightValue.SetLabel(brightnessNames[i])
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
		l.syncBrightnessLabel()
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

// updateSwatches refreshes this instance's CSS provider so both of its
// current-color swatches display the latest hex values.
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
		"#" + l.swatchID(1) + " { background-color: #" + c1 + "; }\n" +
			"#" + l.swatchID(2) + " { background-color: #" + c2 + "; }")
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

// flushApply sends a debounced edit now instead of up to 150ms from now. The
// colour picker calls it as it closes: the popup layer may owe a suppressed
// state refresh, and a refresh that overtakes the pending apply would read
// daemon state from before the last drag and put the previous colour back on
// the swatch. Stop reports whether it actually cancelled a pending send, so
// this cannot double-send one that had already fired.
func (l *lightingView) flushApply() {
	if l.applyTimer != nil && l.applyTimer.Stop() {
		l.applyTimer = nil
		l.sendApply()
	}
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

// focusLightingSection appends the drawer main view's RGB focus items; the
// dashboard rail appends its own instance's in the dashboard's focus list.
func (w *Window) focusLightingSection(b *focusgrid.Builder, items *[]focusItem) {
	if w.lighting != nil {
		w.lighting.appendFocus(b, items)
	}
}

// appendFocus appends this instance's focus items.
func (l *lightingView) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	// Device tabs — horizontal row. Absent on a zone-fixed block.
	if l.tabKB != nil && l.tabLB != nil {
		b.Section(l.section("tabs"))
		tabs := []*gtk.CheckButton{l.tabKB, l.tabLB}
		for i, tc := range b.Line(len(tabs)) {
			btn := tabs[i]
			*items = append(*items, focusItem{
				widget: btn, row: tc.Row, col: tc.Col, section: tc.Section,
				onActivate: func() { btn.SetActive(true) },
			})
		}
	}

	// Mode buttons — a grid three wide. Grid owns how many rows that is, so
	// adding a seventh mode no longer collides with the section below.
	b.Section(l.section("mode"))
	for i, mc := range b.Grid(len(modeOrder), l.modeColumns()) {
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
			onActivate: func() { l.openCustom(ci) },
		})
	}
	addColor(l.section("color1"), l.color1, l.color1Box)
	addColor(l.section("color2"), l.color2, l.color2Box)

	// Speed buttons — horizontal row.
	b.Section(l.section("speed"))
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
	c := b.Section(l.section("brightness")).One()
	*items = append(*items, focusItem{
		widget: l.brightScale, row: c.Row, col: c.Col, section: c.Section,
		isVisible: boxVisible(l.brightBox),
		editable:  true,
		onLeft:    left, onRight: right,
		getValue: get, setValue: set,
	})
}

// section namespaces this instance's focus sections. Sections drive the
// shoulder-button jumps and focusgrid.Sections dedupes them by *name*, so two
// blocks on one page sharing "mode" would collapse into one jump target and the
// second card would be unreachable by bumper — visible to a D-pad, invisible to
// the gesture that exists to skip past a card. The drawer's block is unnamed and
// keeps the historical section names exactly.
func (l *lightingView) section(name string) string {
	if l.cfg.zone == "" {
		return name
	}
	return l.cfg.zone + "-" + name
}

// openCustom opens the HSL picker on this colour input. The picker is one
// popup for the process and the popup layer resolves which surface draws it,
// so the section no longer has to be told which one to reach for.
func (l *lightingView) openCustom(ci *colorInput) {
	l.w.openColorPopup(ci)
}

// Window-level entry points. Each walks every built instance, which is nil-safe
// on a device with no lighting capability — controls.Resolve omits the section
// entirely there, and the dashboard rail asks the same question.

// lightingViews returns every RGB block that has been built: the drawer's one
// re-targetable block, and the dashboard's one per zone when the full window
// exists. They write the same hardware, so anything that syncs one syncs all —
// a stale block showing the previous effect is how a user ends up applying it
// again by touching an unrelated control. Note the drawer's block covers
// whichever zone its tabs are on, so on a two-zone machine one of the three is
// always a second view of a zone another one already shows.
func (w *Window) lightingViews() []*lightingView {
	out := make([]*lightingView, 0, 3)
	if w.lighting != nil {
		out = append(out, w.lighting)
	}
	if m := w.mainWin; m != nil && m.dashboard != nil {
		out = append(out, m.dashboard.lightings...)
	}
	return out
}

func (w *Window) syncLightingSection() {
	for _, l := range w.lightingViews() {
		l.sync()
	}
}

func (w *Window) syncModeVis() {
	for _, l := range w.lightingViews() {
		l.syncModeVis()
	}
}
