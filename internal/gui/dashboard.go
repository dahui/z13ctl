// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// dashboard.go — the full window's Dashboard page: a rail of live controls
// beside one Cairo chart per measured quantity, over the daemon's sample
// history.
//
// Every decision about *what* to draw lives in internal/telemetryplot, which is
// pure and tested: which series exist at all, where each reading sits in the
// window, where the line breaks across a suspend, and what the y-axis spans.
// This file measures the widget, multiplies by the backend scale, and strokes
// the result — the same split fanCurveEditor has with internal/limits.
//
// # The controls below the tiles
//
// The page was charts alone until Jeff, 2026-08-14: "our telemetry page is
// actually supposed to be a general use dashboard, not just for monitoring".
// So it carries the live controls too — profile, autoswitch, refresh rate,
// charge limit and RGB — as a second *instance* of each drawer block, never a
// second implementation. Every one of them is now built twice, and the
// Window-level syncs walk both.
//
// **They are laid out across the width, under the tiles, and that was a
// correction.** The first cut put them in a 320px rail down the left, on the
// reasoning that 320 is the drawer's own width so every block would be used at
// the size it was designed for. Jeff: "you stacked the controls vertically
// again... we don't need it to be in the same format as the drawer. Adjust the
// layout so that it takes advantage of the larger window, and keep the
// telemetry tiles on the top." The rail was the drawer transplanted into a
// window, which is exactly what the desktop design pass exists to stop — and
// it pushed the tiles into two thirds of a page they are the reason for.
//
// So: tiles first at full width, then POWER and DISPLAY as a row of cards, then
// the lighting zones as a full-width row of their own — one card per zone, side
// by side, where the drawer switches between them with a pair of tabs. Nothing
// is stacked that has a horizontal arrangement available to it, and nothing is
// a mode switch that has room to be two controls.
//
// What is deliberately *not* here is anything that edits a profile's contents —
// power limits, the fan curve, the undervolt. Those are the Profiles page. The
// dividing line is what the control changes: this page changes what the machine
// is doing now, the editor changes what a saved profile says. Autoswitch moved
// here from the Profiles page on exactly that reading.

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/controls"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/mainwin"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/dahui/voltaire/v2/internal/telemetryplot"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// dashboardChartHeight is the unscaled height of one chart. `.dash-chart`
// restates it so gamescope scales it, exactly as `.fan-curve-area` does.
// Sparkline height: the card is a glanceable tile, and the trace's job is
// its shape — nothing in one is dragged or read off precisely.
//
// Raised from 64 once the controls moved under the tiles rather than beside
// them: the two together end well short of the window's height, and the spare
// pixels are worth more to the trace than to the margin below it. Still a
// sparkline, not a graph — the axis carries two labels and no gridline values.
const dashboardChartHeight = 88

// dashboardView is the telemetry dashboard. It is a view rather than a widget
// tree hanging off Window so that the full window can host the same thing
// without a second implementation — nothing here reads the drawer's stack.
type dashboardView struct {
	w    *Window
	host viewHost

	focusItems []focusItem

	root     *gtk.Box
	scroll   *gtk.ScrolledWindow
	backBtn  *gtk.Button
	grid     *gtk.FlowBox
	emptyLbl *gtk.Label

	// The rail's controls. Each is this surface's instance of a block the
	// drawer also builds; a nil one is a capability this device lacks, and
	// every Window-level walker nil-checks before adding it.
	profiles *profileSection
	autos    *autoswitchSection

	// boostSw is the CPU boost switch, nil on a device that offers none. Its
	// row is in the POWER card beside the profile, because boost is the other
	// control on this page that changes how hard the machine is allowed to
	// work — the charge limit and autoswitch below it are about the battery.
	boostSw *gtk.Switch
	dsp     *displaySection
	battery *batterySection

	// lightings is one RGB block per lighting zone, where the drawer has one
	// block and a pair of zone tabs. Empty on a device with no lighting.
	lightings []*lightingView

	spanBtns []*gtk.Button
	spans    []time.Duration
	span     time.Duration

	// charts mirrors the installed group set — a real plot's Groups(), or the
	// loading placeholder — rebuilt only when the shape key changes.
	// Repainting is a QueueDraw on the existing areas, so a 1 Hz refresh does
	// not tear widgets down under the pointer.
	charts []*dashboardChart
	shape  string

	// batteryStatus is the battery card's header text, from the get-state poll
	// rather than the history reply — see pollTick. Stored so a shape rebuild
	// repopulates the header instead of blanking it until the next poll.
	batteryStatus string

	// gen invalidates a refresh in flight, on the pattern startTelemetryPolling
	// established; busy keeps one request outstanding at a time, since an api
	// command carries a 10s deadline and a goroutine per tick against a slow
	// daemon would land replies out of order.
	gen  int
	busy bool
}

// dashboardChart is one card: the series of a single kind sharing one axis,
// under a header row carrying the kind's name and its live readout.
type dashboardChart struct {
	d        *dashboardView
	cell     *gtk.FlowBoxChild
	area     *gtk.DrawingArea
	titleLbl *gtk.Label
	valueLbl *gtk.Label
	group    telemetryplot.Group
}

// buildDashboardView constructs the view. Called once, lazily, on first
// navigation — the charts inside it are built on the first refresh, because
// which series exist is a property of the data and not of the device document
// (a declared source that never reads is exactly what must not draw a chart).
func newDashboardView(w *Window, host viewHost) *dashboardView {
	d := &dashboardView{w: w, host: host, span: time.Minute}

	d.root = gtk.NewBox(gtk.OrientationVertical, 0)

	// The header is the drawer's chrome. The full window has a tab bar naming
	// the page and no view to go "back" to, so it asks for neither.
	if host.back != nil {
		d.backBtn = gtk.NewButton()
		d.backBtn.SetIconName("go-previous-symbolic")
		d.backBtn.AddCSSClass("view-back-btn")
		d.backBtn.ConnectClicked(host.back)

		header := gtk.NewBox(gtk.OrientationHorizontal, 8)
		header.SetMarginTop(10)
		header.SetMarginBottom(6)
		header.SetMarginStart(14)
		header.Append(d.backBtn)
		title := gtk.NewLabel("Telemetry")
		title.SetHAlign(gtk.AlignStart)
		title.AddCSSClass("drawer-title")
		header.Append(title)
		d.root.Append(header)
	}

	inner := gtk.NewBox(gtk.OrientationVertical, 0)
	inner.SetMarginStart(14)
	inner.SetMarginEnd(14)
	inner.SetMarginBottom(8)

	// The page is three labelled bands: what the machine is doing, what it is
	// set to, and what it looks like (Jeff, 2026-08-14). The span selector
	// rides on the TELEMETRY heading's own line rather than taking a row of its
	// own — it belongs to that band and nothing else, and a heading row with
	// its control at the far end is a line the page would otherwise spend
	// twice.
	inner.Append(dashSectionHeader("TELEMETRY", d.buildSpanRow()))

	// The card grid. min-width on .dash-card is what drives the reflow: four
	// tiles per row across a 1200px window — the at-a-glance grid the dashboard
	// is for — wrapping down as the window narrows. The FlowBox does all of it,
	// so no Go code holds a breakpoint.
	d.grid = gtk.NewFlowBox()
	d.grid.SetSelectionMode(gtk.SelectionNone)
	d.grid.SetHomogeneous(true)
	d.grid.SetMinChildrenPerLine(1)
	d.grid.SetMaxChildrenPerLine(4)
	d.grid.SetRowSpacing(12)
	d.grid.SetColumnSpacing(12)
	inner.Append(d.grid)

	// Shown in place of the grid for the two *actionable* kinds of nothing —
	// daemon not running, daemon too old. Plain "no readings yet" is not shown
	// as text any more: the placeholder frames below are that state's display
	// (Jeff, 2026-08-14 — a bare page for the first second read as the app
	// failing), and an empty framed chart with a "—" readout reads as waiting,
	// not as a measurement of nothing.
	d.emptyLbl = gtk.NewLabel("")
	d.emptyLbl.AddCSSClass("scale-name")
	d.emptyLbl.SetWrap(true)
	d.emptyLbl.SetXAlign(0)
	d.emptyLbl.SetVisible(false)
	inner.Append(d.emptyLbl)

	// The live controls, under the readouts they act on.
	if ctl := d.buildControls(); ctl != nil {
		inner.Append(ctl)
	}

	scroll := newDrawerScroll(inner)
	d.root.Append(scroll)
	d.scroll = scroll

	// The full card set exists before the first byte of history arrives, so
	// the page never opens onto a blank grid.
	d.showPlaceholder()

	d.buildFocusList()
	return d
}

// buildControls builds the live-controls region under the tiles, or nil when
// there is nothing to put in it.
//
// Capabilities are asked of the device document directly rather than read off
// w.controls, the drawer's resolved list. That list is the *quickbar's* — a
// user who trims their drawer down to two sections has said something about the
// panel they open in a hurry, not about a desktop window. The capability check
// is the same one either way, so a machine with no lighting has no RGB card
// here and no RGB section there.
func (d *dashboardView) buildControls() *gtk.Box {
	// The drawer has no dashboard view any more, but the gate is on the host
	// rather than on that fact: this whole region is laid out for a window's
	// width, and the answer should not depend on remembering that.
	if d.host.back != nil {
		return nil
	}

	w := d.w
	has := func(caps ...controls.Capability) bool {
		return controls.SupportsAll(w.device, caps)
	}

	// Two columns of boxed lists — the same two-column frame the Profiles page
	// uses, so the window's pages read as one application rather than two.
	left := gtk.NewBox(gtk.OrientationVertical, 12)
	left.SetVAlign(gtk.AlignStart)
	right := gtk.NewBox(gtk.OrientationVertical, 12)
	right.SetVAlign(gtk.AlignStart)

	var powerRows []gtk.Widgetter
	if has(controls.CapProfiles) {
		// Custom goes to the Profiles tab rather than to a view of its own:
		// this surface already has that page, and two routes to one editor is
		// how they come to disagree about which profile is being edited.
		p, row := w.newProfileSection(true, func() { d.openProfilesTab() })
		d.profiles = p
		powerRows = append(powerRows, row)
	}
	if has(controls.CapBattery) {
		b, row := w.newBatterySection(true)
		d.battery = b
		powerRows = append(powerRows, row)
	}
	// Below the charge limit rather than above it (Jeff, 2026-08-14: "I think
	// it will present better there"). A switch between two rows that are both
	// wide controls reads as an interruption; under them it closes the block,
	// and the eye runs down the two sliders-and-selectors first.
	if has(controls.CapCPUBoost) {
		powerRows = append(powerRows, d.buildBoostRow())
	}
	if has(controls.CapProfiles, controls.CapBattery) {
		// Last in the card: it is the only setting here that acts on its own
		// later, so it reads as a rule applied to the two above it.
		a, box := w.newAutoswitchSection(true)
		d.autos = a
		powerRows = append(powerRows, box)
	}
	if len(powerRows) > 0 {
		left.Append(sectionCard("POWER", powerRows...))
	}

	// The refresh rate is not a device capability — it belongs to the
	// compositor — so it asks its own backend instead of the document. It is the
	// right column's whole content: the live rate plus an autoswitch block of
	// its own, which reads as a column against POWER's rather than the single
	// stranded row it was before that block joined it. It used to sit below
	// LIGHTING for that reason, and LIGHTING has left this row.
	if dsp, row := w.newDisplaySection(); dsp != nil {
		d.dsp = dsp
		right.Append(sectionCard("DISPLAY", row))
	}

	region := gtk.NewBox(gtk.OrientationVertical, 12)

	if left.FirstChild() != nil || right.FirstChild() != nil {
		// SYSTEM rather than POWER: the band holds the refresh rate as well as
		// the profile, the charge limit and boost, and POWER would both
		// under-describe it and repeat the heading of the card directly beneath
		// it. Splitting it into two bands was the alternative and is worse —
		// DISPLAY would be a band heading over a card heading over one row.
		region.Append(dashSectionHeader("SYSTEM", nil))
		columns := gtk.NewBox(gtk.OrientationHorizontal, 12)
		// Equal halves rather than natural widths: the two cards hold different
		// controls, and letting the wider one win would move the label columns
		// out of line with each other — which is the one thing this layout is
		// for.
		columns.SetHomogeneous(true)
		columns.Append(left)
		columns.Append(right)
		region.Append(columns)
	}

	// The lighting zones take the full width below, as their own row: two cards
	// of five form rows each, and each needs the width one half of the window
	// gives it before the six-effect row starts ellipsizing.
	if has(controls.CapLighting) {
		region.Append(dashSectionHeader("RGB", nil))
		region.Append(d.buildLightingRow())
	}

	if region.FirstChild() == nil {
		return nil
	}
	return region
}

// buildBoostRow builds the CPU boost switch.
//
// A switch rather than a pair of buttons, unlike the button preference in
// Settings: this one genuinely has an off state that describes something ("the
// cores are capped at base clock"), where that one had two arrangements and no
// natural negation. The switch also matches the Settings page's rows, which is
// what a reader coming from there expects an on/off hardware setting to look
// like.
func (d *dashboardView) buildBoostRow() *gtk.Box {
	sw := gtk.NewSwitch()
	sw.SetHAlign(gtk.AlignStart)
	sw.SetVAlign(gtk.AlignCenter)
	// Insensitive until a value has been read, exactly as the settings rows
	// are: State.CPUBoost is absent rather than false when the daemon could not
	// read it, and a switch sitting at "off" would be a claim about the CPU.
	sw.SetSensitive(false)
	sw.ConnectStateSet(func(state bool) bool {
		// gtk.Switch fires state-set on a programmatic SetActive too, so an
		// unguarded sync writes every row back to the daemon.
		if !d.w.syncing {
			d.w.sendCPUBoostSet(state)
		}
		return false
	})
	if d.w.gamescope {
		addTouchActivate(sw, func() { sw.SetActive(!sw.Active()) })
	}
	d.boostSw = sw
	return formRow("CPU boost", sw)
}

// syncCPUBoost re-syncs the boost switch wherever it has been built. A
// Window-level walker on the pattern syncBattery and syncLightingSection set,
// so a second surface growing one is an addition rather than a new sync path.
func (w *Window) syncCPUBoost() {
	if m := w.mainWin; m != nil && m.dashboard != nil {
		m.dashboard.syncBoost()
	}
}

// syncBoost moves the switch to the daemon's reading. Absent means the value
// could not be read, which is insensitive rather than off — and an insensitive
// widget is one the gamepad grid skips, so a controller cannot land on a
// control that would fail.
func (d *dashboardView) syncBoost() {
	if d.boostSw == nil {
		return
	}
	st := d.w.state
	if st == nil || st.CPUBoost == nil {
		d.boostSw.SetSensitive(false)
		return
	}
	d.boostSw.SetSensitive(true)
	d.boostSw.SetActive(*st.CPUBoost)
}

// lightingZones is the zone cards this page builds, in display order. The
// heading is the whole labelling: a card named LIGHTBAR holding Effect, two
// colours, Speed and Brightness needs no further explanation, and a "LIGHTING"
// heading over the pair would be a level of nesting to say what both already
// say.
var lightingZones = []struct{ zone, title string }{
	{"keyboard", "KEYBOARD"},
	{"lightbar", "LIGHTBAR"},
}

// buildLightingRow builds one RGB card per lighting zone, side by side.
//
// The drawer has one block and a pair of zone tabs because 320px fits one set
// of controls; a window fits both, and a mode switch is a price paid for space
// that is not scarce here (Jeff, 2026-08-14). Each card is a separate
// lightingView bound to its zone — a second *instance*, which is what the
// per-instance swatch ids and colorInput.owner were made possible by, and the
// reason this is a config field rather than a second widget tree.
func (d *dashboardView) buildLightingRow() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 12)
	row.SetHomogeneous(true)
	for _, z := range lightingZones {
		l := d.w.newLightingSection(lightingConfig{
			// Namespaced per zone as well as per surface: the swatch provider is
			// registered display-wide, so any two blocks sharing a selector
			// repaint each other's squares.
			swatchPrefix: "dash-" + z.zone + "-",
			zone:         z.zone,
			desktop:      true,
		})
		d.lightings = append(d.lightings, l)

		card := sectionCard(z.title, l.blocks()...)
		// Top-aligned so the shorter card does not stretch: an effect of "off"
		// hides that zone's colours, speed and brightness, which is the honest
		// rendering and makes the two cards genuinely different heights.
		card.SetVAlign(gtk.AlignStart)
		row.Append(card)

		l.syncModeVis()
	}
	return row
}

// openProfilesTab follows the rail's Custom button to the editor.
func (d *dashboardView) openProfilesTab() {
	if m := d.w.mainWin; m != nil {
		m.setTab(mainwin.TabProfiles)
	}
}

// syncControls refreshes what the rail cannot learn from daemon state. The
// profile, autoswitch, charge-limit and RGB blocks are all fed by the
// Window-level syncs, which walk every instance; the refresh rate is the one
// control with a source of its own.
func (d *dashboardView) syncControls() {
	d.dsp.sync() // nil-safe: the method guards its own receiver
	d.syncBoost()
}

// dashSectionHeader builds one band heading: the title, an optional control
// aligned to its right, and the rule under both.
//
// .section-group is the drawer's own group heading — the class "TDP AND POWER"
// and "RGB" already use — so a theme styles these without knowing the dashboard
// exists, and gamescope's scaledCSS already carries it. The rule is a plain
// GtkSeparator for the same reason: .drawer separator is themed, so the line
// takes the palette's border colour on every theme including a hand-written
// one.
//
// A band whose content is empty must not call this at all. A heading with a
// rule under it and nothing below is the same trap as a tab onto an empty page:
// it describes something that is not there, and on this page a missing band
// means the device lacks the capability rather than that anything failed.
func dashSectionHeader(title string, trailing gtk.Widgetter) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 2)
	box.SetMarginTop(14)

	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	lbl := gtk.NewLabel(title)
	lbl.AddCSSClass("section-group")
	lbl.SetXAlign(0)
	lbl.SetHExpand(true)
	lbl.SetVAlign(gtk.AlignCenter)
	row.Append(lbl)
	if trailing != nil {
		row.Append(trailing)
	}
	box.Append(row)

	sep := gtk.NewSeparator(gtk.OrientationHorizontal)
	sep.SetMarginBottom(6)
	box.Append(sep)
	return box
}

// buildSpanRow builds the history-window selector, offering only spans the
// daemon can actually fill.
func (d *dashboardView) buildSpanRow() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.AddCSSClass("btn-group")

	// Right-aligned, natural-width buttons: a compact desktop control above
	// the grid, not the drawer's full-width touch strip. Safe to change here
	// because the dashboard exists only in the full window.
	row.SetHAlign(gtk.AlignEnd)

	// Which spans exist, and which of them this device can fill, are both rules
	// in telemetryplot — where make test reaches them. This builds buttons.
	for _, sp := range telemetryplot.Offered(d.retention()) {
		sp := sp
		btn := gtk.NewButtonWithLabel(sp.Label)
		btn.ConnectClicked(func() { d.setSpan(sp.D) })
		d.w.setHint(btn, telemetryplot.SpanHint(sp))
		row.Append(btn)
		d.spanBtns = append(d.spanBtns, btn)
		d.spans = append(d.spans, sp.D)
	}
	d.syncSpanButtons()
	return row
}

// retention is how much history the daemon keeps, from the capability document.
//
// The fallback is deliberately *short* rather than matching the daemon's own
// default. It is reached only when there is no document at all — no daemon, or
// one older than the capability protocol — and in that case offering an hour
// button would promise a window nothing can fill. Under-claiming costs a user
// with a pre-M2 daemon two buttons they can get back by upgrading; over-claiming
// draws a part-filled chart, which is the honesty rule the whole package is
// built on. telemetryplot.Offered always keeps the shortest span, so this can
// never leave an empty strip.
func (d *dashboardView) retention() time.Duration {
	if doc := d.w.device; doc != nil && doc.Telemetry != nil && doc.Telemetry.HistorySeconds > 0 {
		return time.Duration(doc.Telemetry.HistorySeconds) * time.Second
	}
	return 5 * time.Minute
}

func (d *dashboardView) setSpan(span time.Duration) {
	if d.span == span {
		return
	}
	d.span = span
	d.syncSpanButtons()
	// Restart rather than just refresh: the poll interval is a function of the
	// span, so a loop armed for the old one would keep its cadence.
	d.startPolling()
}

func (d *dashboardView) syncSpanButtons() {
	for i, btn := range d.spanBtns {
		if d.spans[i] == d.span {
			btn.AddCSSClass("active")
		} else {
			btn.RemoveCSSClass("active")
		}
	}
}

// startPolling refreshes the chart while the dashboard is the visible view, at
// a cadence set by the span on screen. It is separate from
// startTelemetryPolling, which reads get-state for the header's live numbers:
// this one asks for the *history*, which is a different command and a much
// larger reply, so running it on every view would be a per-second
// few-thousand-sample round trip nobody is looking at.
func (d *dashboardView) startPolling() {
	d.gen++
	gen := d.gen
	// The first fetch fires now, not a second from now: with only the timeout,
	// opening the page cost a full second of placeholder frames that the data
	// was already available to fill.
	d.refresh()
	// Scaled to the window on screen (telemetryplot.RefreshInterval): a minute
	// wants every sample, an hour wants one refresh in twelve, because at that
	// width a dozen readings share a pixel and the other eleven refetch ~1.5 MiB
	// to redraw an identical chart. The card headers' live numbers are not
	// affected — they come from the 1 Hz get-state poll every view runs.
	every := telemetryplot.RefreshInterval(d.span)
	glib.TimeoutAdd(uint(every.Milliseconds()), func() bool {
		if gen != d.gen || !d.host.current() {
			return false
		}
		d.refresh()
		return true
	})
}

// stopPolling ends the refresh loop. Called on every view switch away and on
// hide, so a closed drawer is not asking the daemon for history every second.
func (d *dashboardView) stopPolling() { d.gen++ }

// refresh fetches the history window and repaints.
//
// The fetch runs off the GTK thread — an api command carries a 10s deadline, so
// an inline call would freeze the drawer against a wedged daemon — and comes
// back through IdleAdd. Failures are deliberately silent, on the same grounds
// as the telemetry poll: this is a background refresh the user did not ask for,
// and repainting the error bar every second would overwrite whatever they were
// reading. Their next deliberate action reports it.
func (d *dashboardView) refresh() {
	if d.busy {
		slog.Debug("dashboard: skipping refresh, request still in flight")
		return
	}
	d.busy = true
	gen := d.gen
	seconds := int(d.span.Seconds())
	go func() {
		handled, samples, err := api.SendTelemetryHistory(seconds)
		glib.IdleAdd(func() {
			// Cleared unconditionally, including on the failure paths: leaving
			// it set would stop the dashboard refreshing for good.
			d.busy = false
			if gen != d.gen {
				return
			}
			if err != nil || !handled {
				slog.Debug("dashboard: history unavailable", "handled", handled, "err", err)
				d.apply(telemetryplot.Plot{}, handled, err)
				return
			}
			d.apply(telemetryplot.Build(samples, time.Now(), d.span, 0), true, nil)
		})
	}()
}

// apply installs a new plot, rebuilding the charts only if the shape changed.
//
// A reply with nothing to draw keeps (or restores) the placeholder frames —
// the empty page is drawn as the full card set waiting for data, not as a
// blank grid. Only the two actionable kinds of nothing replace the frames
// with prose, because "start the daemon" is something the user can do and an
// empty frame would hide it.
func (d *dashboardView) apply(p telemetryplot.Plot, handled bool, err error) {
	if p.Empty() {
		if !handled || err != nil {
			d.grid.SetVisible(false)
			d.emptyLbl.SetLabel(emptyDashboardText(handled, err, d.span))
			d.emptyLbl.SetVisible(true)
			return
		}
		// No readings in the window. Reinstall the frames rather than leaving
		// whatever was on screen: switching spans away from data would
		// otherwise keep the previous window's traces under a header claiming
		// this one.
		d.showPlaceholder()
		d.grid.SetVisible(true)
		d.emptyLbl.SetVisible(false)
		return
	}

	d.installGroups(p.Shape(), p.Groups())
	for _, c := range d.charts {
		c.sync(p)
	}
	d.grid.SetVisible(true)
	d.emptyLbl.SetVisible(false)
}

// placeholderShape is the sentinel installGroups keys the loading frames on.
// Never a real plot's shape: those are built from series labels and always
// carry a ':' (TestPlaceholderIsNotAPlotShape pins it from the other side).
const placeholderShape = "placeholder"

// showPlaceholder installs the framed, empty card set — the full set of
// quantities this device is expected to measure, from the capability document
// where there is one; everything when the daemon never answered.
func (d *dashboardView) showPlaceholder() {
	doc := d.w.device
	caps := telemetryplot.PlaceholderCaps{
		Power: true, Battery: true, GPU: true, CPUStats: true, NPU: true, Net: true,
	}
	if doc != nil {
		caps.Battery = doc.Battery != nil
		caps.Power, caps.GPU, caps.CPUStats, caps.NPU, caps.Net = false, false, false, false, false
		if t := doc.Telemetry; t != nil {
			caps.Power = t.PowerDraw != ""
			caps.GPU = t.GPU != ""
			caps.CPUStats = t.CPUStats != ""
			caps.NPU = t.NPU != ""
			caps.Net = t.Net != ""
		}
	}
	kinds := telemetryplot.PlaceholderKinds(caps)
	d.installGroups(placeholderShape, telemetryplot.Placeholder(kinds))
}

// emptyDashboardText says which kind of nothing this is. Only the two
// actionable kinds reach the screen since the loading frames took over the
// third — the default case remains as the fallback for a caller that routes
// here anyway.
//
// Any error is read as a daemon that predates telemetry-history — which answers
// unknown-command — rather than string-matched, on the same grounds as
// probeStoredTarget: an error from a command that should always succeed is, in
// practice, a daemon that cannot serve it. Over-claiming is cheap here in a way
// it would not be on a write path, because the refresh runs every second: a
// transient failure shows this for one tick and is then replaced by the chart.
func emptyDashboardText(handled bool, err error, span time.Duration) string {
	switch {
	case !handled:
		return "voltaire is not running, so there is no telemetry history to show."
	case err != nil:
		return "This daemon does not serve telemetry history — " +
			"restart it after upgrading (systemctl --user restart voltaire)."
	default:
		return fmt.Sprintf("No readings in the last %s yet — the daemon samples once a second.", shortSpan(span))
	}
}

func shortSpan(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// installGroups replaces the card widgets when the shape key changes — a real
// plot's Shape(), or placeholderShape for the loading frames — and is a no-op
// otherwise, so a 1 Hz refresh never tears widgets down under the pointer.
func (d *dashboardView) installGroups(shape string, groups []telemetryplot.Group) {
	if shape == d.shape {
		return
	}
	d.shape = shape

	for _, c := range d.charts {
		d.grid.Remove(c.container())
	}
	d.charts = nil

	for _, g := range groups {
		c := d.newChart(g)
		d.charts = append(d.charts, c)
		d.grid.Insert(c.container(), -1)
	}
	// The battery header's owner is the get-state poll, so a rebuilt card
	// repopulates from the stored text rather than blanking for a tick.
	d.syncBatteryHeader()
	// The focus list names the span buttons and the back button only — charts
	// are not navigable — so a shape change cannot invalidate it.
}

func (d *dashboardView) newChart(g telemetryplot.Group) *dashboardChart {
	c := &dashboardChart{d: d, group: g}

	c.titleLbl = gtk.NewLabel(g.HeaderTitle())
	c.titleLbl.SetHAlign(gtk.AlignStart)
	c.titleLbl.AddCSSClass("dash-card-title")

	// "—" until a reading arrives: the placeholder card must say "waiting",
	// and a blank label beside a framed empty chart says "broken".
	c.valueLbl = gtk.NewLabel("—")
	c.valueLbl.SetHAlign(gtk.AlignEnd)
	c.valueLbl.SetHExpand(true)
	// Bounded, not free-width: the tile's size must win over its text, so a
	// long readout ellipsizes instead of widening its card and reflowing the
	// grid. 34 chars covers the widest three-series header the expanded set
	// produces ("Pkg: 12.8 · GPU: 3.2 · NPU: 0.1 W") at the ~285px a card
	// gets when seven of them share the 1200px window four to a row.
	c.valueLbl.SetEllipsize(pango.EllipsizeEnd)
	c.valueLbl.SetMaxWidthChars(34)
	c.valueLbl.AddCSSClass("dash-card-value")

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.Append(c.titleLbl)
	header.Append(c.valueLbl)

	c.area = gtk.NewDrawingArea()
	c.area.AddCSSClass("dash-chart")
	c.area.SetSizeRequest(-1, dashboardChartHeight) // .dash-chart scales this under gamescope
	// Never VExpand: GTK4 propagates expand flags upward, so an expanding
	// chart makes the cell, the FlowBox and the page all expand — the single
	// tile row then stretches to the full page height and the "sparkline" is
	// 500px tall. Seen on hardware; the tile's height is its natural height.
	c.area.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		c.draw(cr, width, height)
	})

	card := gtk.NewBox(gtk.OrientationVertical, 6)
	card.AddCSSClass("dash-card")
	card.Append(header)
	card.Append(c.area)

	// Not focusable: there is nothing to activate on a card, and the gamepad
	// grid deliberately skips the charts for the same reason.
	c.cell = gtk.NewFlowBoxChild()
	c.cell.SetFocusable(false)
	c.cell.SetChild(card)
	return c
}

func (c *dashboardChart) container() *gtk.FlowBoxChild { return c.cell }

// sync points the card at the matching group in a new plot and repaints. A
// group that has vanished leaves the card holding its last data, but that
// cannot be seen: a vanished group changes the shape, so the card is being
// replaced in the same pass.
func (c *dashboardChart) sync(p telemetryplot.Plot) {
	for _, g := range p.Groups() {
		if g.Kind != c.group.Kind {
			continue
		}
		c.group = g
		c.titleLbl.SetLabel(g.HeaderTitle())
		if g.Kind != telemetryplot.KindBattery {
			c.valueLbl.SetLabel(g.HeaderValue())
		}
		c.area.QueueDraw()
		return
	}
}

// pollTick consumes the drawer-wide get-state poll while the dashboard is
// current. The battery card is the one header fed from get-state rather than
// the history reply: its state word and wattage arrive in one reply, so the
// word can never contradict the number beside it — and the daemon already
// serves the sampler's own most recent figure there, so the header and the
// chart's right-hand edge agree by construction.
func (d *dashboardView) pollTick(st *api.State) {
	d.batteryStatus = profileui.BatteryStatus(st)
	// Which input is supplying the machine, appended rather than folded into
	// BatteryStatus: that line drops its own state word to stay legible, so it
	// has no room, and the charger is a property of the *supply* rather than of
	// what the pack is doing. Empty on battery and on any device with one way
	// to take power, which is most of them.
	if c := profileui.ChargerLabel(st); c != "" {
		d.batteryStatus = joinHeader(d.batteryStatus, c)
	}
	d.syncBatteryHeader()
}

// joinHeader joins two header fragments with the card headers' separator,
// tolerating an empty left half.
func joinHeader(a, b string) string {
	if a == "" {
		return b
	}
	return a + " · " + b
}

// syncBatteryHeader writes the stored battery status to the battery card's
// header, or a placeholder while nothing can honestly be claimed.
func (d *dashboardView) syncBatteryHeader() {
	for _, c := range d.charts {
		if c.group.Kind != telemetryplot.KindBattery {
			continue
		}
		if d.batteryStatus == "" {
			c.valueLbl.SetLabel("—")
		} else {
			c.valueLbl.SetLabel(d.batteryStatus)
		}
		return
	}
}

// draw strokes one chart.
func (c *dashboardChart) draw(cr *cairo.Context, width, height int) {
	w, h := float64(width), float64(height)
	s := c.d.scale()
	th := c.d.colors()
	fontSize := 9 * s

	leftMargin := 34.0 * s
	rightMargin := 6.0 * s
	topMargin := 6.0 * s
	bottomMargin := 6.0 * s

	chartX, chartY := leftMargin, topMargin
	chartW := w - leftMargin - rightMargin
	chartH := h - topMargin - bottomMargin
	if chartW <= 0 || chartH <= 0 {
		// Not allocated a usable size yet; every mapping below divides by these.
		return
	}

	b := c.group.Bounds
	toY := func(v float64) float64 {
		return chartY + chartH - ((v-b.Min)/b.Span())*chartH
	}

	cr.SetSourceRGBA(0, 0, 0, 0) // transparent — CSS paints the background
	cr.Paint()

	// Grid: four horizontal rules across the axis, and the axis labels for the
	// top and bottom. Intermediate labels are omitted deliberately — at 96px
	// tall with a 9px font they collide, and the chart's job is the shape of the
	// trace rather than reading values off it.
	gr, gg, gb := rgbOr(th.Border, theme.DefaultColors.Border)
	cr.SetSourceRGBA(gr, gg, gb, 0.6)
	cr.SetLineWidth(0.5 * s)
	for i := range 5 {
		y := chartY + chartH*float64(i)/4
		cr.MoveTo(chartX, y)
		cr.LineTo(chartX+chartW, y)
	}
	cr.Stroke()

	dr, dg, db := rgbOr(th.TextDim, theme.DefaultColors.TextDim)
	cr.SetSourceRGBA(dr, dg, db, 1)
	cr.SetFontSize(fontSize)
	cr.MoveTo(2*s, chartY+fontSize)
	cr.ShowText(telemetryplot.FormatValue(c.group.Kind, b.Max))
	cr.MoveTo(2*s, chartY+chartH)
	cr.ShowText(telemetryplot.FormatValue(c.group.Kind, b.Min))

	// The traces. Series of a group share the axis, so their heights are
	// directly comparable — which is the only reason to draw two fans together.
	for i, series := range c.group.Series {
		r, g, bl := c.seriesColor(i, th)
		cr.SetSourceRGBA(r, g, bl, 1)
		cr.SetLineWidth(1.5 * s)
		for _, seg := range series.Segments {
			// A single point has no line to draw; a dot keeps it visible rather
			// than silently dropping a reading that survived a gap alone.
			if len(seg) == 1 {
				cr.Arc(chartX+seg[0].X*chartW, toY(seg[0].V), 1.5*s, 0, 2*math.Pi)
				cr.Fill()
				continue
			}
			for j, pt := range seg {
				x, y := chartX+pt.X*chartW, toY(pt.V)
				if j == 0 {
					cr.MoveTo(x, y)
					continue
				}
				cr.LineTo(x, y)
			}
			cr.Stroke()
		}
	}
}

// seriesColor picks a trace colour. The first series of a chart takes the
// theme's accent, the second the text colour, the third the dim text colour —
// theme-derived rather than hardcoded hues, so a light palette never gets a
// trace it cannot see. Three covers every chart the expanded set draws
// (power, load and clocks each carry three); the modulo keeps a fourth from
// being invisible if a device ever ships one.
func (c *dashboardChart) seriesColor(i int, th theme.Colors) (r, g, b float64) {
	switch i % 3 {
	case 0:
		return rgbOr(th.Accent, theme.DefaultColors.Accent)
	case 1:
		return rgbOr(th.Text, theme.DefaultColors.Text)
	default:
		return rgbOr(th.TextDim, theme.DefaultColors.TextDim)
	}
}

// scale is the backend's CSS scale factor. Cairo is painted rather than styled,
// so it has to apply the factor by hand or it stays at 1x while everything
// around it grows — the same rule fanCurveEditor follows.
func (d *dashboardView) scale() float64 {
	if d.w == nil || d.w.backend == nil {
		return 1.0
	}
	if s := d.w.backend.Scale(); s > 0 {
		return s
	}
	return 1.0
}

func (d *dashboardView) colors() theme.Colors {
	if d.w == nil {
		return theme.DefaultColors
	}
	return d.w.colors
}

// rgbOr resolves a theme hex to Cairo components, falling back twice so a
// malformed custom theme cannot produce an invisible chart.
func rgbOr(hex, fallback string) (r, g, b float64) {
	if cr, cg, cb, ok := colorconv.RGB(hex); ok {
		return cr, cg, cb
	}
	if cr, cg, cb, ok := colorconv.RGB(fallback); ok {
		return cr, cg, cb
	}
	return 1, 1, 1
}

// buildFocusList builds the gamepad grid: the back button, the span selector,
// then the rail's controls. The charts are not navigable — there is nothing to
// activate on one — and no rail block changes shape, so this list is fixed and
// never rebuilt.
//
// The span row comes before the rail because it is the topmost thing on the
// page, which is the order every other focus list is in. Whichever way round,
// the bumpers jump between the sections.
func (d *dashboardView) buildFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	if d.backBtn != nil {
		c := b.Section("nav").One()
		items = append(items, focusItem{
			widget: d.backBtn, row: c.Row, col: c.Col, section: c.Section,
			onActivate: d.host.back,
		})
	}

	b.Section("span")
	for i, coord := range b.Line(len(d.spanBtns)) {
		btn := d.spanBtns[i]
		span := d.spans[i]
		items = append(items, focusItem{
			widget: btn, row: coord.Row, col: coord.Col, section: coord.Section,
			onActivate: func() { d.setSpan(span) },
		})
	}

	// Each block appends its own items and names its own section, so the rail's
	// gamepad order is its visual order by construction — the property
	// controlBuilder exists to give the drawer, reached here by building both
	// halves from the same nil-checked instance.
	// In the POWER card's own reading order. It was profile → autoswitch →
	// battery for as long as this page has existed, while the card has always
	// shown profile → charge limit → autoswitch: D-pad down from Profile landed
	// on the autoswitch switch and skipped the charge limit entirely, and
	// nothing on screen looked wrong. Same class as the profile row's Line(3)
	// and the settings buttons' One()-each — a coordinate that disagrees with
	// the widgets is the failure nobody notices with a mouse in their hand, and
	// this one predated all of them.
	if d.profiles != nil {
		d.profiles.appendFocus(b, &items)
	}
	if d.battery != nil {
		d.battery.appendFocus(b, &items)
	}
	if d.boostSw != nil {
		c := b.Section("boost").One()
		sw := d.boostSw
		items = append(items, focusItem{
			widget: sw, row: c.Row, col: c.Col, section: c.Section,
			onActivate: func() { sw.SetActive(!sw.Active()) },
		})
	}
	if d.autos != nil {
		d.autos.appendFocus(b, &items)
	}
	d.dsp.appendFocus(b, &items) // nil-safe
	for _, l := range d.lightings {
		l.appendFocus(b, &items)
	}

	items = append(items, d.host.errBar.focusItem())
	d.focusItems = items
	logFocusList(d.host.focusName("dashboard"), items)
}
