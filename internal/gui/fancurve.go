// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// fancurve.go — the 8-point fan curve chart in the custom profile view: its
// coordinate mapping, drawing and pointer handling.
//
// It is a widget rather than a view: the curve model and every constraint rule
// live in internal/limits, and which floor applies is the custom view's
// editorFloorPL1. Everything here is Cairo rather than CSS, which is why it
// carries its own scale() and reads theme colours directly — a CSS provider
// swap does not reach it (see Window.redrawFanCurve).

import (
	"fmt"
	"math"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/limits"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// fanCurveEditor renders and handles interaction for the 8-point fan curve.
// The curve model and its constraint rules live in internal/limits; this type
// owns only the drawing and pointer handling.
type fanCurveEditor struct {
	area     *gtk.DrawingArea
	points   limits.Curve // temp/PWM bounds come from Window.limits
	dragging int          // point index being dragged, -1 if none
	hovered  int          // point index under cursor, -1 if none

	// c is the editor's view, for the target's fan floor, the theme palette
	// and the live APU temperature. Nil only in a curve built without one,
	// which every accessor here tolerates.
	c *customView

	// Chart area within the DrawingArea (set during draw).
	chartX, chartY, chartW, chartH float64
}

// curveString returns the curve in "temp:pwm,temp:pwm,..." format for the API.
func (fc *fanCurveEditor) curveString() string { return fc.points.String() }

// limits returns the device envelope driving the editor's axes and clamping,
// falling back to the defaults when the editor has no parent view.
func (fc *fanCurveEditor) limits() limits.Limits {
	if fc.c == nil {
		return limits.DefaultLimits()
	}
	return fc.c.w.limits
}

// tempRange is the editor's x axis, in Celsius.
func (fc *fanCurveEditor) tempRange() (lo, hi int) {
	l := fc.limits()
	return l.TempMin, l.TempMax
}

// floor is the fan floor curve in force for the editor's target, nil when
// unconstrained or when the editor has no parent view. Like customView's
// fanFloorPWM it reads editorFloorPL1, not the slider position.
func (fc *fanCurveEditor) floor() []api.FanCurvePoint {
	if fc.c == nil {
		return nil
	}
	return fc.limits().ActiveFloor(fc.c.editorFloorPL1)
}

// enforceConstraints repairs the curve after point idx moved. The rules live in
// internal/limits, where they are unit tested.
func (fc *fanCurveEditor) enforceConstraints(idx int) {
	fc.limits().EnforceCurve(&fc.points, idx, fc.floor())
}

// Coordinate mapping.
func (fc *fanCurveEditor) tempToX(temp int) float64 {
	lo, hi := fc.tempRange()
	return fc.chartX + (float64(temp-lo)/float64(hi-lo))*fc.chartW
}
func (fc *fanCurveEditor) pwmToY(pwm int) float64 {
	return fc.chartY + fc.chartH - (float64(pwm)/float64(limits.PWMMax))*fc.chartH // inverted
}
func (fc *fanCurveEditor) xToTemp(x float64) int {
	lo, hi := fc.tempRange()
	t := lo + int(math.Round((x-fc.chartX)/fc.chartW*float64(hi-lo)))
	if t < lo {
		t = lo
	}
	if t > hi {
		t = hi
	}
	return t
}
func (fc *fanCurveEditor) yToPWM(y float64) int {
	p := int(math.Round((fc.chartY + fc.chartH - y) / fc.chartH * float64(limits.PWMMax)))
	if p < limits.PWMMin {
		p = limits.PWMMin
	}
	if p > limits.PWMMax {
		p = limits.PWMMax
	}
	return p
}

// scale returns the factor the drawer's sizes are multiplied by: 1.0 on
// layer-shell, resolution-derived under gamescope. Everything drawn here is
// Cairo rather than CSS, so it has to apply the factor itself — the chart grew
// with .fan-curve-area while the points, fonts and margins stayed at 1x, which
// left the grab targets progressively harder to hit as resolution went up.
func (fc *fanCurveEditor) scale() float64 {
	if fc.c == nil || fc.c.w.backend == nil {
		return 1.0
	}
	if s := fc.c.w.backend.Scale(); s > 0 {
		return s
	}
	return 1.0
}

// rgb returns the theme colour named by hex as Cairo components, falling back to
// the default palette's value when the theme supplied something unparseable —
// drawing the zero value would be black, which vanishes on a dark background.
func (fc *fanCurveEditor) rgb(hex, fallback string) (r, g, b float64) {
	if cr, cg, cb, ok := colorconv.RGB(hex); ok {
		return cr, cg, cb
	}
	if cr, cg, cb, ok := colorconv.RGB(fallback); ok {
		return cr, cg, cb
	}
	return 1, 1, 1
}

// pointRadius is the drawn radius of a curve point, and hitRadius the distance
// within which a press grabs one. The hit radius is deliberately the larger:
// these are dragged with a thumb on a touchscreen.
func (fc *fanCurveEditor) pointRadius() float64 { return 6.0 * fc.scale() }
func (fc *fanCurveEditor) hitRadius() float64   { return 20.0 * fc.scale() }

// hitTest returns the index of the point nearest to (x,y) within tolerance, or -1.
func (fc *fanCurveEditor) hitTest(x, y float64) int {
	tolerance := fc.hitRadius()
	best := -1
	bestDist := tolerance * tolerance
	for i, p := range fc.points {
		px := fc.tempToX(p.Temp)
		py := fc.pwmToY(p.PWM)
		dx := x - px
		dy := y - py
		d := dx*dx + dy*dy
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// draw renders the fan curve chart.
func (fc *fanCurveEditor) draw(cr *cairo.Context, width, height int) {
	w := float64(width)
	h := float64(height)
	s := fc.scale()
	th := theme.DefaultColors
	if fc.c != nil {
		th = fc.c.w.colors
	}
	fontSize := 9 * s

	// Chart margins. Scaled with everything else: the y-axis labels have to fit in
	// leftMargin, and they grow with fontSize.
	leftMargin := 36.0 * s
	bottomMargin := 20.0 * s
	topMargin := 8.0 * s
	rightMargin := 8.0 * s
	fc.chartX = leftMargin
	fc.chartY = topMargin
	fc.chartW = w - leftMargin - rightMargin
	fc.chartH = h - topMargin - bottomMargin

	// Nothing sensible to draw if the widget has not been allocated a usable size
	// yet; the coordinate helpers divide by chartW/chartH.
	if fc.chartW <= 0 || fc.chartH <= 0 {
		return
	}

	// Background.
	cr.SetSourceRGBA(0, 0, 0, 0) // transparent — CSS handles bg
	cr.Paint()

	// Grid lines.
	gr, gg, gb := fc.rgb(th.Border, theme.DefaultColors.Border)
	cr.SetSourceRGBA(gr, gg, gb, 0.6)
	cr.SetLineWidth(0.5 * s)
	// Horizontal: 0%, 25%, 50%, 75%, 100%.
	for _, pct := range []float64{0, 25, 50, 75, 100} {
		y := fc.pwmToY(int(pct / 100.0 * limits.PWMMax))
		cr.MoveTo(fc.chartX, y)
		cr.LineTo(fc.chartX+fc.chartW, y)
	}
	// Vertical: every 10°C across the device's range.
	tLo, tHi := fc.tempRange()
	for temp := tLo; temp <= tHi; temp += 10 {
		x := fc.tempToX(temp)
		cr.MoveTo(x, fc.chartY)
		cr.LineTo(x, fc.chartY+fc.chartH)
	}
	cr.Stroke()

	// Axis labels.
	dr, dg, db := fc.rgb(th.TextDim, theme.DefaultColors.TextDim)
	cr.SetSourceRGBA(dr, dg, db, 1)
	cr.SetFontSize(fontSize)
	// Y-axis labels.
	for _, pct := range []int{0, 25, 50, 75, 100} {
		y := fc.pwmToY(int(float64(pct) / 100.0 * limits.PWMMax))
		cr.MoveTo(2*s, y+3*s)
		cr.ShowText(fmt.Sprintf("%d%%", pct))
	}
	// X-axis labels.
	for temp := tLo + 5; temp <= tHi-5; temp += 20 {
		x := fc.tempToX(temp)
		cr.MoveTo(x-8*s, fc.chartY+fc.chartH+14*s)
		cr.ShowText(fmt.Sprintf("%d°", temp))
	}

	// High-TDP fan floor. While sustained TDP is above the safe max the daemon
	// rejects any point below this line at that point's temperature, and
	// enforceConstraints holds drags at or above it — drawing it explains why
	// the points will not go lower. It is a ramp, not a flat line: the floor
	// bottoms out at 50% for idle temperatures and reaches full speed by 80°C,
	// so the line has to show where each temperature's limit actually is.
	if floor := fc.floor(); len(floor) > 0 {
		// Same @z13-error token as the error bar and .tdp-warning: this line marks
		// a limit the daemon enforces, so it should read as the theme's warning
		// colour rather than a hardcoded red that clashes with light palettes.
		er, eg, eb := fc.rgb(th.Error, theme.DefaultColors.Error)
		cr.SetSourceRGBA(er, eg, eb, 0.9)
		cr.SetLineWidth(1.5 * s)
		cr.SetDash([]float64{6 * s, 3 * s}, 0)
		leftY := fc.pwmToY(limits.FloorPWMAt(floor, tLo))
		cr.MoveTo(fc.chartX, leftY)
		for _, p := range floor {
			// Interior knees only; the edges are evaluated at the axis bounds so
			// the line always spans the full chart, whatever range the floor
			// curve itself covers.
			if p.Temp <= tLo || p.Temp >= tHi {
				continue
			}
			cr.LineTo(fc.tempToX(p.Temp), fc.pwmToY(p.PWM))
		}
		cr.LineTo(fc.chartX+fc.chartW, fc.pwmToY(limits.FloorPWMAt(floor, tHi)))
		cr.Stroke()
		cr.SetDash(nil, 0)
		cr.SetFontSize(fontSize)
		cr.MoveTo(fc.chartX+4*s, leftY-4*s)
		cr.ShowText(fmt.Sprintf("%d–100%% min (TDP > %dW)", pwmPct(floor[0].PWM), fc.limits().TDPMaxSafe))
	}

	// Current APU temperature indicator line, and the operating point on it.
	if fc.c != nil && fc.c.w.state != nil && fc.c.w.state.Temperature > 0 {
		apuTemp := fc.c.w.state.Temperature
		if apuTemp >= tLo && apuTemp <= tHi {
			tx := fc.tempToX(apuTemp)
			tr, tg, tb := fc.rgb(th.Text, theme.DefaultColors.Text)
			cr.SetSourceRGBA(tr, tg, tb, 0.4)
			cr.SetLineWidth(1 * s)
			cr.SetDash([]float64{4 * s, 3 * s}, 0)
			cr.MoveTo(tx, fc.chartY)
			cr.LineTo(tx, fc.chartY+fc.chartH)
			cr.Stroke()
			cr.SetDash(nil, 0)

			// Where that temperature meets the curve: the PWM this curve is
			// asking for right now, with its percentage beside it. The line
			// alone says *where you are*; the dot says what that means, which
			// is the half a curve editor exists to show — reading a duty cycle
			// off a line by eye is exactly the work the chart should be doing.
			//
			// It marks what the *drawn* curve commands, not what the fans are
			// doing: the curve under edit may not be the one in force, and the
			// high-TDP floor (drawn separately above) can raise the effective
			// value. Claiming the latter would need the applied curve, which
			// this widget deliberately does not have.
			pwm := limits.PWMAt(fc.points[:], apuTemp)
			py := fc.pwmToY(pwm)
			cr.SetSourceRGBA(tr, tg, tb, 0.85)
			cr.Arc(tx, py, fc.pointRadius()*0.75, 0, 2*math.Pi)
			cr.Fill()

			// Above the dot, or below it when the curve is near the top of the
			// chart and the label would be clipped.
			label := fmt.Sprintf("%d%%", pwmPct(pwm))
			cr.SetFontSize(fontSize)
			ext := cr.TextExtents(label)
			lx := tx - ext.Width/2
			lx = math.Max(fc.chartX+2*s, math.Min(lx, fc.chartX+fc.chartW-ext.Width-2*s))
			ly := py - 8*s
			if ly-ext.Height < fc.chartY {
				ly = py + 8*s + ext.Height
			}
			cr.MoveTo(lx, ly)
			cr.ShowText(label)
		}
	}

	ar, ag, ab := fc.rgb(th.Accent, theme.DefaultColors.Accent)

	// Filled area under curve.
	cr.SetSourceRGBA(ar, ag, ab, 0.15)
	cr.MoveTo(fc.tempToX(fc.points[0].Temp), fc.pwmToY(0))
	for _, p := range fc.points {
		cr.LineTo(fc.tempToX(p.Temp), fc.pwmToY(p.PWM))
	}
	cr.LineTo(fc.tempToX(fc.points[len(fc.points)-1].Temp), fc.pwmToY(0))
	cr.ClosePath()
	cr.Fill()

	// Line connecting points.
	cr.SetSourceRGBA(ar, ag, ab, 1)
	cr.SetLineWidth(2 * s)
	for i, p := range fc.points {
		x := fc.tempToX(p.Temp)
		y := fc.pwmToY(p.PWM)
		if i == 0 {
			cr.MoveTo(x, y)
		} else {
			cr.LineTo(x, y)
		}
	}
	cr.Stroke()

	// Point circles.
	hr, hg, hb := fc.rgb(th.Text, theme.DefaultColors.Text)
	for i, p := range fc.points {
		x := fc.tempToX(p.Temp)
		y := fc.pwmToY(p.PWM)
		radius := fc.pointRadius()
		if i == fc.dragging || i == fc.hovered {
			radius *= 8.0 / 6.0 // grown while active, in proportion
			// Outer ring.
			cr.SetSourceRGBA(hr, hg, hb, 0.6)
			cr.SetLineWidth(1 * s)
			cr.Arc(x, y, radius+2*s, 0, 2*math.Pi)
			cr.Stroke()
		}
		cr.SetSourceRGBA(ar, ag, ab, 1)
		cr.Arc(x, y, radius, 0, 2*math.Pi)
		cr.Fill()
	}
}

// newFanCurveEditor creates the DrawingArea and sets up input handling.
func (c *customView) newFanCurveEditor() *fanCurveEditor {
	fc := &fanCurveEditor{
		dragging: -1,
		hovered:  -1,
		c:        c,
		points:   c.w.limits.DefaultCurve(),
	}

	fc.area = gtk.NewDrawingArea()
	fc.area.AddCSSClass("fan-curve-area")
	fc.area.SetSizeRequest(-1, 240) // .fan-curve-area scales this under gamescope
	fc.area.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		fc.draw(cr, width, height)
	})

	// Drag gesture for point dragging (CAPTURE phase for gamescope touch).
	drag := gtk.NewGestureDrag()
	drag.SetPropagationPhase(gtk.PhaseCapture)

	var startX, startY float64

	drag.ConnectDragBegin(func(x, y float64) {
		idx := fc.hitTest(x, y)
		if idx < 0 {
			drag.SetState(gtk.EventSequenceDenied)
			return
		}
		fc.dragging = idx
		startX, startY = x, y
		fc.area.QueueDraw()
	})

	drag.ConnectDragUpdate(func(offsetX, offsetY float64) {
		if fc.dragging < 0 {
			return
		}
		x := startX + offsetX
		y := startY + offsetY
		fc.points[fc.dragging].Temp = fc.xToTemp(x)
		fc.points[fc.dragging].PWM = fc.yToPWM(y)
		fc.enforceConstraints(fc.dragging)
		fc.area.QueueDraw()
	})

	drag.ConnectDragEnd(func(_, _ float64) {
		fc.dragging = -1
		fc.area.QueueDraw()
		// The completed drag is the curve's edit boundary for the window's
		// commit button (customcommit.go); a no-op in the drawer.
		c.refreshCommitDirty()
		// A drag off a preset's points must clear its highlight, or the row
		// keeps claiming a curve the user has since changed.
		c.syncPresetHighlight()
	})

	fc.area.AddController(drag)

	// Hover tracking (mouse only).
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, y float64) {
		if fc.dragging >= 0 {
			return
		}
		prev := fc.hovered
		fc.hovered = fc.hitTest(x, y)
		if fc.hovered != prev {
			fc.area.QueueDraw()
		}
	})
	motion.ConnectLeave(func() {
		if fc.hovered >= 0 {
			fc.hovered = -1
			fc.area.QueueDraw()
		}
	})
	fc.area.AddController(motion)

	return fc
}

// buildPresetRow builds the row of named starting-point curves above the
// chart, or (nil, nil) when the device declares none — a device with no
// presets gets no row rather than an empty one, the same capability-by-absence
// rule the rest of the tree follows.
//
// Choosing a preset only loads the editor. Nothing is sent, so the curve is
// visible, draggable and revertible before the commit bar writes it — which is
// what keeps a preset from being an invisible mode the profile remembers. It
// is also why this is the one place in the GUI that may write fc.points
// outside a drag.
func (c *customView) buildPresetRow() (*gtk.Box, []*gtk.Button) {
	presets := c.w.limits.Presets
	if len(presets) == 0 {
		return nil, nil
	}

	row := gtk.NewBox(gtk.OrientationHorizontal, 4)
	row.AddCSSClass("btn-group")
	row.AddCSSClass("preset-row")
	row.SetHomogeneous(true)

	btns := make([]*gtk.Button, 0, len(presets))
	for _, p := range presets {
		btn := gtk.NewButtonWithLabel(p.Label)
		if p.Description != "" {
			c.w.setHint(btn, p.Description)
		}
		name := p.Name
		btn.ConnectClicked(func() { c.applyPreset(name) })
		row.Append(btn)
		btns = append(btns, btn)
	}
	return row, btns
}

// applyPreset loads a preset's points into the editor as an ordinary edit.
func (c *customView) applyPreset(name string) {
	if c.fanCurve == nil {
		return
	}
	pts, ok := c.w.limits.PresetCurve(name)
	if !ok {
		return
	}
	c.fanCurve.points = pts
	// The floor still governs what may be committed, exactly as it does for a
	// dragged curve: a preset below it at the profile's own limit is raised
	// here rather than refused on send. enforceConstraints is the one place
	// that rule lives, so a preset cannot route around it.
	c.fanCurve.enforceConstraints(0)
	c.fanCurve.area.QueueDraw()
	c.syncPresetHighlight()
	c.refreshCommitDirty()
}

// syncPresetHighlight marks the preset the editor's curve currently equals, or
// none when it equals no preset. Equality is exact (limits.PresetMatching): a
// curve one drag away from a preset is a curve the user drew, and labelling it
// otherwise would misreport what the commit button is about to send.
func (c *customView) syncPresetHighlight() {
	if len(c.presetBtns) == 0 || c.fanCurve == nil {
		return
	}
	active := c.w.limits.PresetMatching(c.fanCurve.points)
	for i, btn := range c.presetBtns {
		if i >= len(c.w.limits.Presets) {
			break
		}
		if c.w.limits.Presets[i].Name == active && active != "" {
			btn.AddCSSClass("active")
		} else {
			btn.RemoveCSSClass("active")
		}
	}
}
