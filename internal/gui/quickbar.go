// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// quickbar.go — what the drawer's main view shows, and the Settings card that
// changes it.
//
// Which sections exist, in what order, and how the editor's rows move are
// internal/controls' rules (pure, tested); this file builds widgets from them.
// The drawer rebuilds its sections in place when the layout changes, so a
// choice in Settings shows up the next time the drawer opens, with no restart.

import (
	"log/slog"

	"github.com/dahui/voltaire/v2/internal/controls"
	"github.com/dahui/voltaire/v2/internal/display"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/theme"
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// sessionCaps is the capabilities only this client can know: a refresh rate
// is the compositor's, so whether one can be read here is not in the device
// document. Checked once — display.Available is a PATH lookup.
func (w *Window) sessionCaps() []controls.Capability {
	if w.hasDisplay {
		return []controls.Capability{controls.CapRefreshRate}
	}
	return nil
}

// layoutConfig is the drawer's effective layout source: gui.toml's controls
// list when it has one (locked reports that), else the list the Settings
// editor saved to config.toml, else the defaults.
func (w *Window) layoutConfig() (cfg controls.Config, locked bool) {
	return controls.Effective(w.guiFile, theme.LoadAppConfig().QuickbarControls)
}

// appendSections builds the resolved sections into the drawer's scrolling box.
//
// The sections, their group headings and the separators between them all come
// from the resolved control list rather than from a literal sequence of
// Appends. With no saved layout this produces exactly the sequence that used
// to be written out in buildContent — controls.TestLayoutReproducesTheShippedChrome
// pins that — and a user who reorders or hides a section gets the headings
// following their choice instead of stranded above the wrong content.
//
// Every hint registered while building is recorded, so a rebuild can drop the
// entries for widgets it is about to discard (see rebuildDrawer).
func (w *Window) appendSections(inner *gtk.Box) {
	w.collectHints = true
	defer func() { w.collectHints = false }()

	builders := w.controlBuilders()
	for _, row := range controls.Layout(w.controls) {
		if row.Separator {
			inner.Append(separator())
		}
		if row.Heading != "" {
			inner.Append(groupLabel(row.Heading))
		}
		cb, ok := builders[row.Control.ID]
		if !ok {
			// The registry names a control this build has no builder for. It
			// cannot happen from a config file (Resolve drops unknown IDs), so
			// it means the two lists have drifted — log rather than panic, and
			// leave the rest of the drawer usable.
			slog.Warn("no builder for control; skipping", "id", row.Control.ID)
			continue
		}
		cb.build(inner)
	}

	// Set initial visibility based on default mode (static). Safe when the
	// lighting section was not built: every widget it touches is nil-guarded.
	w.syncModeVis()
}

// rebuildDrawer re-resolves the layout and rebuilds the drawer's sections in
// place: the title row, the bottom bar and every other view are untouched.
//
// The drawer's section instances are dropped first. Every Window-level walker
// over them is nil-safe already — controls.Resolve has always been able to
// drop a section on a device without the capability — and the window's copies
// are gated on the document, not on these fields, so nothing outside the
// drawer notices. The rebuilt sections are synced from the state already in
// hand rather than a new fetch.
func (w *Window) rebuildDrawer() {
	if w.drawerInner == nil {
		return
	}
	w.closePopup()

	for _, h := range w.drawerHints {
		delete(w.hints, h)
	}
	w.drawerHints = nil
	for child := w.drawerInner.FirstChild(); child != nil; child = w.drawerInner.FirstChild() {
		w.drawerInner.Remove(child)
	}
	w.profiles, w.autoswitch, w.battery, w.lighting = nil, nil, nil, nil
	w.boostSw, w.display = nil, nil

	cfg, _ := w.layoutConfig()
	w.controls = resolveControls(cfg, w.device, w.sessionCaps()...)
	w.appendSections(w.drawerInner)

	w.syncState()
	w.syncing = true
	w.syncCPUBoost()
	w.syncing = false
	if w.display != nil && w.visible.Load() {
		w.display.sync()
	}

	w.buildMainFocusList()
	if w.viewStack != nil && w.viewStack.VisibleChildName() == "main" {
		w.swapFocusList(w.mainFocusItems)
	}
}

// recordHint notes a hint registered while the drawer's sections are being
// built, so rebuildDrawer can forget it with the widget. Called by setHint.
func (w *Window) recordHint(widget gtk.Widgetter) {
	if w.collectHints {
		w.drawerHints = append(w.drawerHints, coreglib.BaseObject(widget).Native())
	}
}

// --- the opt-in drawer sections ------------------------------------------

// buildBoostSection is the drawer's CPU boost: the heading and the switch on
// one line, the drawer autoswitch block's shape. The window's is a form row on
// the Dashboard (dashboard.go); syncCPUBoost moves both.
func (w *Window) buildBoostSection() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.Append(sectionLabel("CPU BOOST"))

	sw := gtk.NewSwitch()
	sw.SetHAlign(gtk.AlignEnd)
	sw.SetHExpand(true)
	// Insensitive until a value has been read: State.CPUBoost is absent rather
	// than false when the daemon could not read it, and a switch at "off"
	// would be a claim about the CPU.
	sw.SetSensitive(false)
	w.setHint(sw, "Allow boost clocks above the base frequency")
	sw.ConnectStateSet(func(on bool) bool {
		if !w.syncing {
			w.sendCPUBoostSet(on)
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(sw, func() { sw.SetActive(!sw.Active()) })
	}
	w.boostSw = sw
	row.Append(sw)
	return row
}

func (w *Window) focusBoostSection(b *focusgrid.Builder, items *[]focusItem) {
	sw := w.boostSw
	if sw == nil {
		return
	}
	c := b.Section("boost").One()
	*items = append(*items, focusItem{
		widget: sw, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { sw.SetActive(!sw.Active()) },
	})
}

// syncBoostSwitch moves a boost switch to the daemon's reading; absent is
// insensitive, never off.
func (w *Window) syncBoostSwitch(sw *gtk.Switch) {
	if sw == nil {
		return
	}
	st := w.state
	if st == nil || st.CPUBoost == nil {
		sw.SetSensitive(false)
		return
	}
	sw.SetSensitive(true)
	sw.SetActive(*st.CPUBoost)
}

// buildDisplaySection is the drawer's refresh rate: the live control alone
// (newDisplaySection's compact shape). Re-read on every show, since it shells
// out and the rate changes only when someone changes it.
func (w *Window) buildDisplaySection(inner *gtk.Box) {
	d, box := w.newDisplaySection(true)
	if d == nil {
		return
	}
	w.display = d
	inner.Append(box)
	d.sync()
}

func (w *Window) focusDisplaySection(b *focusgrid.Builder, items *[]focusItem) {
	w.display.appendFocus(b, items) // nil-safe
}

// hasDisplayBackend is checked once at startup.
func hasDisplayBackend() bool { return display.Available() }

// --- the Settings card ----------------------------------------------------

// quickbarRow is one control's line in the QUICKBAR card. The widgets are
// built once and reordered in place, so a move never tears a button out from
// under the pointer or the gamepad focus.
type quickbarRow struct {
	box      *gtk.Box
	up, down *gtk.Button
	sw       *gtk.Switch
}

// buildQuickbarCard builds the QUICKBAR card: one row per control this machine
// supports — a show switch and ▲/▼ — in drawer order, and a reset.
//
// ▲/▼ rather than drag or grab-and-move (Jeff, 2026-10-09): they are plain
// buttons in the focus grid, so mouse, touch and a controller all do the same
// thing and there is no "holding a row" state to learn. A change is saved and
// the drawer rebuilt at once; there is no separate save.
func (s *settingsView) buildQuickbarCard() *gtk.Box {
	w := s.w
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("section-card")
	card.SetMarginTop(12)
	heading := gtk.NewLabel("QUICKBAR")
	heading.SetHAlign(gtk.AlignStart)
	heading.AddCSSClass("section-label")
	card.Append(heading)

	desc := gtk.NewLabel("What the quickbar shows, and in what order.")
	desc.SetHAlign(gtk.AlignStart)
	desc.SetXAlign(0)
	desc.SetWrap(true)
	desc.AddCSSClass("setting-desc")
	card.Append(desc)

	// gui.toml is hand-edited, and a person's file is never overwritten from
	// here: the card shows the layout it describes and says where to change it.
	cfg, locked := w.layoutConfig()
	s.qbLocked = locked
	s.qbNote = blockNote()
	card.Append(s.qbNote)
	if locked {
		setBlockNote(s.qbNote, "Set by the controls list in ~/.config/voltaire/"+controls.ConfigFile+
			". Remove that list to arrange the quickbar here.")
	}

	s.qbList = gtk.NewBox(gtk.OrientationVertical, 4)
	card.Append(s.qbList)
	s.qbRows = controls.EditorRows(cfg.Quickbar.Controls, w.device, w.sessionCaps()...)
	s.qbWidgets = map[string]*quickbarRow{}
	for _, r := range s.qbRows {
		s.qbWidgets[r.Control.ID] = s.buildQuickbarRow(r.Control)
		s.qbList.Append(s.qbWidgets[r.Control.ID].box)
	}

	s.qbReset = gtk.NewButtonWithLabel("Reset to default")
	s.qbReset.SetHAlign(gtk.AlignEnd)
	s.qbReset.ConnectClicked(func() {
		s.applyQuickbar(controls.EditorRows(nil, w.device, w.sessionCaps()...), s.qbReset)
	})
	resetRow := gtk.NewBox(gtk.OrientationHorizontal, 0)
	resetRow.AddCSSClass("btn-group")
	resetRow.SetHAlign(gtk.AlignEnd)
	// Set apart from the rows: it acts on the whole list, not on the last row
	// it would otherwise sit against (Jeff, 2026-10-09).
	resetRow.SetMarginTop(14)
	resetRow.Append(s.qbReset)
	card.Append(resetRow)

	s.renderQuickbar()
	return card
}

// buildQuickbarRow builds one control's line: its name, ▲, ▼ and the switch.
func (s *settingsView) buildQuickbarRow(c controls.Control) *quickbarRow {
	w := s.w
	q := &quickbarRow{box: gtk.NewBox(gtk.OrientationHorizontal, 8)}

	name := gtk.NewLabel(c.Label)
	name.SetHAlign(gtk.AlignStart)
	name.SetHExpand(true)
	name.AddCSSClass("setting-name")
	q.box.Append(name)

	arrows := gtk.NewBox(gtk.OrientationHorizontal, 4)
	arrows.AddCSSClass("btn-group")
	arrows.SetVAlign(gtk.AlignCenter)
	id := c.ID
	q.up = gtk.NewButtonWithLabel("▲")
	q.up.ConnectClicked(func() { s.moveQuickbar(id, -1, q.up) })
	w.setHint(q.up, "Move "+c.Label+" up the quickbar")
	q.down = gtk.NewButtonWithLabel("▼")
	q.down.ConnectClicked(func() { s.moveQuickbar(id, 1, q.down) })
	w.setHint(q.down, "Move "+c.Label+" down the quickbar")
	arrows.Append(q.up)
	arrows.Append(q.down)
	q.box.Append(arrows)

	q.sw = gtk.NewSwitch()
	q.sw.SetVAlign(gtk.AlignCenter)
	w.setHint(q.sw, "Show "+c.Label+" in the quickbar")
	q.sw.ConnectStateSet(func(on bool) bool {
		if !s.qbSyncing {
			s.showQuickbar(id, on, q.sw)
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(q.sw, func() { q.sw.SetActive(!q.sw.Active()) })
	}
	q.box.Append(q.sw)
	return q
}

func (s *settingsView) rowIndex(id string) int {
	for i, r := range s.qbRows {
		if r.Control.ID == id {
			return i
		}
	}
	return -1
}

func (s *settingsView) moveQuickbar(id string, delta int, from gtk.Widgetter) {
	if rows, ok := controls.MoveRow(s.qbRows, s.rowIndex(id), delta); ok {
		s.applyQuickbar(rows, from)
	}
}

func (s *settingsView) showQuickbar(id string, on bool, from gtk.Widgetter) {
	i := s.rowIndex(id)
	if i < 0 {
		return
	}
	rows := append([]controls.EditorRow(nil), s.qbRows...)
	rows[i].Shown = on
	s.applyQuickbar(rows, from)
}

// applyQuickbar saves rows, rebuilds the drawer, and moves the card onto
// them. from is the widget that asked, which keeps the gamepad focus while its
// row moves.
func (s *settingsView) applyQuickbar(rows []controls.EditorRow, from gtk.Widgetter) {
	if s.qbLocked {
		return
	}
	w := s.w
	prev := controls.ParseList(theme.LoadAppConfig().QuickbarControls)
	saved := controls.SavedList(rows, prev, w.device, w.sessionCaps()...)
	theme.UpdateAppConfig(func(cfg *theme.AppConfig) { cfg.QuickbarControls = controls.FormatList(saved) })
	slog.Info("quickbar layout changed", "controls", controls.FormatList(saved))

	s.qbRows = rows
	w.rebuildDrawer()
	s.renderQuickbar()

	s.buildFocusList()
	if s.host.current() {
		w.swapFocusList(s.focusItems)
		w.focusWidget(from)
	}
}

// renderQuickbar puts the rows in order and every control in its state: the
// switch, the arrows that can move (none past either end), and the reset,
// live only when there is a saved layout to discard.
func (s *settingsView) renderQuickbar() {
	s.qbSyncing = true
	defer func() { s.qbSyncing = false }()

	var prev *gtk.Box
	for i, r := range s.qbRows {
		q := s.qbWidgets[r.Control.ID]
		if prev == nil {
			s.qbList.ReorderChildAfter(q.box, nil)
		} else {
			s.qbList.ReorderChildAfter(q.box, prev)
		}
		prev = q.box
		q.sw.SetActive(r.Shown)
		q.sw.SetSensitive(!s.qbLocked)
		q.up.SetSensitive(!s.qbLocked && i > 0)
		q.down.SetSensitive(!s.qbLocked && i < len(s.qbRows)-1)
	}
	s.qbReset.SetSensitive(!s.qbLocked && theme.LoadAppConfig().QuickbarControls != "")
}

// appendQuickbarFocus adds the card's rows in their current order, each a
// line of three — ▲, ▼, switch — since they sit side by side; then the reset.
func (s *settingsView) appendQuickbarFocus(b *focusgrid.Builder, items *[]focusItem) {
	if s.qbList == nil {
		return
	}
	b.Section("quickbar")
	for _, r := range s.qbRows {
		q := s.qbWidgets[r.Control.ID]
		widgets := []gtk.Widgetter{q.up, q.down, q.sw}
		for i, c := range b.Line(3) {
			switch wd := widgets[i].(type) {
			case *gtk.Button:
				*items = append(*items, focusItem{
					widget: wd, row: c.Row, col: c.Col, section: c.Section,
					onActivate: func() { wd.Activate() },
				})
			case *gtk.Switch:
				*items = append(*items, focusItem{
					widget: wd, row: c.Row, col: c.Col, section: c.Section,
					onActivate: func() { wd.SetActive(!wd.Active()) },
				})
			}
		}
	}
	c := b.One()
	*items = append(*items, focusItem{
		widget: s.qbReset, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { s.qbReset.Activate() },
	})
}
