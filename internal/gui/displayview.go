// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// displayview.go — the REFRESH RATE control on the dashboard rail.
//
// The rules (which rates the panel offers at the resolution it is running, how
// they are labelled, which screen a single control acts on) live in
// internal/display, where `make test` can reach them. This file is the dropdown
// and the two calls.
//
// It is the one control in the drawer or the window that does not go through
// the daemon. A refresh rate belongs to the compositor rather than to the
// hardware voltaire owns, and internal/display's package doc has the argument;
// the short version is that the daemon is a systemd user service with no
// guaranteed WAYLAND_DISPLAY, while a GTK client is by definition in the
// session.

import (
	"errors"
	"log/slog"

	"github.com/dahui/voltaire/v2/internal/display"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// displaySection is the REFRESH RATE block.
type displaySection struct {
	w *Window

	label *gtk.Label
	dd    *dropdown
	note  *gtk.Label

	// rates is the last query's answer, which the dropdown's options callback
	// reads. Snapshotted rather than queried on open because opening a list is
	// a click and the query is a DBus round trip through another process.
	rates  []display.Rate
	output string

	// busy keeps one query in flight, on the pattern the dashboard's own
	// refresh uses: the page can be re-synced faster than a subprocess returns.
	busy bool
	gen  int
}

// newDisplaySection creates a REFRESH RATE block, or nil when nothing on this
// machine can report a video mode.
//
// A nil return is how the capability is absent — the same shape as a device
// document's nil section. A control that could only ever fail is worse than no
// control: it says the machine offers something it does not.
func (w *Window) newDisplaySection() (*displaySection, *gtk.Box) {
	if !display.Available() {
		slog.Debug("no display backend; the refresh-rate control is not built")
		return nil, nil
	}

	d := &displaySection{w: w}

	box := gtk.NewBox(gtk.OrientationVertical, 4)

	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.AddCSSClass("btn-group")
	d.dd = w.newDropdown(dropdownConfig{
		options: func() []dropdownOption {
			opts := make([]dropdownOption, 0, len(d.rates))
			for _, r := range d.rates {
				opts = append(opts, dropdownOption{
					value:    r.ModeID,
					label:    r.Label,
					selected: r.Current,
				})
			}
			return opts
		},
		onSelect: func(modeID string) { d.apply(modeID) },
	})
	w.setHint(d.dd.btn, "Refresh rate for this screen")
	d.dd.setLabel("—")
	row.Append(d.dd.btn)

	// The row's own name label is kept so applyQuery can add the output name to
	// it on a multi-screen machine. formRow builds it, so it is fished back out
	// rather than built here — one construction of a form row, not two.
	formed := formRow("Refresh rate", row)
	d.label = firstLabel(formed)
	box.Append(formed)

	// Failures land in the flow of the card rather than the error bar: this
	// refreshes whenever the page is shown, and a background read the user did
	// not ask for must not repaint the bar over whatever they were reading.
	// A deliberate *change* does report there — see apply.
	d.note = blockNote()
	box.Append(d.note)

	return d, box
}

// sync re-reads the screen's modes. Called when the page is shown rather than
// on the 1 Hz poll: this shells out, and a subprocess per second to redraw a
// value that changes when the user changes it would be its own power draw.
func (d *displaySection) sync() {
	if d == nil || d.busy {
		return
	}
	d.busy = true
	d.gen++
	gen := d.gen
	go func() {
		outs, err := display.Query()
		glib.IdleAdd(func() {
			// Cleared on every path, including the failures: leaving it set
			// would stop the control ever refreshing again.
			d.busy = false
			if gen != d.gen {
				return
			}
			d.applyQuery(outs, err)
		})
	}()
}

// applyQuery installs a query result. Split from sync so the decisions are
// reachable without a compositor when this ever grows a test.
func (d *displaySection) applyQuery(outs []display.Output, err error) {
	if err != nil {
		slog.Debug("display: query failed", "err", err)
		d.rates, d.output = nil, ""
		d.dd.setLabel("—")
		d.dd.btn.SetSensitive(false)
		setBlockNote(d.note, "Could not read the display configuration.")
		return
	}

	out, ok := display.Primary(outs)
	if !ok {
		d.rates, d.output = nil, ""
		d.dd.setLabel("—")
		d.dd.btn.SetSensitive(false)
		setBlockNote(d.note, "No screen is currently enabled.")
		return
	}

	d.output = out.Name
	d.rates = display.Rates(out)

	// Name the screen only when there is more than one lit, so the common case
	// is not carrying a connector name nobody needs — and the uncommon one
	// never leaves the user guessing which screen a click will change.
	if d.label != nil {
		title := "Refresh rate"
		if display.EnabledCount(outs) > 1 {
			title += " (" + out.Name + ")"
		}
		d.label.SetText(title)
	}

	// One rate is not a choice. The panel is told what it is running and the
	// control is dead rather than absent, because a card that appears and
	// disappears with a docking cable reads as a bug.
	d.dd.btn.SetSensitive(len(d.rates) > 1)
	switch {
	case len(d.rates) == 0:
		d.dd.setLabel("—")
		setBlockNote(d.note, "This screen reports no modes at its current resolution.")
	default:
		d.dd.setLabel(currentRateLabel(d.rates))
		setBlockNote(d.note, "")
	}
}

// currentRateLabel is the trigger's collapsed text: the rate that is running.
func currentRateLabel(rates []display.Rate) string {
	for _, r := range rates {
		if r.Current {
			return r.Label
		}
	}
	return "—"
}

// apply switches the screen to a mode, then re-reads to show what actually
// happened. The re-read is not belt and braces: the compositor may refuse or
// substitute a mode, and the control has to end up showing the machine's state
// rather than the request.
func (d *displaySection) apply(modeID string) {
	if d.output == "" || modeID == "" {
		return
	}
	out := d.output
	go func() {
		slog.Debug("display: setting mode", "output", out, "mode", modeID)
		if err := display.Apply(out, modeID); err != nil {
			if errors.Is(err, display.ErrNoBackend) {
				// Built means a backend was on PATH; gone by now means it was
				// uninstalled under a running drawer. Say so plainly.
				err = errors.New("no display backend is available")
			}
			d.w.reportError("Set refresh rate", err)
		} else {
			d.w.clearErrorAsync()
		}
		glib.IdleAdd(func() bool {
			d.sync()
			return false
		})
	}()
}

// appendFocus appends this instance's focus item: the rate dropdown.
func (d *displaySection) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	if d == nil || d.dd == nil {
		return
	}
	c := b.Section("display").One()
	*items = append(*items, focusItem{
		widget: d.dd.btn, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { d.dd.btn.Activate() },
	})
}
