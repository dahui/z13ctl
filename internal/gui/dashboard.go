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
	"github.com/dahui/voltaire/v2/internal/telemetryplot"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
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
const dashboardChartHeight = 96

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
	chartBox *gtk.Box
	emptyLbl *gtk.Label

	spanBtns []*gtk.Button
	spans    []time.Duration
	span     time.Duration

	// charts mirrors plot.Groups(); rebuilt only when the plot's Shape changes.
	// Repainting is a QueueDraw on the existing areas, so a 1 Hz refresh does
	// not tear widgets down under the pointer.
	charts []*dashboardChart
	shape  string
	plot   telemetryplot.Plot

	// gen invalidates a refresh in flight, on the pattern startTelemetryPolling
	// established; busy keeps one request outstanding at a time, since an api
	// command carries a 10s deadline and a goroutine per tick against a slow
	// daemon would land replies out of order.
	gen  int
	busy bool
}

// dashboardChart is one chart: the series of a single kind, sharing one axis.
type dashboardChart struct {
	d     *dashboardView
	box   *gtk.Box
	area  *gtk.DrawingArea
	title *gtk.Label
	group telemetryplot.Group
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

	d.chartBox = gtk.NewBox(gtk.OrientationVertical, 0)
	inner.Append(d.chartBox)

	// Shown while there is nothing to draw. Deliberately not empty axes: a
	// chart frame with no line in it reads as "this machine measured nothing",
	// which is a different and wrong claim.
	d.emptyLbl = gtk.NewLabel("")
	d.emptyLbl.AddCSSClass("scale-name")
	d.emptyLbl.SetWrap(true)
	d.emptyLbl.SetXAlign(0)
	d.emptyLbl.SetVisible(false)
	inner.Append(d.emptyLbl)

	scroll := newDrawerScroll(inner)
	d.root.Append(scroll)
	d.scroll = scroll

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
		btn.SetHExpand(true)
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

// showDashboardView switches the drawer to the dashboard, building it on first
// access. A second call returns to the main view.
//
// **Nothing in the drawer calls this.** The dashboard is a reading surface and
// belongs to the full window; the drawer is the controls a user wants close at
// hand, and a chart at 320px is not one of them. The single caller is
// Window.openFull's gamescope branch, where a second toplevel does not
// composite and this is the only surface that session has — so the double press
// still reaches the charts. It goes away when the wrapper-level stack lands and
// gamescope gets a real full window.
func (w *Window) showDashboardView() {
	if w.viewStack == nil {
		return
	}
	w.closePopup()
	if w.viewStack.VisibleChildName() == "dashboard" {
		w.showMainView()
		return
	}
	if w.dashboard == nil {
		w.dashboard = newDashboardView(w, w.drawerHost("dashboard"))
		w.viewStack.AddNamed(w.dashboard.root, "dashboard")
	}
	w.viewStack.SetVisibleChildName("dashboard")
	w.swapFocusList(w.dashboard.focusItems)
	w.dashboard.refresh()
	w.dashboard.startPolling()
}

// startPolling refreshes the chart once a second while the dashboard is the
// visible view. It is separate from startTelemetryPolling, which reads
// get-state for the header's live numbers: this one asks for the *history*,
// which is a different command and a much larger reply, so running it on every
// view would be a per-second few-hundred-sample round trip nobody is looking at.
func (d *dashboardView) startPolling() {
	d.gen++
	gen := d.gen
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
func (d *dashboardView) apply(p telemetryplot.Plot, handled bool, err error) {
	d.plot = p
	if shape := p.Shape(); shape != d.shape {
		d.shape = shape
		d.rebuildCharts()
	}
	for _, c := range d.charts {
		c.sync(p)
	}

	empty := p.Empty()
	d.chartBox.SetVisible(!empty)
	d.emptyLbl.SetVisible(empty)
	if empty {
		d.emptyLbl.SetLabel(emptyDashboardText(handled, err, d.span))
	}
}

// emptyDashboardText says which kind of nothing this is. "No data" would cover
// all three and explain none of them, and two of the three are things the user
// can act on.
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

// rebuildCharts replaces the chart widgets to match the plot's current shape.
func (d *dashboardView) rebuildCharts() {
	for _, c := range d.charts {
		d.chartBox.Remove(c.container())
	}
	d.charts = nil

	for _, g := range d.plot.Groups() {
		c := d.newChart(g)
		d.charts = append(d.charts, c)
		d.chartBox.Append(c.container())
	}
	// The focus list names the span buttons and the back button only — charts
	// are not navigable — so a shape change cannot invalidate it.
}

func (d *dashboardView) newChart(g telemetryplot.Group) *dashboardChart {
	c := &dashboardChart{d: d, group: g}

	c.title = gtk.NewLabel("")
	c.title.SetHAlign(gtk.AlignStart)
	c.title.AddCSSClass("section-label")

	c.area = gtk.NewDrawingArea()
	c.area.AddCSSClass("dash-chart")
	c.area.SetSizeRequest(-1, dashboardChartHeight) // .dash-chart scales this under gamescope
	c.area.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		c.draw(cr, width, height)
	})

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(c.title)
	box.Append(c.area)
	c.box = box
	return c
}

func (c *dashboardChart) container() *gtk.Box { return c.box }

// sync points the chart at the matching group in a new plot and repaints. A
// group that has vanished leaves the chart holding its last data, but that
// cannot be seen: a vanished group changes the shape, so the chart is being
// replaced in the same pass.
func (c *dashboardChart) sync(p telemetryplot.Plot) {
	for _, g := range p.Groups() {
		if g.Kind != c.group.Kind {
			continue
		}
		c.group = g
		c.title.SetLabel(chartTitle(g))
		c.area.QueueDraw()
		return
	}
}

// chartTitle is the heading and the live readout in one line: "APU  52°C", or
// "FAN  1: 2400 · 2: 2600 RPM" when a chart carries more than one series.
func chartTitle(g telemetryplot.Group) string {
	if len(g.Series) == 1 {
		s := g.Series[0]
		return fmt.Sprintf("%s  %s%s", s.Label, formatValue(s.Kind, s.Latest), s.Unit)
	}
	out := ""
	for i, s := range g.Series {
		if i > 0 {
			out += " · "
		}
		out += fmt.Sprintf("%s: %s", s.Label, formatValue(s.Kind, s.Latest))
	}
	return out + " " + g.Unit
}

// formatValue renders a reading. Temperature and RPM are whole numbers on this
// hardware; power is not, and truncating 27.4 W to 27 loses the only digit that
// moves while a load ramps.
func formatValue(kind telemetryplot.Kind, v float64) string {
	if kind == telemetryplot.KindPower {
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.0f", v)
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
	cr.ShowText(formatValue(c.group.Kind, b.Max))
	cr.MoveTo(2*s, chartY+chartH)
	cr.ShowText(formatValue(c.group.Kind, b.Min))

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
// theme's accent; a second takes the dim text colour rather than a hardcoded
// second hue, so a light palette does not get a trace it cannot see. Charts
// never carry more than two series on any device shipping today, and the
// modulo keeps a third from being invisible if one ever does.
func (c *dashboardChart) seriesColor(i int, th theme.Colors) (r, g, b float64) {
	if i%2 == 0 {
		return rgbOr(th.Accent, theme.DefaultColors.Accent)
	}
	return rgbOr(th.Text, theme.DefaultColors.Text)
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
