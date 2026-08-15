// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// battery.go — the charge-limit slider.
//
// One control, one file, because it is now built twice: the drawer main view's
// and the dashboard rail's. It was three fragments on Window (a scale field, a
// builder in controls.go, a sync and a debounce in sync.go), which is fine for
// a singleton and wrong the moment there are two — the debounce in particular
// closed over one scale while syncBattery wrote another.

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// batteryLimitDebounce is how long a drag settles before the limit is written.
const batteryLimitDebounce = 200 * time.Millisecond

// batterySection is the charge-limit block.
type batterySection struct {
	w     *Window
	scale *gtk.Scale
	value *gtk.Label // window only: the inline readout beside the slider
	timer *time.Timer
}

// buildBatterySection creates the drawer main view's BATTERY LIMIT section and
// registers it as w.battery, which the control registry's focus half reads.
func (w *Window) buildBatterySection() *gtk.Box {
	b, box := w.newBatterySection(false)
	w.battery = b
	return box
}

// newBatterySection creates a charge-limit block: the cap scale (40–100%),
// debounced.
//
// The window shows the percentage in a label beside the slider; the drawer
// keeps GtkScale's own floating value. That is the one real behavioural
// difference between the two shapes: the built-in value sits above the handle
// and travels with it, which is right under a thumb that is already there and
// wrong on a page you are scanning, where the number you want to read is the
// one that will not hold still.
func (w *Window) newBatterySection(desktop bool) (*batterySection, *gtk.Box) {
	b := &batterySection{w: w}

	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, 40, 100, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(!desktop)
	sc.SetValue(80)
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
		return b, formRow("Charge limit", formSlider(sc, b.value))
	}

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(sectionLabel("BATTERY LIMIT"))
	box.Append(sc)
	return b, box
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
	w := b.w
	// The syncing guard every other input has. sync() sets this scale from
	// daemon state, which fires this handler, so without it every drawer open
	// wrote the limit straight back to the hardware 200ms later. Harmless while
	// the write succeeds — it is the value the daemon just reported — but on a
	// device that rejects it that is now a visible error bar on every open,
	// since daemon failures are no longer swallowed.
	//
	// Checked here rather than in the timer: by the time it fires the sync has
	// long finished and the flag is false again.
	if w.syncing {
		return
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(batteryLimitDebounce, func() {
		glib.IdleAdd(func() bool {
			val := int(b.scale.Value()) // scale read must stay on the GTK thread
			go func() {
				slog.Debug("sendBatteryLimitSet: calling daemon", "limit", val)
				start := time.Now()
				if err := apiresult.Err(api.SendBatteryLimitSet(val)); err != nil {
					w.reportError("Set battery limit", err)
					return
				}
				w.clearErrorAsync()
				slog.Debug("sendBatteryLimitSet: done", "elapsed", time.Since(start))
			}()
			return false
		})
	})
}

// sync sets the scale from daemon state. Callers must hold w.syncing.
func (b *batterySection) sync() {
	st := b.w.state
	if st == nil || st.Battery == 0 || b.scale == nil {
		return
	}
	b.scale.SetValue(float64(st.Battery))
	b.syncValueLabel()
}

// appendFocus appends this instance's focus item: the charge-limit slider.
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
}
