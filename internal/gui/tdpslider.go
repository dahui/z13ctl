// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// tdpslider.go — the plain TDP control of a slider device (power.slider, the
// OneXConsole model): one slider that sets the machine's TDP, in the drawer and
// in the Dashboard's POWER card.
//
// A device on the usual model never builds it — its TDP lives in a custom
// profile and is edited on the Profiles page — so on the Z13 nothing here runs
// and the drawer is unchanged. Every rule (what the slider shows, what the note
// under it says, its range) is in internal/profileui and internal/limits; this
// file is the widgets, the debounce and the send.

import (
	"fmt"
	"strconv"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// tdpSliderDebounce is how long a drag settles before the TDP is written.
// Longer than the charge limit's because every write here is three SMU
// commands racing the GPU driver for the mailbox: oxp-tdpd froze a game by
// writing on every slider event, and the daemon's own spacing (pptMinGap) is a
// backstop, not a reason to send a drag's worth of values.
const tdpSliderDebounce = 400 * time.Millisecond

// tdpSliderStep is the D-pad step in watts. One watt, as OneXConsole's slider
// moves: the range is 75 W wide and held directions auto-repeat.
const tdpSliderStep = 1

// tdpSliderSection is one TDP slider block.
type tdpSliderSection struct {
	w     *Window
	scale *gtk.Scale
	value *gtk.Label // the readout: beside the heading in the drawer, beside the slider in the window
	note  *gtk.Label
	// send holds sync off from the first move of a drag until its send has
	// landed (settleSend.pending): a refresh arriving mid-drag (another client,
	// a charger edge) would otherwise yank the handle out from under the
	// finger, and the refresh after our own send brings the daemon's answer.
	send settleSend
}

// buildTDPSliderSection creates the drawer's TDP block and registers it as
// w.tdpSlider, which the control registry's focus half reads.
func (w *Window) buildTDPSliderSection() *gtk.Box {
	s, box := w.newTDPSliderSection(false)
	w.tdpSlider = s
	return box
}

// newTDPSliderSection creates a TDP block over the current source's range
// (w.limits, which follows get-state's tdp_limits: 10–85 W on the OXP's AC,
// 10–55 W on its battery).
//
// The drawer prints the watts at the end of the heading line rather than
// letting GtkScale float the number over the handle: "30" over a handle says
// nothing about the unit, and a heading row with its value at the far end is
// how the drawer's other headed rows read.
func (w *Window) newTDPSliderSection(desktop bool) (*tdpSliderSection, *gtk.Box) {
	s := &tdpSliderSection{w: w, send: settleSend{delay: tdpSliderDebounce}}

	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal,
		float64(w.limits.TDPMin), float64(w.limits.TDPSliderMax()), 1)
	sc.SetDigits(0)
	sc.SetDrawValue(false)
	sc.SetHExpand(true)
	sc.SetValue(float64(w.limits.TDPMin))
	sc.SetFocusable(false)
	// Insensitive until the daemon has reported a limit, like the boost
	// switch: a handle parked at the minimum would be a claim about the
	// hardware, and a drag from there would send it.
	sc.SetSensitive(false)
	w.wheelScrollsView(sc)
	sc.ConnectValueChanged(func() {
		s.syncValueLabel()
		s.queueSend()
	})
	s.scale = sc
	s.value = formValueLabel("")
	s.note = blockNote()
	w.setHint(sc, "The sustained power limit, applied to the fast and slow limits too")

	if desktop {
		col := gtk.NewBox(gtk.OrientationVertical, 2)
		col.Append(formSlider(sc, s.value))
		col.Append(s.note)
		return s, formRow("TDP", col)
	}

	head := gtk.NewBox(gtk.OrientationHorizontal, 4)
	head.Append(sectionLabel("TDP"))
	s.value.SetHExpand(true)
	s.value.SetHAlign(gtk.AlignEnd)
	head.Append(s.value)

	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(head)
	box.Append(sc)
	box.Append(s.note)
	return s, box
}

func (s *tdpSliderSection) syncValueLabel() {
	if !s.scale.Sensitive() {
		s.value.SetLabel("")
		return
	}
	s.value.SetLabel(fmt.Sprintf("%d W", int(s.scale.Value())))
}

// applyLimits moves the scale onto the current source's range, in place. The
// caller holds w.syncing, so a value SetRange clamps (a remembered 70 W after
// the charger is pulled) is not sent: the daemon applies the cap itself and
// the next sync shows what it holds.
func (s *tdpSliderSection) applyLimits() {
	s.scale.SetRange(float64(s.w.limits.TDPMin), float64(s.w.limits.TDPSliderMax()))
	s.syncValueLabel()
}

// queueSend debounces the write. A bare tdp set is exactly `voltaire tdp
// --set N`, which on a slider device is always the TDP setting: applied and
// remembered, and, with a custom profile running, leaving that profile
// unchanged (the note under the slider says so beforehand). The slider never
// edits a profile; that is the Profiles page's job.
func (s *tdpSliderSection) queueSend() {
	s.send.queue(s.w, func() int { return int(s.scale.Value()) }, func(watts int) (bool, error) {
		return api.SendTdpSet(strconv.Itoa(watts), "", "", "", false)
	}, "Set TDP", true)
}

// sync sets the slider from daemon state. Callers must hold w.syncing.
func (s *tdpSliderSection) sync() {
	st := s.w.state
	if s.send.pending {
		return
	}
	watts, ok := profileui.TDPSliderValue(st)
	s.scale.SetSensitive(ok)
	if ok {
		s.scale.SetValue(float64(watts))
	}
	s.syncValueLabel()
	setBlockNote(s.note, profileui.TDPSliderNote(st))
}

// appendFocus appends this instance's focus item: the slider.
func (s *tdpSliderSection) appendFocus(bd *focusgrid.Builder, items *[]focusItem) {
	if s.scale == nil {
		return
	}
	left, right, get, set := scaleAdjust(s.scale, tdpSliderStep)
	c := bd.Section("tdp").One()
	*items = append(*items, focusItem{
		widget: s.scale, row: c.Row, col: c.Col, section: c.Section,
		editable: true,
		onLeft:   left, onRight: right,
		getValue: get, setValue: set,
	})
}

// focusTDPSliderSection appends the drawer's slider; the dashboard appends its
// own instance's in its own focus list.
func (w *Window) focusTDPSliderSection(b *focusgrid.Builder, items *[]focusItem) {
	if w.tdpSlider != nil {
		w.tdpSlider.appendFocus(b, items)
	}
}

// tdpSliderSections returns every TDP slider that has been built.
func (w *Window) tdpSliderSections() []*tdpSliderSection {
	out := make([]*tdpSliderSection, 0, 2)
	if w.tdpSlider != nil {
		out = append(out, w.tdpSlider)
	}
	if m := w.mainWin; m != nil && m.dashboard != nil && m.dashboard.tdpSlider != nil {
		out = append(out, m.dashboard.tdpSlider)
	}
	return out
}

func (w *Window) syncTDPSlider() {
	for _, s := range w.tdpSliderSections() {
		s.sync()
	}
}
