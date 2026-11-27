// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// chargeonce.go — the one-time full charge: a switch that starts or cancels
// the daemon's batterylimit-full-once, and a note saying what it will do or is
// doing. The window has it under the charge-limit slider; the drawer has it
// only as an opt-in control ("charge-once"), off by default, because it is not
// used often enough to earn space there (Jeff, 2026-11-26).
//
// The charge-limit slider keeps showing the limit throughout, which is the
// limit the charge returns to, and moving it cancels the charge (the daemon's
// rule), so the two controls never disagree about what the limit is.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type chargeOnceControl struct {
	w       *Window
	sw      *gtk.Switch
	note    *gtk.Label
	row     *gtk.Box // window only: the switch with the note beside it
	desktop bool
}

// newChargeOnceControl builds the switch and its note. Insensitive until state
// says there is a limit to lift: with none the battery charges to full
// already, and the daemon refuses.
func (w *Window) newChargeOnceControl(desktop bool) *chargeOnceControl {
	c := &chargeOnceControl{w: w, desktop: desktop}
	sw := gtk.NewSwitch()
	sw.SetVAlign(gtk.AlignCenter)
	sw.SetSensitive(false)
	sw.ConnectStateSet(func(on bool) bool {
		// state-set fires on a programmatic SetActive too.
		if !w.syncing {
			w.sendChargeOnce(on)
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(sw, func() { sw.SetActive(!sw.Active()) })
	}
	c.sw = sw

	note := gtk.NewLabel("")
	note.AddCSSClass("setting-desc")
	note.SetXAlign(0)
	note.SetWrap(true)
	c.note = note

	// Each surface lays the two out itself, so neither widget is ever
	// appended twice: a GTK widget has one parent, and a second Append fails
	// with only a warning, leaving the widget out of the layout (the drawer
	// first showed its heading with no switch for exactly that).
	if desktop {
		c.row = gtk.NewBox(gtk.OrientationHorizontal, 8)
		c.row.Append(sw)
		c.row.Append(note)
	}
	return c
}

// buildChargeOnceSection is the drawer's opt-in section: the heading and the
// switch on one line, the CPU boost section's shape, and the note under them
// while a charge runs. Not synced here: the drawer's build and rebuild sync
// every section afterwards under w.syncing (syncBattery reaches this one), and
// a SetActive outside it would be sent to the daemon as the user's choice.
func (w *Window) buildChargeOnceSection() *gtk.Box {
	c := w.newChargeOnceControl(false)
	w.setHint(c.sw, "Charge to 100% this once, then go back to the limit")
	c.sw.SetHAlign(gtk.AlignEnd)
	c.sw.SetHExpand(true)
	head := gtk.NewBox(gtk.OrientationHorizontal, 4)
	head.Append(sectionLabel("FULL CHARGE ONCE"))
	head.Append(c.sw)
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(head)
	box.Append(c.note)
	w.chargeOnce = c
	return box
}

func (w *Window) focusChargeOnceSection(b *focusgrid.Builder, items *[]focusItem) {
	w.chargeOnce.appendFocus(b.Section("charge-once"), items)
}

// sync moves the switch and note to the daemon's state. The window's note
// always explains the switch; the drawer's shows only while a charge runs,
// since the switch's hint says the rest and the drawer is narrow. Callers
// hold w.syncing. Safe on a nil control (one not built on this surface).
func (c *chargeOnceControl) sync(st *api.State) {
	if c == nil {
		return
	}
	active := st != nil && st.ChargeOnce
	limited := st != nil && st.Battery > 0 && st.Battery < c.w.limits.BatteryMax
	c.sw.SetSensitive(active || limited)
	c.sw.SetActive(active)
	switch {
	case active:
		c.note.SetLabel(fmt.Sprintf("Charging to 100%%, then back to %d%%", st.Battery))
	case c.desktop && limited:
		c.note.SetLabel(fmt.Sprintf("Charge to 100%% once, then back to %d%%", st.Battery))
	case c.desktop:
		c.note.SetLabel("Set a limit below 100% first")
	default:
		c.note.SetLabel("")
	}
	c.note.SetVisible(c.note.Label() != "")
}

// appendFocus appends the switch as one focus item in the builder's current
// section. Safe on a nil control.
func (c *chargeOnceControl) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	if c == nil {
		return
	}
	sw := c.sw
	pos := b.One()
	*items = append(*items, focusItem{
		widget: sw, row: pos.Row, col: pos.Col, section: pos.Section,
		onActivate: func() { sw.SetActive(!sw.Active()) },
	})
}
