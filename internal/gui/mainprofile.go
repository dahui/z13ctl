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

// profileSection is the main view's PROFILE block.
type profileSection struct {
	w *Window

	btns      map[string]*gtk.Button // firmware profile buttons by name
	customBtn *gtk.Button            // opens the custom view; labelled with the running custom profile
}

// buildProfileSection creates the main view's PROFILE section: the three
// firmware profiles on one row, and a single Custom button beneath them.
//
// The custom profiles are deliberately not listed here. One row per saved
// profile pushed the RGB and battery controls off the bottom of a 320px
// drawer, so the whole family collapses to one button that opens the custom
// view; the button is labelled with the running custom profile, so the main
// view still says what is in force.
func (w *Window) buildProfileSection() *gtk.Box {
	p := &profileSection{w: w, btns: make(map[string]*gtk.Button)}
	w.profiles = p

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("PROFILE"))

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
	box.Append(stockRow)

	// In a .btn-group of its own: the .active style that marks the running
	// profile is scoped to that class, so a bare button would never highlight.
	customRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	customRow.AddCSSClass("btn-group")
	p.customBtn = gtk.NewButtonWithLabel(profileui.Custom(nil).Label)
	p.customBtn.SetHExpand(true)
	w.setHint(p.customBtn, "Custom profiles: power limits, fan curve and undervolt")
	p.customBtn.ConnectClicked(func() { w.showCustomView() })
	customRow.Append(p.customBtn)
	box.Append(customRow)

	return box
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
	p.customBtn.SetLabel(cs.Label)
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
}

// buildAutoswitchSection creates the AUTOSWITCH section: the enable switch and
// a dropdown per power source. The dropdowns open in the in-surface popup
// layer (popup.go), so they work under gamescope where a GtkDropDown's popover
// would be invisible.
func (w *Window) buildAutoswitchSection() *gtk.Box {
	a := &autoswitchSection{w: w}
	w.autoswitch = a

	box := gtk.NewBox(gtk.OrientationVertical, 4)

	labelRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	labelRow.Append(sectionLabel("AUTOSWITCH"))
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
	labelRow.Append(sw)
	box.Append(labelRow)

	// The two target rows are shown only while autoswitch is enabled: they
	// are meaningless when it is off, and this is the main view, where three
	// permanent rows for a feature most users leave alone is exactly the
	// crowding the profile list was moved out to avoid.
	a.targets = gtk.NewBox(gtk.OrientationVertical, 4)
	a.targets.Append(a.buildTargetRow("On AC", &a.ac, &a.acDD))
	a.targets.Append(a.buildTargetRow("On battery", &a.batt, &a.battDD))
	a.targets.SetVisible(false)
	box.Append(a.targets)
	return box
}

// buildTargetRow creates one "label + dropdown" row. target and ddDst point at
// the section's fields for this side; both are only ever touched on the GTK
// thread.
func (a *autoswitchSection) buildTargetRow(label string, target *string, ddDst **dropdown) *gtk.Box {
	w := a.w
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

	var d *dropdown
	d = w.newDropdown(dropdownConfig{
		options: func() []dropdownOption {
			opts := profileui.TargetOptions(w.state)
			rows := make([]dropdownOption, len(opts))
			for i, o := range opts {
				rows[i] = dropdownOption{
					value:    o,
					label:    profileui.TargetLabel(o),
					selected: o == *target,
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

// focusProfileSection appends the PROFILE block's focus items.
func (w *Window) focusProfileSection(b *focusgrid.Builder, items *[]focusItem) {
	p := w.profiles
	if p == nil {
		return
	}
	stock := profileui.StockRows(nil)
	b.Section("profile")
	for i, c := range b.Line(len(stock)) {
		btn := p.btns[stock[i].Name]
		*items = append(*items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.Activate() },
		})
	}
	if btn := p.customBtn; btn != nil {
		c := b.One()
		*items = append(*items, focusItem{
			widget: btn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { btn.Activate() },
		})
	}
}

// focusAutoswitchSection appends the AUTOSWITCH block's focus items. The two
// target rows are only navigable while the feature is enabled, matching what
// the pointer can reach.
func (w *Window) focusAutoswitchSection(b *focusgrid.Builder, items *[]focusItem) {
	a := w.autoswitch
	if a == nil || a.sw == nil {
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

// Window-level entry points. Each nil-guards its section, which the control
// registry may have dropped for this device.

func (w *Window) syncProfiles() {
	if w.profiles != nil {
		w.profiles.sync()
	}
}

func (w *Window) syncAutoswitch() {
	if w.autoswitch != nil {
		w.autoswitch.sync()
	}
}
