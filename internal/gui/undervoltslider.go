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
	"log/slog"
	"strconv"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
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
	timer *time.Timer

	// pending and gen hold sync off from the first move until the send has
	// landed; see tdpSliderSection. Main thread only.
	pending bool
	gen     int
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
	s := &undervoltSliderSection{w: w}
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
	w := s.w
	if w.syncing {
		return
	}
	s.pending = true
	s.gen++
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(undervoltSliderDebounce, func() {
		glib.IdleAdd(func() bool {
			co := strconv.Itoa(int(s.scale.Value())) // read on the GTK thread
			gen := s.gen
			go func() {
				slog.Debug("undervolt slider: calling daemon", "cpu", co)
				err := apiresult.Err(api.SendUndervoltSet(co))
				glib.IdleAdd(func() bool {
					if s.gen == gen {
						s.pending = false
					}
					return false
				})
				if err != nil {
					w.reportError("Set undervolt", err)
				} else {
					w.clearErrorAsync()
				}
				w.refreshState() // onto what the daemon holds either way
			}()
			return false
		})
	})
}

// sync sets the slider from daemon state. Callers hold w.syncing.
func (s *undervoltSliderSection) sync() {
	if s.pending {
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
