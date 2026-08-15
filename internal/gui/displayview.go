// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// displayview.go — the DISPLAY card: the refresh rate now, and the rate to
// select on each power source.
//
// The rules (which rates the panel offers at the resolution it is running, how
// they are labelled, which screen a single control acts on, how a stored
// preference resolves against the screen in front of you) live in
// internal/display, where `make test` can reach them. This file is the three
// dropdowns and the calls.
//
// It is the one control in the drawer or the window that does not go through
// the daemon. A refresh rate belongs to the compositor rather than to the
// hardware voltaire owns, and internal/display's package doc has the argument;
// the short version is that the daemon is a systemd user service with no
// guaranteed WAYLAND_DISPLAY, while a GTK client is by definition in the
// session. That is also why the *switching* is here rather than in the
// daemon's autoswitch watcher: the daemon reports that the power source moved,
// and a session client decides what that means for the screen.

import (
	"errors"
	"log/slog"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/display"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// displaySection is the DISPLAY card.
type displaySection struct {
	w *Window

	label *gtk.Label
	dd    *dropdown
	note  *gtk.Label

	// sw turns the rate switching on, and targets holds the two rows it governs
	// — shown only while it is on, exactly as the profile autoswitch block's
	// are. The rows store; they never apply. The Refresh rate row above is how
	// the rate is changed now, and these say what should happen on a transition,
	// the same division the profile targets have.
	sw       *gtk.Switch
	targets  *gtk.Box
	acDD     *dropdown
	battDD   *dropdown
	prefNote *gtk.Label

	// syncing suppresses the switch's state-set handler while sync writes to it.
	// A gtk.Switch fires state-set on a *programmatic* SetActive too, so an
	// unguarded sync would write the config straight back. Local rather than
	// Window.syncing because this card's data comes from the compositor, not
	// from daemon state, so its syncs run outside every w.syncing block.
	syncing bool

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

	// The switch and the two rows it governs, built to match the profile
	// autoswitch block one card over: same control, same labels, same rule that
	// the targets are only shown while the switch is on. That parallel is the
	// point — this is the same idea for a different subsystem, and a user who
	// has met one should not have to work out that the other is it again.
	d.sw = gtk.NewSwitch()
	d.sw.SetHAlign(gtk.AlignStart)
	d.sw.SetHExpand(true)
	w.setHint(d.sw, "Change the refresh rate when the charger is plugged or unplugged")
	d.sw.ConnectStateSet(func(on bool) bool {
		if !d.syncing {
			d.setEnabled(on)
		}
		return false
	})
	if w.gamescope {
		addTouchActivate(d.sw, func() { d.sw.SetActive(!d.sw.Active()) })
	}
	box.Append(formRow("Autoswitch", d.sw))

	d.targets = gtk.NewBox(gtk.OrientationVertical, 4)
	d.targets.Append(subFormRow("On AC", formDropdown(d.buildPrefRow(true, &d.acDD))))
	d.targets.Append(subFormRow("On battery", formDropdown(d.buildPrefRow(false, &d.battDD))))
	d.targets.SetVisible(false)
	box.Append(d.targets)

	// Two notes, and they can never both be showing: PrefNote says nothing
	// until a query has returned rates, which is exactly when the query note is
	// the one with something to say.
	d.prefNote = blockNote()
	box.Append(d.prefNote)

	// Failures land in the flow of the card rather than the error bar: this
	// refreshes whenever the page is shown, and a background read the user did
	// not ask for must not repaint the bar over whatever they were reading.
	// A deliberate *change* does report there — see apply.
	d.note = blockNote()
	box.Append(d.note)

	// Dead until the first query lands. Everything on this card needs the rate
	// list — the switch most of all, since turning it on asks DefaultPrefs for
	// two rates — and the card is built well before Query returns.
	d.setSensitive(false)
	d.syncPrefs()
	return d, box
}

// buildPrefRow creates one power source's preference dropdown.
func (d *displaySection) buildPrefRow(onAC bool, dst **dropdown) *gtk.Button {
	dd := d.w.newDropdown(dropdownConfig{
		options: func() []dropdownOption {
			// Rate, not For: this asks which row the chooser stands on,
			// which is a different question from what would be applied.
			cur := d.w.refreshPrefs.Rate(onAC)
			opts := display.PrefOptions(d.rates)
			rows := make([]dropdownOption, len(opts))
			for i, o := range opts {
				rows[i] = dropdownOption{
					value:    display.FormatPref(o.Hz),
					label:    o.Label,
					selected: o.Hz == cur,
				}
			}
			return rows
		},
		onSelect: func(v string) {
			p := d.w.refreshPrefs
			if onAC {
				p.AC = display.ParsePref(v)
			} else {
				p.Battery = display.ParsePref(v)
			}
			d.w.setRefreshPrefs(p)
			d.syncPrefs()
		},
	})
	d.w.setHint(dd.btn, "Refresh rate to select when this power source becomes active")
	*dst = dd
	return dd.btn
}

// setEnabled acts on the switch. Turning it on fills whichever sides have never
// been set — see display.DefaultPrefs for why that is not the running rate on
// both — so the two rows appear already saying what will happen rather than
// showing "—" and waiting to be discovered. Turning it off keeps the rates, so
// switching back on restores the pair instead of re-guessing it.
func (d *displaySection) setEnabled(on bool) {
	p := d.w.refreshPrefs
	p.Enabled = on
	if on {
		p = p.WithDefaults(d.rates)
	}
	d.w.setRefreshPrefs(p)
	d.syncPrefs()
}

// syncPrefs moves the switch, the two triggers and the note onto the stored
// values. It reads Window.refreshPrefs rather than the config file: the switch
// itself runs whether or not this card was ever built, so the Window owns the
// pair and this is a view of it.
func (d *displaySection) syncPrefs() {
	if d == nil || d.sw == nil {
		return
	}
	d.syncing = true
	defer func() { d.syncing = false }()

	p := d.w.refreshPrefs
	d.sw.SetActive(p.Enabled)
	d.targets.SetVisible(p.Enabled)
	d.acDD.setLabel(display.PrefLabel(p.AC))
	d.battDD.setLabel(display.PrefLabel(p.Battery))
	setBlockNote(d.prefNote, display.PrefNote(p, d.rates))
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
	// Whatever else happens, the preference triggers end up showing what is
	// stored and the note ends up consistent with the rates in hand.
	defer d.syncPrefs()

	if err != nil {
		slog.Debug("display: query failed", "err", err)
		d.rates, d.output = nil, ""
		d.setSensitive(false)
		d.dd.setLabel("—")
		setBlockNote(d.note, "Could not read the display configuration.")
		return
	}

	out, ok := display.Primary(outs)
	if !ok {
		d.rates, d.output = nil, ""
		d.setSensitive(false)
		d.dd.setLabel("—")
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
	d.setSensitive(len(d.rates) > 1)
	switch {
	case len(d.rates) == 0:
		d.dd.setLabel("—")
		setBlockNote(d.note, "This screen reports no modes at its current resolution.")
	default:
		d.dd.setLabel(currentRateLabel(d.rates))
		setBlockNote(d.note, "")
	}
}

// setSensitive moves the whole card together. A screen offering fewer than two
// rates has nothing to choose *now* and nothing to switch to later, so the
// switch and its two rows are as dead as the live control — and a live control
// greying out above controls that did not would read as though they still
// worked. The switch especially: turning it on asks display.DefaultPrefs for
// two rates, and there are none to give it.
func (d *displaySection) setSensitive(on bool) {
	for _, dd := range []*dropdown{d.dd, d.acDD, d.battDD} {
		if dd != nil {
			dd.btn.SetSensitive(on)
		}
	}
	if d.sw != nil {
		d.sw.SetSensitive(on)
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

// appendFocus appends this instance's focus items in the card's reading order:
// the live rate, the switch, then the two target rows — navigable only while
// the switch is on, matching what the pointer can reach. Identical in shape to
// autoswitchSection.appendFocus, because the block is.
//
// Nothing gates any of them on sensitivity: focusItem.visible() already skips an
// insensitive widget, which is what a screen with one mode leaves all four.
func (d *displaySection) appendFocus(b *focusgrid.Builder, items *[]focusItem) {
	if d == nil || d.dd == nil {
		return
	}
	c := b.Section("display").One()
	*items = append(*items, focusItem{
		widget: d.dd.btn, row: c.Row, col: c.Col, section: c.Section,
		onActivate: func() { d.dd.btn.Activate() },
	})

	if d.sw == nil {
		return
	}
	sc := b.One()
	*items = append(*items, focusItem{
		widget: d.sw, row: sc.Row, col: sc.Col, section: sc.Section,
		onActivate: func() { d.sw.SetActive(!d.sw.Active()) },
	})

	vis := boxVisible(d.targets)
	for _, dd := range []*dropdown{d.acDD, d.battDD} {
		if dd == nil {
			continue
		}
		dd := dd
		tc := b.One()
		*items = append(*items, focusItem{
			widget: dd.btn, row: tc.Row, col: tc.Col, section: tc.Section,
			isVisible:  vis,
			onActivate: func() { dd.btn.Activate() },
		})
	}
}

// --- the switch itself -------------------------------------------------
//
// Window-level rather than the card's, because it has to work whether or not
// the card was ever built: the full window is opened by a double press, and a
// user who never opens it still expects the rate they configured to be applied.

// resolveRefreshPrefs reads the stored pair. Like the button preference, an
// unreadable value costs a warning and the default rather than silence — this
// one is written by a control, so a value the parser rejects is either a
// hand-edit or a bug in the UI, and both are worth a line in the journal.
func resolveRefreshPrefs() display.Prefs {
	cfg := theme.LoadAppConfig()
	p := display.Prefs{
		Enabled: display.ParseEnabled(cfg.RefreshAutoswitch),
		AC:      warnUnreadablePref("refresh_ac", cfg.RefreshAC),
		Battery: warnUnreadablePref("refresh_battery", cfg.RefreshBattery),
	}
	if p.Enabled {
		slog.Info("refresh rate follows the power source",
			"on_ac", display.PrefLabel(p.AC), "on_battery", display.PrefLabel(p.Battery))
	}
	return p
}

func warnUnreadablePref(key, raw string) int {
	hz := display.ParsePref(raw)
	if hz == display.PrefUnset && raw != "" {
		slog.Warn("refresh preference not recognised, leaving that source alone",
			"key", key, "value", raw)
	}
	return hz
}

// setRefreshPrefs records the whole trio and persists it.
//
// Whole rather than one field at a time because the switch changes two of the
// three at once (turning it on fills the rates it has never been given), and a
// per-field setter would have had to write the file twice to do it.
//
// It deliberately does not apply anything, even when a rate names the source
// currently running. These rows say what should happen on a *transition*; the
// live row above them is how the rate is changed now, and a control that
// quietly did both would leave the user unable to tell which one they had used.
// The autoswitch profile targets behave the same way.
func (w *Window) setRefreshPrefs(p display.Prefs) {
	w.refreshPrefs = p
	theme.UpdateAppConfig(func(cfg *theme.AppConfig) {
		cfg.RefreshAutoswitch = display.FormatEnabled(p.Enabled)
		cfg.RefreshAC = display.FormatPref(p.AC)
		cfg.RefreshBattery = display.FormatPref(p.Battery)
	})
	slog.Info("refresh rate autoswitch changed", "enabled", p.Enabled,
		"on_ac", display.PrefLabel(p.AC), "on_battery", display.PrefLabel(p.Battery))
}

// refreshForPowerSource acts on a change of power source, and only on a change.
//
// It is edge-triggered for the same reason the daemon's autoswitch watcher is:
// a level-triggered "keep the configured rate applied" would fight the user the
// moment they set a rate by hand, and the manual control is one row above this
// one. State reaches it through refreshState, which the daemon's power-source
// event calls — so the settle window, the UPower nudge and the Mains-only
// reading are all the daemon's, done once, correctly.
//
// The *first* observation latches without acting, and that is the important
// half. The daemon applies its autoswitch decision at startup because a profile
// is state it owns and has to restore; a refresh rate is the compositor's and
// the compositor already persists it across logins. Applying here at startup
// would mean voltaire-gui overriding the session's saved mode every time the
// service restarts, and would make a manual change impossible to keep — the
// user would set 180 on battery and get 60 back at the next login, for reasons
// nothing on screen explains.
func (w *Window) refreshForPowerSource(st *api.State) {
	// Unknown says nothing, so it must not disturb the latch either: on a VM or
	// a desktop the source is never known, and treating that as "battery" is
	// exactly the mistake SourceKnown exists to prevent.
	if st == nil || !st.SourceKnown {
		return
	}
	first := !w.sourceKnown
	if !first && st.OnAC == w.onAC {
		return
	}
	w.onAC, w.sourceKnown = st.OnAC, true
	if first {
		slog.Debug("power source latched", "source", sourceWord(st.OnAC))
		return
	}
	slog.Debug("power source changed", "source", sourceWord(st.OnAC))
	w.applyRefreshFor(st.OnAC)
}

// applyRefreshFor selects the rate configured for a power source.
func (w *Window) applyRefreshFor(onAC bool) {
	hz := w.refreshPrefs.For(onAC)
	if hz == display.PrefUnset {
		return
	}
	go func() {
		outs, err := display.Query()
		if err != nil {
			// Not reported to the error bar: nothing the user just did failed,
			// and the card's own note says so the next time they look at it.
			slog.Info("refresh rate: could not read the display configuration",
				"source", sourceWord(onAC), "err", err)
			return
		}
		out, ok := display.Primary(outs)
		if !ok {
			return
		}
		r, ok := display.Match(display.Rates(out), hz)
		if !ok {
			// The docked case, and deliberately not an error: the preference is
			// still right for the screen it was set on. PrefNote says the same
			// thing in the card.
			slog.Info("refresh rate: this screen has no such mode, leaving it alone",
				"want", display.PrefLabel(hz), "output", out.Name)
			return
		}
		if r.Current {
			slog.Debug("refresh rate already set", "rate", r.Label)
			return
		}
		if err := display.Apply(out.Name, r.ModeID); err != nil {
			// A change the user configured, so this one does report: it is the
			// difference between "voltaire did not do it" and "voltaire cannot".
			w.reportError("Set refresh rate", err)
			return
		}
		slog.Info("refresh rate switched",
			"source", sourceWord(onAC), "rate", r.Label, "output", out.Name)
		glib.IdleAdd(func() bool {
			// Only matters if the card happens to be on screen, which it is
			// whenever someone unplugs while watching the dashboard.
			if m := w.mainWin; m != nil && m.dashboard != nil {
				m.dashboard.dsp.sync()
			}
			return false
		})
	}()
}

func sourceWord(onAC bool) string {
	if onAC {
		return "AC"
	}
	return "battery"
}
