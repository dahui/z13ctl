// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// levels.go — the screen's brightness and the sound volume: one slider type,
// built in the drawer (opt-in on a laptop, in a handheld's device default)
// and on the Dashboard (DISPLAY and SOUND cards).
//
// Neither goes through the daemon. Like the refresh rate, both belong to the
// session: the brightness to KWin on KDE or to logind elsewhere
// (internal/display/brightness.go), the volume to PipeWire
// (internal/audio). Unlike the refresh rate both work under gamescope, where
// logind and PipeWire are as present as on a desktop.
//
// Reads happen on show and after each change, never on the 1 Hz poll: each is
// a subprocess or a D-Bus round trip, for a value that changes only when the
// user changes it. A change made elsewhere (a brightness key, Steam's slider)
// is picked up the next time the drawer or the page is shown.

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/v2/internal/audio"
	"github.com/dahui/voltaire/v2/internal/display"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// levelDebounce settles a drag before writing. Short, because each write is
// cheap and the user is watching the screen dim or listening for the level.
const levelDebounce = 120 * time.Millisecond

// levelConfig is what differs between the two sliders.
type levelConfig struct {
	title   string // form-row label; upper-cased for the drawer's heading
	hint    string
	errOp   string // the error bar's operation name
	section string // focus section
	min     int
	read    func() (pct int, muted bool, err error)
	set     func(pct int) error
	readErr string // the note when the level cannot be read
}

// levelSlider is one built slider.
type levelSlider struct {
	w     *Window
	cfg   levelConfig
	scale *gtk.Scale
	value *gtk.Label
	note  *gtk.Label
	timer *time.Timer

	// quiet suppresses the send while sync moves the handle: the level comes
	// from the session, not daemon state, so w.syncing does not cover it.
	// pending holds sync off mid-drag until the write has landed; gen keeps an
	// earlier write from clearing a later drag's hold; busy keeps one read in
	// flight. Main thread only.
	quiet   bool
	pending bool
	gen     int
	busy    bool
}

func brightnessConfig() levelConfig {
	return levelConfig{
		title:   "Brightness",
		hint:    "The screen's brightness",
		errOp:   "Set brightness",
		section: "brightness",
		min:     display.BrightnessMin,
		read: func() (int, bool, error) {
			v, err := display.ReadBrightness()
			return v, false, err
		},
		set:     display.SetBrightness,
		readErr: "Could not read the screen's brightness.",
	}
}

func volumeConfig() levelConfig {
	return levelConfig{
		title:   "Volume",
		hint:    "The sound volume; moving it unmutes",
		errOp:   "Set volume",
		section: "volume",
		min:     0,
		read: func() (int, bool, error) {
			v, err := audio.Read()
			return v.Percent, v.Muted, err
		},
		set:     audio.Set,
		readErr: "Could not read the sound volume.",
	}
}

// newLevelSlider builds a slider over cfg. desktop is the Dashboard's form
// row; otherwise the drawer's heading with the value at its end, like the TDP
// slider's.
func (w *Window) newLevelSlider(cfg levelConfig, desktop bool) (*levelSlider, *gtk.Box) {
	l := &levelSlider{w: w, cfg: cfg}
	l.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontal, float64(cfg.min), 100, 1)
	l.scale.SetDigits(0)
	l.scale.SetDrawValue(false)
	l.scale.SetHExpand(true)
	l.scale.SetFocusable(false)
	l.scale.SetSensitive(false) // until the first read lands
	w.wheelScrollsView(l.scale)
	w.setHint(l.scale, cfg.hint)
	l.value = formValueLabel("")
	l.note = blockNote()
	l.scale.ConnectValueChanged(func() {
		l.value.SetLabel(fmt.Sprintf("%d%%", int(l.scale.Value())))
		l.queueSend()
	})

	if desktop {
		col := gtk.NewBox(gtk.OrientationVertical, 2)
		col.Append(formSlider(l.scale, l.value))
		col.Append(l.note)
		return l, formRow(cfg.title, col)
	}
	head := gtk.NewBox(gtk.OrientationHorizontal, 4)
	head.Append(sectionLabel(upper(cfg.title)))
	l.value.SetHExpand(true)
	l.value.SetHAlign(gtk.AlignEnd)
	head.Append(l.value)
	box := gtk.NewBox(gtk.OrientationVertical, 4)
	box.Append(head)
	box.Append(l.scale)
	box.Append(l.note)
	return l, box
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// sync re-reads the level, off the GTK thread.
func (l *levelSlider) sync() {
	if l == nil || l.busy {
		return
	}
	l.busy = true
	go func() {
		pct, muted, err := l.cfg.read()
		glib.IdleAdd(func() bool {
			l.busy = false
			if l.pending {
				return false
			}
			if err != nil {
				slog.Debug("level read failed", "level", l.cfg.section, "err", err)
				l.scale.SetSensitive(false)
				l.value.SetLabel("")
				setBlockNote(l.note, l.cfg.readErr)
				return false
			}
			l.quiet = true
			l.scale.SetValue(float64(pct))
			l.quiet = false
			l.scale.SetSensitive(true)
			if muted {
				l.value.SetLabel("Muted")
			} else {
				l.value.SetLabel(fmt.Sprintf("%d%%", pct))
			}
			setBlockNote(l.note, "")
			return false
		})
	}()
}

// queueSend debounces the write, then re-reads every instance of this level
// so the drawer and the Dashboard agree.
func (l *levelSlider) queueSend() {
	if l.quiet {
		return
	}
	l.pending = true
	l.gen++
	if l.timer != nil {
		l.timer.Stop()
	}
	l.timer = time.AfterFunc(levelDebounce, func() {
		glib.IdleAdd(func() bool {
			pct := int(l.scale.Value()) // read on the GTK thread
			gen := l.gen
			go func() {
				err := l.cfg.set(pct)
				glib.IdleAdd(func() bool {
					if l.gen == gen {
						l.pending = false
					}
					if err != nil {
						l.w.reportError(l.cfg.errOp, err)
					} else {
						l.w.clearErrorAsync()
					}
					l.w.syncLevels(l.cfg.section)
					return false
				})
			}()
			return false
		})
	})
}

// appendFocus adds the slider, five percent per D-pad press.
func (l *levelSlider) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	if l == nil {
		return
	}
	left, right, get, set := scaleAdjust(l.scale, 5)
	c := b.Section(l.cfg.section).One()
	*items = append(*items, focusItem{
		widget: l.scale, row: c.Row, col: c.Col, section: c.Section,
		editable: true,
		onLeft:   left, onRight: right,
		getValue: get, setValue: set,
	})
}

// --- the drawer's instances and the walkers ---

func (w *Window) buildBrightnessSection() *gtk.Box {
	l, box := w.newLevelSlider(brightnessConfig(), false)
	w.brightness = l
	return box
}

func (w *Window) buildVolumeSection() *gtk.Box {
	l, box := w.newLevelSlider(volumeConfig(), false)
	w.volume = l
	return box
}

func (w *Window) focusBrightnessSection(b *focusgrid.Builder, items *[]focusItem) {
	w.brightness.appendFocus(b, items) // nil-safe
}

func (w *Window) focusVolumeSection(b *focusgrid.Builder, items *[]focusItem) {
	w.volume.appendFocus(b, items) // nil-safe
}

// levelSliders returns every built slider for one level ("brightness" or
// "volume"), or all of them for "".
func (w *Window) levelSliders(section string) []*levelSlider {
	var out []*levelSlider
	add := func(l *levelSlider) {
		if l != nil && (section == "" || l.cfg.section == section) {
			out = append(out, l)
		}
	}
	add(w.brightness)
	add(w.volume)
	if m := w.mainWin; m != nil && m.dashboard != nil {
		add(m.dashboard.brightness)
		add(m.dashboard.volume)
	}
	return out
}

// syncLevels re-reads one level's sliders on every surface ("" for all).
func (w *Window) syncLevels(section string) {
	for _, l := range w.levelSliders(section) {
		l.sync()
	}
}
