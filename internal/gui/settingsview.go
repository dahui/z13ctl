// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// settingsview.go — the full window's Settings tab: one row per firmware
// toggle the device declares.
//
// Every rule about *which* rows exist and what each may claim lives in
// internal/settingsui, which is pure and tested — the same split dashboardView
// has with internal/telemetryplot. This file builds a label, a switch and a
// send.
//
// # Generic rows, not two named switches
//
// The drawer's bottom bar names its two switches as GTK literals and writes
// their warning prose beside them, which is why a device with a different set
// of toggles could not be described at all. Here the id, the label and the
// prose all come from api.DeviceInfo.Toggles, so this page is already the
// renderer a plugin-contributed toggle needs — nothing about it knows what a
// boot sound is.
//
// The drawer keeps its two switches deliberately. They are quick controls on
// the surface reached in a hurry, and the drawer/window overlap is the
// established shape here: both surfaces show profiles, both show autoswitch,
// and both stay in step because both sync from the same get-state.

import (
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/settingsui"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// settingsView is the firmware-toggle page.
type settingsView struct {
	w    *Window
	host viewHost

	focusItems []focusItem

	root     *gtk.Box
	scroll   *gtk.ScrolledWindow
	backBtn  *gtk.Button
	card     *gtk.Box
	emptyLbl *gtk.Label

	rows []*settingsRow
}

// settingsRow is one toggle: its label and prose, and the switch that writes it.
type settingsRow struct {
	id   string
	sw   *gtk.Switch
	desc *gtk.Label
	box  *gtk.Box
}

// newSettingsView builds the page.
//
// The row set is built once and never rebuilt, unlike the dashboard's charts:
// which toggles exist comes from the capability document, which is static for
// the daemon's lifetime and fetched once before any widget exists. Only the
// values move, and sync moves them on the existing widgets — so a background
// refresh can never tear a switch out from under the pointer.
func newSettingsView(w *Window, host viewHost) *settingsView {
	s := &settingsView{w: w, host: host}

	s.root = gtk.NewBox(gtk.OrientationVertical, 0)

	// The drawer's chrome. The full window has a tab bar naming the page and
	// nothing to go back to, so it asks for neither — as every hosted view does.
	if host.back != nil {
		s.backBtn = gtk.NewButton()
		s.backBtn.SetIconName("go-previous-symbolic")
		s.backBtn.AddCSSClass("view-back-btn")
		s.backBtn.ConnectClicked(host.back)

		header := gtk.NewBox(gtk.OrientationHorizontal, 8)
		header.SetMarginTop(10)
		header.SetMarginBottom(6)
		header.SetMarginStart(14)
		header.Append(s.backBtn)
		title := gtk.NewLabel("Settings")
		title.SetHAlign(gtk.AlignStart)
		title.AddCSSClass("drawer-title")
		header.Append(title)
		s.root.Append(header)
	}

	inner := gtk.NewBox(gtk.OrientationVertical, 0)
	inner.SetMarginStart(14)
	inner.SetMarginEnd(14)
	inner.SetMarginTop(10)
	inner.SetMarginBottom(8)

	s.card = gtk.NewBox(gtk.OrientationVertical, 6)
	s.card.AddCSSClass("section-card")
	heading := gtk.NewLabel("FIRMWARE")
	heading.SetHAlign(gtk.AlignStart)
	heading.AddCSSClass("section-label")
	s.card.Append(heading)
	inner.Append(s.card)

	// Shown in place of the rows, and it says which kind of nothing this is:
	// a daemon that was down when the window opened is fixed by starting it,
	// a device with no toggles is fixed by nothing. Same honesty rule as the
	// dashboard's empty text.
	s.emptyLbl = gtk.NewLabel("")
	s.emptyLbl.AddCSSClass("scale-name")
	s.emptyLbl.SetWrap(true)
	s.emptyLbl.SetXAlign(0)
	s.emptyLbl.SetVisible(false)
	s.card.Append(s.emptyLbl)

	s.buildRows()

	scroll := newDrawerScroll(inner)
	s.root.Append(scroll)
	s.scroll = scroll

	s.buildFocusList()
	return s
}

// buildRows creates one row per declared toggle, or the empty text when there
// are none. Values are filled in by sync; a row starts insensitive because
// nothing has been read yet, which is exactly what Known false means.
func (s *settingsView) buildRows() {
	rows := settingsui.Rows(s.w.device, nil)
	if len(rows) == 0 {
		s.emptyLbl.SetLabel(settingsui.EmptyReason(s.w.device))
		s.emptyLbl.SetVisible(true)
		return
	}
	for _, r := range rows {
		s.card.Append(s.buildRow(r))
	}
}

// buildRow is one label-and-switch line with the device's own prose beneath it.
func (s *settingsView) buildRow(r settingsui.Row) *gtk.Box {
	row := &settingsRow{id: r.ID}

	line := gtk.NewBox(gtk.OrientationHorizontal, 8)
	line.SetMarginTop(4)

	text := gtk.NewBox(gtk.OrientationVertical, 2)
	text.SetHExpand(true)
	// The name leads and the description supports it. Written the other way
	// round first — the name in .scale-name (10px, dim) over a description at
	// the default size — which read as a caption above a heading (Jeff,
	// 2026-08-14).
	label := gtk.NewLabel(r.Label)
	label.SetHAlign(gtk.AlignStart)
	label.AddCSSClass("setting-name")
	text.Append(label)

	// The prose is device data (api.ToggleInfo.Description), not a literal:
	// "may cause ghosting" is a fact about the panel, and a page rendering
	// rows generically has no way to derive it.
	row.desc = gtk.NewLabel(r.Description)
	row.desc.SetHAlign(gtk.AlignStart)
	row.desc.SetXAlign(0)
	row.desc.SetWrap(true)
	row.desc.AddCSSClass("setting-desc")
	row.desc.SetVisible(r.Description != "")
	text.Append(row.desc)
	line.Append(text)

	row.sw = gtk.NewSwitch()
	row.sw.SetVAlign(gtk.AlignCenter)
	// Insensitive until a value has been read — see sync. A switch showing
	// "off" for a toggle nobody could read would be a claim about hardware.
	row.sw.SetSensitive(false)
	if r.Description != "" {
		s.w.setHint(row.sw, r.Description)
	}
	id := r.ID
	row.sw.ConnectStateSet(func(state bool) bool {
		// syncing is what keeps a programmatic SetActive from writing back to
		// the daemon — the drawer's rule for every switch it owns.
		if !s.w.syncing {
			v := 0
			if state {
				v = 1
			}
			s.w.sendFeatureSet(id, v)
		}
		return false
	})
	if s.w.gamescope {
		addTouchActivate(row.sw, func() { row.sw.SetActive(!row.sw.Active()) })
	}
	line.Append(row.sw)

	row.box = line
	s.rows = append(s.rows, row)
	return line
}

// sync moves every switch to the value in the latest get-state.
//
// A toggle absent from State.Features could not be read, so its row goes
// insensitive rather than showing a fabricated "off" — and an insensitive
// widget is one the gamepad grid skips, so a controller cannot land on a
// control it could not operate anyway.
func (s *settingsView) sync() {
	if len(s.rows) == 0 {
		return
	}
	byID := map[string]settingsui.Row{}
	for _, r := range settingsui.Rows(s.w.device, s.w.state) {
		byID[r.ID] = r
	}
	for _, row := range s.rows {
		r, ok := byID[row.id]
		row.sw.SetSensitive(ok && r.Known)
		if ok && r.Known {
			row.sw.SetActive(r.On)
		}
	}
}

// buildFocusList builds the gamepad grid: the back button where there is one,
// then one line per switch.
func (s *settingsView) buildFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	if s.backBtn != nil {
		c := b.Section("nav").One()
		items = append(items, focusItem{
			widget: s.backBtn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: s.host.back,
		})
	}

	b.Section("toggles")
	for _, row := range s.rows {
		c := b.One()
		sw := row.sw
		items = append(items, focusItem{
			widget: sw, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { sw.SetActive(!sw.Active()) },
		})
	}

	items = append(items, s.host.errBar.focusItem())
	s.focusItems = items
	logFocusList(s.host.focusName("settings"), items)
}

// settingsViews returns every settings page that has been built. There is only
// ever the window's today — the drawer keeps its two bottom-bar switches
// instead — but the helper matches customViews so that a second instance is an
// addition here and not a new sync path.
func (w *Window) settingsViews() []*settingsView {
	if w.mainWin != nil && w.mainWin.settings != nil {
		return []*settingsView{w.mainWin.settings}
	}
	return nil
}

// syncSettings re-syncs every settings page that has been built.
func (w *Window) syncSettings() {
	for _, s := range w.settingsViews() {
		s.sync()
	}
}
