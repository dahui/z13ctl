// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// rumbleview.go — the built-in game controller's vibration strength (the
// daemon's rumble commands; hid-oxp's rumble_intensity on the OneXPlayer X2).
// Built twice: a row in the Settings page's CONTROLLER card, and a drawer
// control ("rumble", in a handheld's device default). The rules (whether it
// exists, its scale, the readout) are internal/settingsui's.

import (
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/settingsui"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// rumbleDebounce settles a drag before the write. Short: each write is one
// HID report, and the strength is felt on the next rumble, not as it moves.
const rumbleDebounce = 250 * time.Millisecond

// rumbleSection is one vibration-strength slider.
type rumbleSection struct {
	w       *Window
	scale   *gtk.Scale
	value   *gtk.Label
	hi      int
	section string // focus section: "controller" on Settings, "rumble" in the drawer
	timer   *time.Timer

	// pending holds sync off from the first move until the send has landed,
	// so a refresh cannot yank the handle mid-drag; gen keeps an earlier send
	// from clearing a later drag's hold. Main thread only.
	pending bool
	gen     int
}

// newRumbleSection builds the slider, or returns nil when the device offers
// no vibration control. desktop is the Settings row (name, description and
// slider on one line); otherwise the drawer's heading with the value at its
// end, like the other drawer sliders.
func (w *Window) newRumbleSection(desktop bool) (*rumbleSection, *gtk.Box) {
	lo, hi, ok := settingsui.RumbleRange(w.device)
	if !ok {
		return nil, nil
	}
	r := &rumbleSection{w: w, hi: hi, section: "rumble"}
	r.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontal, float64(lo), float64(hi), 1)
	r.scale.SetDigits(0)
	r.scale.SetDrawValue(false)
	r.scale.SetFocusable(false)
	r.scale.SetSensitive(false) // until a strength has been read: see sync
	w.wheelScrollsView(r.scale)
	w.setHint(r.scale, "The controller's vibration strength; the far left turns it off")
	r.value = formValueLabel("")
	r.scale.ConnectValueChanged(func() {
		r.value.SetLabel(settingsui.RumbleText(int(r.scale.Value()), r.hi))
		r.queueSend()
	})

	if desktop {
		r.section = "controller"
		line := gtk.NewBox(gtk.OrientationHorizontal, 8)
		line.SetMarginTop(4)
		text := gtk.NewBox(gtk.OrientationVertical, 2)
		text.SetHExpand(true)
		name := gtk.NewLabel("Vibration strength")
		name.SetHAlign(gtk.AlignStart)
		name.AddCSSClass("setting-name")
		text.Append(name)
		desc := gtk.NewLabel("How strongly the controller rumbles when a game asks it to. voltaire puts it back after a restart or a suspend.")
		desc.SetHAlign(gtk.AlignStart)
		desc.SetXAlign(0)
		desc.SetWrap(true)
		desc.AddCSSClass("setting-desc")
		text.Append(desc)
		line.Append(text)
		r.scale.SetSizeRequest(220, -1)
		slider := formSlider(r.scale, r.value)
		slider.SetHExpand(false)
		slider.SetVAlign(gtk.AlignCenter)
		line.Append(slider)
		return r, line
	}

	r.scale.SetHExpand(true)
	head := gtk.NewBox(gtk.OrientationHorizontal, 4)
	head.Append(sectionLabel("VIBRATION"))
	r.value.SetHExpand(true)
	r.value.SetHAlign(gtk.AlignEnd)
	head.Append(r.value)
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(head)
	box.Append(r.scale)
	return r, box
}

// buildRumbleCard returns the Settings page's CONTROLLER card, or nil when the
// device offers no vibration control.
func (s *settingsView) buildRumbleCard() *gtk.Box {
	r, row := s.w.newRumbleSection(true)
	if r == nil {
		return nil
	}
	s.rumble = r
	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("section-card")
	card.SetMarginTop(12)
	heading := gtk.NewLabel("CONTROLLER")
	heading.SetHAlign(gtk.AlignStart)
	heading.AddCSSClass("section-label")
	card.Append(heading)
	card.Append(row)
	return card
}

// queueSend debounces the write; see tdpSliderSection.queueSend for the
// syncing check and the pending hold. The refresh afterwards moves every
// instance onto the daemon's answer.
func (r *rumbleSection) queueSend() {
	w := r.w
	if w.syncing {
		return
	}
	r.pending = true
	r.gen++
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(rumbleDebounce, func() {
		glib.IdleAdd(func() bool {
			v := int(r.scale.Value()) // read on the GTK thread
			gen := r.gen
			go func() {
				err := apiresult.Err(api.SendRumbleSet(v))
				glib.IdleAdd(func() bool {
					if r.gen == gen {
						r.pending = false
					}
					return false
				})
				if err != nil {
					w.reportError("Set vibration strength", err)
				} else {
					w.clearErrorAsync()
				}
				w.refreshState()
			}()
			return false
		})
	})
}

// sync moves the slider onto get-state's live strength. Callers hold
// w.syncing.
func (r *rumbleSection) sync() {
	if r == nil || r.pending {
		return
	}
	v, ok := settingsui.RumbleValue(r.w.state)
	r.scale.SetSensitive(ok)
	if ok {
		r.scale.SetValue(float64(v))
		r.value.SetLabel(settingsui.RumbleText(v, r.hi))
	} else {
		r.value.SetLabel("")
	}
}

// appendFocus adds the slider, stepping one notch per D-pad press.
func (r *rumbleSection) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	if r == nil {
		return
	}
	left, right, get, set := scaleAdjust(r.scale, 1)
	c := b.Section(r.section).One()
	*items = append(*items, focusItem{
		widget: r.scale, row: c.Row, col: c.Col, section: c.Section,
		editable: true,
		onLeft:   left, onRight: right,
		getValue: get, setValue: set,
	})
}

// buildRumbleSection is the drawer's control; the registry only builds it on a
// device with the capability, so the nil return cannot reach Append.
func (w *Window) buildRumbleSection(inner *gtk.Box) {
	r, box := w.newRumbleSection(false)
	if r == nil {
		return
	}
	w.rumble = r
	inner.Append(box)
}

func (w *Window) focusRumbleSection(b *focusgrid.Builder, items *[]focusItem) {
	w.rumble.appendFocus(b, items) // nil-safe
}

// syncRumble moves the drawer's slider; the Settings page's moves with its
// own sync. Callers hold w.syncing.
func (w *Window) syncRumble() {
	w.rumble.sync() // nil-safe
}
