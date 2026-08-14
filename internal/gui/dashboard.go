// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// dashboard.go — the telemetry dashboard: one Cairo chart per measured
// quantity, over the daemon's sample history.
//
// Every decision about *what* to draw lives in internal/telemetryplot, which is
// pure and tested: which series exist at all, where each reading sits in the
// window, where the line breaks across a suspend, and what the y-axis spans.
// This file measures the widget, multiplies by the backend scale, and strokes
// the result — the same split fanCurveEditor has with internal/limits.

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/dahui/voltaire/v2/internal/telemetryplot"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// dashboardSpans are the history windows the user can pick between.
//
// The longest is capped by what the daemon actually retains — the device
// document's telemetry.history_seconds, 300 by default — so an option that
// could only ever draw a part-filled chart is not offered. dashboardSpans is
// filtered against that at build time rather than hardcoded, because a device
// declaring an hour of history should be able to show it.
var dashboardSpans = []struct {
	label string
	span  time.Duration
}{
	{"1m", time.Minute},
	{"5m", 5 * time.Minute},
	{"15m", 15 * time.Minute},
	{"1h", time.Hour},
}

// dashboardChartHeight is the unscaled height of one chart. `.dash-chart`
// restates it so gamescope scales it, exactly as `.fan-curve-area` does.
// Sparkline height: the card is a glanceable tile, and the trace's job is
// its shape — nothing in one is dragged or read off precisely.
const dashboardChartHeight = 64

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

	inner.Append(d.buildSpanRow())

	// The card grid. min-width on .dash-card is what drives the reflow: all
	// four tiles in one row at the window's 900px default — the at-a-glance
	// row the dashboard is for — wrapping to two per row at the 560px
	// minimum. The FlowBox does all of it, so no Go code holds a breakpoint.
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

	scroll := newDrawerScroll(inner)
	d.root.Append(scroll)
	d.scroll = scroll

	// The full card set exists before the first byte of history arrives, so
	// the page never opens onto a blank grid.
	d.showPlaceholder()

	d.buildFocusList()
	return d
}

// buildSpanRow builds the history-window selector, offering only spans the
// daemon can actually fill.
func (d *dashboardView) buildSpanRow() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.AddCSSClass("btn-group")
	row.SetMarginTop(6)
	row.SetMarginBottom(4)

	// Right-aligned, natural-width buttons: a compact desktop control above
	// the grid, not the drawer's full-width touch strip. Safe to change here
	// because the dashboard exists only in the full window.
	row.SetHAlign(gtk.AlignEnd)

	retained := d.retention()
	for _, s := range dashboardSpans {
		// Offer a span only if the daemon retains at least most of it. The
		// shortest is always offered, or a device with a very short ring would
		// present no choices at all and the row would be an empty strip.
		if s.span > retained && len(d.spans) > 0 {
			continue
		}
		span := s.span
		btn := gtk.NewButtonWithLabel(s.label)
		btn.ConnectClicked(func() { d.setSpan(span) })
		d.w.setHint(btn, "Show the last "+s.label+" of telemetry")
		row.Append(btn)
		d.spanBtns = append(d.spanBtns, btn)
		d.spans = append(d.spans, span)
	}
	d.syncSpanButtons()
	return row
}

// retention is how much history the daemon keeps, from the capability
// document. A device that declares none falls back to the protocol default
// rather than offering no spans at all.
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
	d.refresh()
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

// startPolling refreshes the chart once a second while the dashboard is the
// visible view. It is separate from startTelemetryPolling, which reads
// get-state for the header's live numbers: this one asks for the *history*,
// which is a different command and a much larger reply, so running it on every
// view would be a per-second few-hundred-sample round trip nobody is looking at.
func (d *dashboardView) startPolling() {
	d.gen++
	gen := d.gen
	// The first fetch fires now, not a second from now: with only the timeout,
	// opening the page cost a full second of placeholder frames that the data
	// was already available to fill.
	d.refresh()
	glib.TimeoutAdd(1000, func() bool {
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
	d.syncBatteryHeader()
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

// buildDashboardFocusList builds the gamepad grid: the back button and the
// span selector. The charts are not navigable — there is nothing to activate
// on one — so this list is fixed and never rebuilt.
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

	items = append(items, d.host.errBar.focusItem())
	d.focusItems = items
	logFocusList(d.host.focusName("dashboard"), items)
}
