// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// settingsview.go — the full window's Settings tab: voltaire's own preferences,
// then one row per firmware toggle the device declares.
//
// Every rule about *which* toggle rows exist and what each may claim lives in
// internal/settingsui, which is pure and tested — the same split dashboardView
// has with internal/telemetryplot. This file builds a label, a switch and a
// send.
//
// # Two cards, and only one of them is a renderer
//
// VOLTAIRE holds preferences that belong to this application: they are ours,
// there is a finite known set of them, and each is written by name. FIRMWARE
// holds the device's toggles, which are *data* — id, label and prose all come
// from the capability document, and nothing in that half knows what a boot
// sound is. The distinction matters because the second card's whole design is
// that it cannot be written by hand; putting an app preference into it would
// have been the first hardcoded row in a renderer built to have none.
// Their text still lives outside this file (internal/buttonpref) on the same
// grounds api.ToggleInfo.Description is device data: the page that draws a
// setting is not the place its behaviour is described.
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
	"github.com/dahui/voltaire/v2/internal/buttonpref"
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

	// The button-preference row: one button per surface, and the summary line
	// beneath them, which is rewritten on every choice because it names the
	// selection.
	pressBtns map[buttonpref.Surface]*gtk.Button
	pressDesc *gtk.Label
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

	inner.Append(s.buildAppCard())

	s.card = gtk.NewBox(gtk.OrientationVertical, 6)
	s.card.AddCSSClass("section-card")
	s.card.SetMarginTop(12)
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

// buildAppCard builds voltaire's own preferences — today, which surface the
// hardware button opens.
//
// The control is a pair of buttons rather than a switch: a switch would have to
// be labelled for one of the two outcomes ("single press opens the full
// window"), so the *other* arrangement would only exist as its negation, and
// the off position would describe nothing. Two named buttons say what both
// choices are. It is also not a dropdown — see popup.go for why the drawer has
// none, and two options do not earn a list that has to be opened.
func (s *settingsView) buildAppCard() *gtk.Box {
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("section-card")
	heading := gtk.NewLabel("VOLTAIRE")
	heading.SetHAlign(gtk.AlignStart)
	heading.AddCSSClass("section-label")
	card.Append(heading)

	line := gtk.NewBox(gtk.OrientationHorizontal, 8)
	line.SetMarginTop(4)

	text := gtk.NewBox(gtk.OrientationVertical, 2)
	text.SetHExpand(true)
	label := gtk.NewLabel(buttonpref.RowLabel)
	label.SetHAlign(gtk.AlignStart)
	label.AddCSSClass("setting-name")
	text.Append(label)

	// Two lines of prose: what the setting is for, then what the current
	// choice actually does. The second is the one that changes, and it is the
	// one that tells the user about the double press at all.
	desc := gtk.NewLabel(buttonpref.RowDescription)
	desc.SetHAlign(gtk.AlignStart)
	desc.SetXAlign(0)
	desc.SetWrap(true)
	desc.AddCSSClass("setting-desc")
	text.Append(desc)

	s.pressDesc = gtk.NewLabel("")
	s.pressDesc.SetHAlign(gtk.AlignStart)
	s.pressDesc.SetXAlign(0)
	s.pressDesc.SetWrap(true)
	s.pressDesc.AddCSSClass("setting-desc")
	text.Append(s.pressDesc)
	line.Append(text)

	group := gtk.NewBox(gtk.OrientationHorizontal, 4)
	group.AddCSSClass("btn-group")
	group.SetVAlign(gtk.AlignCenter)
	s.pressBtns = map[buttonpref.Surface]*gtk.Button{}
	for _, opt := range buttonpref.Options() {
		opt := opt
		btn := gtk.NewButtonWithLabel(opt.Label())
		btn.ConnectClicked(func() { s.selectPress(opt) })
		s.pressBtns[opt] = btn
		group.Append(btn)
	}
	line.Append(group)
	card.Append(line)

	s.syncPress()
	return card
}

// selectPress applies a choice. It takes effect immediately — the dispatcher
// reads the preference at press time — so there is nothing to restart and
// nothing to save separately.
func (s *settingsView) selectPress(opt buttonpref.Surface) {
	s.w.setButtonPress(opt)
	s.syncPress()
}

// syncPress moves the highlight and rewrites the summary. It is not driven by
// daemon state: this preference is the client's, so unlike every other row on
// this page it has no get-state to be told about it.
func (s *settingsView) syncPress() {
	for opt, btn := range s.pressBtns {
		if opt == s.w.press {
			btn.AddCSSClass("active")
		} else {
			btn.RemoveCSSClass("active")
		}
	}
	if s.pressDesc != nil {
		s.pressDesc.SetLabel(buttonpref.Summary(s.w.press))
	}
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

	// One Line, not a One() each: the two buttons sit side by side, so D-pad
	// right has to reach the second. A coordinate that disagrees with what is on
	// screen is the failure nobody notices with a mouse in their hand — the
	// profile row on the dashboard was written the wrong way round first.
	opts := buttonpref.Options()
	b.Section("voltaire")
	for i, c := range b.Line(len(opts)) {
		btn := s.pressBtns[opts[i]]
		if btn == nil {
			continue
		}
		items = append(items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.Activate() },
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
