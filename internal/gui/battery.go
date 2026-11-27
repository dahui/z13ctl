// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// battery.go — the charge-limit slider (and, in the window, the one-time full
// charge under it: chargeonce.go).
//
// One control, one file, because it is now built twice: the drawer main view's
// and the dashboard rail's. It was three fragments on Window (a scale field, a
// builder in controls.go, a sync and a debounce in sync.go), which is fine for
// a singleton and wrong the moment there are two — the debounce in particular
// closed over one scale while syncBattery wrote another.

import (
	"fmt"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// batteryLimitDebounce is how long a drag settles before the limit is written.
const batteryLimitDebounce = 200 * time.Millisecond

// batterySection is the charge-limit block.
type batterySection struct {
	w     *Window
	scale *gtk.Scale
	value *gtk.Label // window only: the inline readout beside the slider
	send  settleSend

	// once is the one-time full charge, window only: the drawer has it as an
	// opt-in control of its own (w.chargeOnce), off by default, because it is
	// not used often enough to earn space there (Jeff, 2026-11-26).
	once *chargeOnceControl
}

// buildBatterySection creates the drawer main view's BATTERY LIMIT section and
// registers it as w.battery, which the control registry's focus half reads.
func (w *Window) buildBatterySection() *gtk.Box {
	b, box := w.newBatterySection(false)
	w.battery = b
	return box
}

// newBatterySection creates a charge-limit block: the cap scale over the
// device's range (w.limits; the kernel publishes none, so it is device data),
// debounced.
//
// The window shows the percentage in a label beside the slider; the drawer
// keeps GtkScale's own floating value. That is the one real behavioural
// difference between the two shapes: the built-in value sits above the handle
// and travels with it, which is right under a thumb that is already there and
// wrong on a page you are scanning, where the number you want to read is the
// one that will not hold still.
func (w *Window) newBatterySection(desktop bool) (*batterySection, *gtk.Box) {
	b := &batterySection{w: w, send: settleSend{delay: batteryLimitDebounce}}

	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal,
		float64(w.limits.BatteryMin), float64(w.limits.BatteryMax), 1)
	sc.SetDigits(0)
	sc.SetDrawValue(!desktop)
	// The top of the range — no limit — until the first sync says otherwise.
	sc.SetValue(float64(w.limits.BatteryMax))
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() {
		b.syncValueLabel()
		b.queueSend()
	})
	b.scale = sc

	if desktop {
		b.value = formValueLabel("")
		b.syncValueLabel()
		b.once = w.newChargeOnceControl(true)
		box := gtk.NewBox(gtk.OrientationVertical, 4)
		box.Append(formRow("Charge limit", formSlider(sc, b.value)))
		box.Append(subFormRow("Full charge", b.once.row))
		return b, box
	}

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("BATTERY LIMIT"))
	box.Append(sc)
	return b, box
}

// applyLimits moves the scale onto the device's current range, in place. The
// caller holds w.syncing, so a value SetRange clamps is not sent as an edit.
func (b *batterySection) applyLimits() {
	b.scale.SetRange(float64(b.w.limits.BatteryMin), float64(b.w.limits.BatteryMax))
	b.syncValueLabel()
}

// syncValueLabel keeps the inline readout on the slider. A no-op in the drawer,
// which has no such label.
func (b *batterySection) syncValueLabel() {
	if b.value != nil {
		b.value.SetLabel(fmt.Sprintf("%d%%", int(b.scale.Value())))
	}
}

// queueSend debounces the write. The delay is about the drag, not chattiness:
// a slider passes through every value between its ends, and each is a real
// sysfs write on the way to the one the user meant.
func (b *batterySection) queueSend() {
	b.send.queue(b.w, func() int { return int(b.scale.Value()) }, api.SendBatteryLimitSet, "Set battery limit", false)
}

// sync sets the scale (and the window's one-time charge) from daemon state.
// Callers must hold w.syncing.
func (b *batterySection) sync() {
	st := b.w.state
	if b.scale == nil {
		return
	}
	b.once.sync(st)
	if st == nil || st.Battery == 0 {
		return
	}
	b.scale.SetValue(float64(st.Battery))
	b.syncValueLabel()
}

// appendFocus appends this instance's focus items: the charge-limit slider,
// then (in the window) the one-time charge switch below it.
func (b *batterySection) appendFocus(bd *focusgrid.Builder, items *[]focusItem) {
	if b.scale == nil {
		return
	}
	left, right, get, set := scaleAdjust(b.scale, 5)
	c := bd.Section("battery").One()
	*items = append(*items, focusItem{
		widget: b.scale, row: c.Row, col: c.Col, section: c.Section,
		editable: true,
		onLeft:   left, onRight: right,
		getValue: get, setValue: set,
	})
	b.once.appendFocus(bd, items)
}

// focusBatterySection appends the drawer main view's charge-limit slider; the
// dashboard rail appends its own instance's in the dashboard's focus list.
func (w *Window) focusBatterySection(b *focusgrid.Builder, items *[]focusItem) {
	if w.battery != nil {
		w.battery.appendFocus(b, items)
	}
}

// batterySections returns every charge-limit block that has been built.
func (w *Window) batterySections() []*batterySection {
	out := make([]*batterySection, 0, 2)
	if w.battery != nil {
		out = append(out, w.battery)
	}
	if m := w.mainWin; m != nil && m.dashboard != nil && m.dashboard.battery != nil {
		out = append(out, m.dashboard.battery)
	}
	return out
}

func (w *Window) syncBattery() {
	for _, b := range w.batterySections() {
		b.sync()
	}
	w.chargeOnce.sync(w.state) // the drawer's opt-in control, when built
}
