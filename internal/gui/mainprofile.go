// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// mainprofile.go — the two profile-related sections of the *main* view: the
// firmware profile buttons, and the AC/battery autoswitch pair.
//
// They are separate structs because the control registry can drop either one
// independently: autoswitch needs both the profile and battery capabilities,
// since the daemon can only observe a power-source change through the latter.
// The custom view's profile selector is a different thing entirely and stays in
// profiles.go.
//
// Every rule (which rows exist, which targets are offered, how a name is
// validated) lives in internal/profileui where it is unit tested; these are the
// widgets and the daemon calls.

import (
	"log/slog"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// profileSection is a PROFILE block: the firmware profiles, and one button
// standing for the whole custom family.
type profileSection struct {
	w *Window

	btns      map[string]*gtk.Button // firmware profile buttons by name
	customBtn *gtk.Button            // opens the custom profiles; labelled with the running one

	// onCustom is where the Custom button goes. It differs by surface — the
	// drawer's own custom view, or the full window's Profiles tab.
	onCustom func()

	// desktop selects the window's form row over the drawer's stacked block.
	desktop bool
}

// buildProfileSection creates the drawer main view's PROFILE section and
// registers it as w.profiles, which the control registry's focus half reads.
func (w *Window) buildProfileSection() *gtk.Box {
	p, box := w.newProfileSection(false, func() { w.showCustomView() })
	w.profiles = p
	return box
}

// newProfileSection creates a PROFILE block: the firmware profiles, and one
// button standing for the whole custom family.
//
// The custom profiles are deliberately not listed here. One row per saved
// profile pushed the RGB and battery controls off the bottom of a 320px
// drawer, so the whole family collapses to one button that opens the editor;
// the button is labelled with the running custom profile, so the surface still
// says what is in force. Two instances exist — the drawer main view's and the
// dashboard's — and each is synced through Window.profileSections.
//
// The window puts all four on one line, where the drawer stacks the Custom
// button under the three. That is not only about width. Stacked, Custom reads
// as a fourth profile you can select, which it is not: it *navigates*, and the
// desktop convention for a control that opens something else is a trailing
// ellipsis. On one line with the ellipsis it is unmistakably the odd one out,
// and the row still says what is running.
func (w *Window) newProfileSection(desktop bool, onCustom func()) (*profileSection, *gtk.Box) {
	p := &profileSection{
		w: w, btns: make(map[string]*gtk.Button), onCustom: onCustom, desktop: desktop,
	}

	stockRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	stockRow.AddCSSClass("btn-group")
	stockRow.SetHomogeneous(true)
	for _, r := range profileui.StockRows(nil) {
		r := r
		btn := gtk.NewButtonWithLabel(r.Label)
		btn.SetHExpand(true)
		btn.ConnectClicked(func() {
			// Optimistic highlight, as the stock buttons always had;
			// sendProfileSet refreshes state itself once the daemon has
			// applied the profile.
			setActiveButton(p.btns, r.Name)
			w.sendProfileSet(r.Name)
		})
		p.btns[r.Name] = btn
		stockRow.Append(btn)
	}

	p.customBtn = gtk.NewButtonWithLabel(p.customLabel(nil))
	p.customBtn.SetHExpand(true)
	w.setHint(p.customBtn, "Custom profiles: power limits, fan curve and undervolt")
	p.customBtn.ConnectClicked(func() { p.openCustom() })

	if desktop {
		stockRow.Append(p.customBtn)
		return p, formRow("Profile", stockRow)
	}

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("PROFILE"))
	box.Append(stockRow)
	// In a .btn-group of its own: the .active style that marks the running
	// profile is scoped to that class, so a bare button would never highlight.
	customRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	customRow.AddCSSClass("btn-group")
	customRow.Append(p.customBtn)
	box.Append(customRow)
	return p, box
}

// customLabel is the Custom button's text: the running custom profile's name,
// with the window's trailing ellipsis marking it as a control that opens the
// editor rather than one that selects a profile.
func (p *profileSection) customLabel(st *api.State) string {
	label := profileui.Custom(st).Label
	if p.desktop {
		return label + "…"
	}
	return label
}

// openCustom follows the Custom button to whichever profile editor this
// surface owns.
func (p *profileSection) openCustom() {
	if p.onCustom != nil {
		p.onCustom()
	}
}

// sync updates the main view's profile controls from daemon state. The section
// never changes shape, so this only moves highlights and the Custom button's
// label.
//
// Callers must hold w.syncing (as syncState does).
func (p *profileSection) sync() {
	st := p.w.state
	if st != nil && st.Profile != "" {
		setActiveButton(p.btns, st.Profile)
	}
	if p.customBtn == nil {
		return
	}
	cs := profileui.Custom(st)
	p.customBtn.SetLabel(p.customLabel(st))
	if cs.Active {
		p.customBtn.AddCSSClass("active")
	} else {
		p.customBtn.RemoveCSSClass("active")
	}
}

// autoswitchSection is the main view's AUTOSWITCH block: an enable switch and
// one profile target per power source.
type autoswitchSection struct {
	w *Window

	sw      *gtk.Switch
	targets *gtk.Box // the two target rows; shown only while enabled
	acDD    *dropdown
	battDD  *dropdown

	// The three value fields mirror the widgets so a send can snapshot them on
	// the GTK thread; they are synced from daemon state.
	enabled bool
	ac      string
	batt    string

	timer *time.Timer // debounce, so re-picking a target sends once

	// desktop selects the window's form rows over the drawer's stacked block.
	desktop bool
}

// buildAutoswitchSection creates the drawer main view's AUTOSWITCH section and
// registers it as w.autoswitch, which syncAutoswitch and the main view's focus
// list read.
func (w *Window) buildAutoswitchSection() *gtk.Box {
	a, box := w.newAutoswitchSection(false)
	w.autoswitch = a
	return box
}

// newAutoswitchSection creates an AUTOSWITCH block: the enable switch and a
// dropdown per power source. The dropdowns open in the in-surface popup layer
// (popup.go), so they work under gamescope where a GtkDropDown's popover would
// be invisible — and on whichever surface hosts the instance, since openPopup
// targets activePopup(). Two instances exist: the drawer main view's
// (buildAutoswitchSection) and the full window's Profiles page (Jeff,
// 2026-08-14: autoswitch belongs with the profile controls). Each carries its
// own debounce timer and mirror fields, so the two cannot interleave a send.
func (w *Window) newAutoswitchSection(desktop bool) (*autoswitchSection, *gtk.Box) {
	a := &autoswitchSection{w: w, desktop: desktop}

	box := gtk.NewBox(gtk.OrientationVertical, 4)

	sw := gtk.NewSwitch()
	sw.SetHAlign(gtk.AlignEnd)
	sw.SetHExpand(true)
	w.setHint(sw, "Switch profiles automatically when the charger is plugged or unplugged")
	sw.ConnectStateSet(func(on bool) bool {
		if !w.syncing {
			a.enabled = on
			a.queueSend()
			a.syncVis()
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(sw, func() { sw.SetActive(!sw.Active()) })
	}
	a.sw = sw

	if desktop {
		// The switch sits at the start of the control column like every other
		// control on the page, not pushed to the far edge as it is in the
		// drawer's label row — a form's controls line up with each other, and a
		// switch alone at the right margin reads as belonging to nothing.
		sw.SetHAlign(gtk.AlignStart)
		// The two targets are indented under it: they are meaningless without
		// it, and the indent is what says so on a page where every other row
		// stands on its own.
		box.Append(formRow("Autoswitch", sw))
	} else {
		labelRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
		labelRow.Append(sectionLabel("AUTOSWITCH"))
		labelRow.Append(sw)
		box.Append(labelRow)
	}

	// The two target rows are shown only while autoswitch is enabled: they
	// are meaningless when it is off, and in the drawer this is the main view,
	// where three permanent rows for a feature most users leave alone is
	// exactly the crowding the profile list was moved out to avoid.
	a.targets = gtk.NewBox(gtk.OrientationVertical, 4)
	a.targets.Append(a.buildTargetRow("On AC", &a.ac, &a.acDD))
	a.targets.Append(a.buildTargetRow("On battery", &a.batt, &a.battDD))
	a.targets.SetVisible(false)
	box.Append(a.targets)
	return a, box
}

// buildTargetRow creates one "label + dropdown" row. target and ddDst point at
// the section's fields for this side; both are only ever touched on the GTK
// thread.
func (a *autoswitchSection) buildTargetRow(label string, target *string, ddDst **dropdown) *gtk.Box {
	w := a.w
	var d *dropdown
	d = w.newDropdown(dropdownConfig{
		options: func() []dropdownOption {
			opts := profileui.TargetRows(w.state)
			rows := make([]dropdownOption, len(opts))
			for i, o := range opts {
				rows[i] = dropdownOption{
					value: o.Name,
					label: o.Label,
					// An empty profile is shown greyed with its label saying
					// why, not hidden: a list silently missing the user's
					// profiles reads as broken (Jeff, 2026-08-14).
					disabled: o.Empty,
					selected: o.Name == *target,
				}
			}
			return rows
		},
		onSelect: func(v string) {
			*target = v
			d.setLabel(profileui.TargetLabel(v))
			a.queueSend()
		},
	})
	d.setLabel(profileui.TargetLabel(*target))
	w.setHint(d.btn, "Profile to apply when this power source becomes active")
	*ddDst = d

	if a.desktop {
		// formDropdown, not the bare trigger: a trigger outside a .btn-group
		// gets no colour from voltaire's sheet at all. See its doc — this row is
		// the one that was wrong.
		return subFormRow(label, formDropdown(d.btn))
	}

	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.AddCSSClass("btn-group")
	name := gtk.NewLabel(label)
	name.AddCSSClass("scale-name")
	name.SetHAlign(gtk.AlignStart)
	// A size request keeps the two dropdowns aligned: without it "On AC"
	// and "On battery" are different widths and the triggers start at
	// different offsets.
	name.SetSizeRequest(72, -1)
	row.Append(name)
	row.Append(d.btn)
	return row
}

// syncVis shows the target rows only while autoswitch is enabled.
func (a *autoswitchSection) syncVis() {
	if a.targets != nil {
		a.targets.SetVisible(a.enabled)
	}
}

// queueSend debounces the autoswitch config send. The debounce is not only
// about chattiness: cycling a target is several clicks in a row, and concurrent
// per-click goroutines could land on the daemon out of order, leaving an
// intermediate choice as the stored one. One timer, one snapshot of the fields
// on the GTK thread, one send.
func (a *autoswitchSection) queueSend() {
	w := a.w
	if a.timer != nil {
		a.timer.Stop()
	}
	a.timer = time.AfterFunc(300*time.Millisecond, func() {
		glib.IdleAdd(func() bool {
			enabled, ac, batt := a.enabled, a.ac, a.batt
			go func() {
				if err := apiresult.Err(api.SendAutoswitchSet(enabled, ac, batt)); err != nil {
					w.reportError("Configure autoswitch", err)
					return
				}
				w.clearErrorAsync()
				slog.Debug("autoswitch sent", "enabled", enabled, "ac", ac, "battery", batt)
				w.refreshState()
			}()
			return false
		})
	})
}

// sync updates the autoswitch widgets and their mirror fields from daemon
// state. Callers must hold w.syncing: SetActive on the switch fires its
// state-set handler, which would otherwise send the value straight back.
func (a *autoswitchSection) sync() {
	if a.sw == nil {
		return
	}
	cfg := profileui.Autoswitch(a.w.state)
	a.enabled, a.ac, a.batt = cfg.Enabled, cfg.AC, cfg.Battery
	a.sw.SetActive(cfg.Enabled)
	a.acDD.setLabel(profileui.TargetLabel(cfg.AC))
	a.battDD.setLabel(profileui.TargetLabel(cfg.Battery))
	a.syncVis()
}

// focusProfileSection appends the drawer main view's PROFILE focus items; the
// dashboard rail appends its own instance's in the dashboard's focus list.
func (w *Window) focusProfileSection(b *focusgrid.Builder, items *[]focusItem) {
	if w.profiles != nil {
		w.profiles.appendFocus(b, items)
	}
}

// appendFocus appends this instance's focus items.
//
// The grid follows the widgets: in the window all four share a line, so D-pad
// right walks from Performance to Custom…; in the drawer the Custom button is
// on its own row beneath the three, so D-pad down reaches it. Written as one
// Line for the window rather than a Line plus a One, because a coordinate that
// disagrees with what is on screen is the failure nobody notices with a mouse
// in their hand — the reason controlBuilder pairs the two halves at all.
func (p *profileSection) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	stock := profileui.StockRows(nil)
	btns := make([]*gtk.Button, 0, len(stock)+1)
	for _, r := range stock {
		btns = append(btns, p.btns[r.Name])
	}

	b.Section("profile")
	if p.desktop && p.customBtn != nil {
		btns = append(btns, p.customBtn)
	}
	for i, c := range b.Line(len(btns)) {
		btn := btns[i]
		*items = append(*items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.Activate() },
		})
	}
	if p.desktop || p.customBtn == nil {
		return
	}
	c := b.One()
	*items = append(*items, focusItem{
		widget: p.customBtn, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { p.customBtn.Activate() },
	})
}

// focusAutoswitchSection appends the drawer main view's AUTOSWITCH focus
// items; the window's Profiles page appends its own instance's in the custom
// view's focus list.
func (w *Window) focusAutoswitchSection(b *focusgrid.Builder, items *[]focusItem) {
	a := w.autoswitch
	if a == nil {
		return
	}
	a.appendFocus(b, items)
}

// appendFocus appends this instance's focus items: the switch, then the two
// target rows, navigable only while the feature is enabled — matching what
// the pointer can reach.
func (a *autoswitchSection) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	if a.sw == nil {
		return
	}
	c := b.Section("autoswitch").One()
	*items = append(*items, focusItem{
		widget: a.sw, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { a.sw.SetActive(!a.sw.Active()) },
	})

	vis := boxVisible(a.targets)
	for _, d := range []*dropdown{a.acDD, a.battDD} {
		if d == nil {
			continue
		}
		dc := b.One()
		*items = append(*items, focusItem{
			widget: d.btn, row: dc.Row, col: dc.Col, section: dc.Section,
			isVisible:  vis,
			onActivate: func() { d.btn.Activate() },
		})
	}
}

// Window-level entry points. Each walks every built instance, which is nil-safe
// on a device the control registry dropped the section for.

// profileSections returns every PROFILE block that has been built: the drawer
// main view's, and the dashboard rail's when the full window exists.
func (w *Window) profileSections() []*profileSection {
	out := make([]*profileSection, 0, 2)
	if w.profiles != nil {
		out = append(out, w.profiles)
	}
	if m := w.mainWin; m != nil && m.dashboard != nil && m.dashboard.profiles != nil {
		out = append(out, m.dashboard.profiles)
	}
	return out
}

// autoswitchSections returns every AUTOSWITCH block that has been built. The
// window's lives on the dashboard rail rather than on the Profiles page (Jeff,
// 2026-08-14: it changes what the machine is doing now rather than what a
// profile contains, so it belongs with the live controls).
func (w *Window) autoswitchSections() []*autoswitchSection {
	out := make([]*autoswitchSection, 0, 2)
	if w.autoswitch != nil {
		out = append(out, w.autoswitch)
	}
	if m := w.mainWin; m != nil && m.dashboard != nil && m.dashboard.autos != nil {
		out = append(out, m.dashboard.autos)
	}
	return out
}

func (w *Window) syncProfiles() {
	for _, p := range w.profileSections() {
		p.sync()
	}
}

func (w *Window) syncAutoswitch() {
	for _, a := range w.autoswitchSections() {
		a.sync()
	}
}
