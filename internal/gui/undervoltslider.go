// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// undervoltslider.go — the undervolt setting of a slider device (the daemon's
// State.DeviceUndervolt): one slider beside the TDP slider, in the Dashboard's
// POWER card and as an opt-in drawer control. A bare undervolt send is the
// device setting there, as a bare TDP send is the slider's; a profile's own
// offset is still edited on the Profiles page.
//
// A device on the usual model never builds it (its offset lives in custom
// profiles). Every rule — what the slider shows, the note under it — is in
// internal/profileui; this file is the widgets, the debounce and the send.

import (
	"strconv"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// undervoltSliderDebounce is how long a drag settles before the offset is
// written. Longer than the TDP slider's: every write is an SMU message that
// changes the CPU's voltage curve, and only the settled value matters.
const undervoltSliderDebounce = 600 * time.Millisecond

// undervoltSliderSection is one undervolt slider block.
type undervoltSliderSection struct {
	w     *Window
	scale *gtk.Scale
	value *gtk.Label
	note  *gtk.Label
	send  settleSend // holds sync off mid-drag; see tdpSliderSection
}

// buildUndervoltSliderSection creates the drawer's block and registers it as
// w.uvSlider, which the control registry's focus half reads.
func (w *Window) buildUndervoltSliderSection() *gtk.Box {
	s, box := w.newUndervoltSliderSection(false)
	w.uvSlider = s
	return box
}

// newUndervoltSliderSection creates an undervolt block over the device's
// offset range (w.limits.UVMin..UVMax). desktop is the Dashboard's form row;
// otherwise the drawer's heading with the value at its end.
func (w *Window) newUndervoltSliderSection(desktop bool) (*undervoltSliderSection, *gtk.Box) {
	s := &undervoltSliderSection{w: w, send: settleSend{delay: undervoltSliderDebounce}}
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal,
		float64(w.limits.UVMin), float64(w.limits.UVMax), 1)
	sc.SetDigits(0)
	sc.SetDrawValue(false)
	sc.SetHExpand(true)
	sc.SetValue(float64(w.limits.UVMax))
	sc.SetFocusable(false)
	sc.SetSensitive(false) // until the daemon has answered: see sync
	w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() {
		s.syncValueLabel()
		s.queueSend()
	})
	s.scale = sc
	s.value = formValueLabel("")
	s.note = blockNote()
	w.setHint(sc, "Lowers the CPU's voltage at every clock; the far right is stock. Too far crashes the machine")

	if desktop {
		col := gtk.NewBox(gtk.OrientationVertical, 2)
		col.Append(formSlider(sc, s.value))
		col.Append(s.note)
		return s, formRow("Undervolt", col)
	}

	head := gtk.NewBox(gtk.OrientationHorizontal, 4)
	head.Append(sectionLabel("UNDERVOLT"))
	s.value.SetHExpand(true)
	s.value.SetHAlign(gtk.AlignEnd)
	head.Append(s.value)
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(head)
	box.Append(sc)
	box.Append(s.note)
	return s, box
}

func (s *undervoltSliderSection) syncValueLabel() {
	if !s.scale.Sensitive() {
		s.value.SetLabel("")
		return
	}
	s.value.SetLabel(profileui.UndervoltValueText(int(s.scale.Value()), s.w.limits.UVText.Unit))
}

// queueSend debounces the write: a bare `voltaire undervolt --set N`, which on
// a slider device is the undervolt setting (0 clears it), leaving a running
// custom profile as the note under the slider says beforehand.
func (s *undervoltSliderSection) queueSend() {
	s.send.queue(s.w, func() int { return int(s.scale.Value()) }, func(co int) (bool, error) {
		return api.SendUndervoltSet(strconv.Itoa(co))
	}, "Set undervolt", true)
}

// sync sets the slider from daemon state. Callers hold w.syncing.
func (s *undervoltSliderSection) sync() {
	if s.send.pending {
		return
	}
	st := s.w.state
	co, ok := profileui.UndervoltSliderValue(st)
	s.scale.SetSensitive(ok)
	if ok {
		s.scale.SetValue(float64(co))
	}
	s.syncValueLabel()
	setBlockNote(s.note, profileui.UndervoltSliderNote(st))
}

// appendFocus appends this instance's slider, one step per D-pad press.
func (s *undervoltSliderSection) appendFocus(bd *focusgrid.Builder, items *[]focusItem) {
	if s == nil || s.scale == nil {
		return
	}
	left, right, get, set := scaleAdjust(s.scale, 1)
	c := bd.Section("undervolt").One()
	*items = append(*items, focusItem{
		widget: s.scale, row: c.Row, col: c.Col, section: c.Section,
		editable: true,
		onLeft:   left, onRight: right,
		getValue: get, setValue: set,
	})
}

func (w *Window) focusUndervoltSliderSection(b *focusgrid.Builder, items *[]focusItem) {
	w.uvSlider.appendFocus(b, items) // nil-safe
}

// syncUndervoltSlider moves every built instance. Callers hold w.syncing.
func (w *Window) syncUndervoltSlider() {
	if w.uvSlider != nil {
		w.uvSlider.sync()
	}
	if m := w.mainWin; m != nil && m.dashboard != nil && m.dashboard.uvSlider != nil {
		m.dashboard.uvSlider.sync()
	}
}
