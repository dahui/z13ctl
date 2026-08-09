// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// tdp.go — Custom profile view: TDP sliders, fan curve editor, telemetry.

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/colorconv"
	"github.com/dahui/voltaire/v2/internal/limits"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/dahui/voltaire/v2/internal/theme"
	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
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
	w        *Window      // parent for theme colors + telemetry

	// Chart area within the DrawingArea (set during draw).
	chartX, chartY, chartW, chartH float64
}

// curveString returns the curve in "temp:pwm,temp:pwm,..." format for the API.
func (fc *fanCurveEditor) curveString() string { return fc.points.String() }

// limits returns the device envelope driving the editor's axes and clamping,
// falling back to the defaults when the editor has no parent window.
func (fc *fanCurveEditor) limits() limits.Limits {
	if fc.w == nil {
		return limits.DefaultLimits()
	}
	return fc.w.limits
}

// tempRange is the editor's x axis, in Celsius.
func (fc *fanCurveEditor) tempRange() (lo, hi int) {
	l := fc.limits()
	return l.TempMin, l.TempMax
}

// fanFloorPWM returns the minimum fan PWM the daemon will accept for the
// editor's current target.
//
// Derived from editorFloorPL1 rather than the slider position: the daemon
// validates against the applied limit (live target) or the profile's own
// saved TDP (stored target), and a slider the user has moved but not saved is
// neither. Must be called from the GTK main thread.
func (w *Window) fanFloorPWM() int {
	return w.limits.FanFloorPWM(w.editorFloorPL1)
}

// floor is the fan floor curve in force for the editor's target, nil when
// unconstrained or when the editor has no parent window. Like fanFloorPWM it
// reads editorFloorPL1, not the slider position.
func (fc *fanCurveEditor) floor() []api.FanCurvePoint {
	if fc.w == nil {
		return nil
	}
	return fc.limits().ActiveFloor(fc.w.editorFloorPL1)
}

// editPlan resolves how the custom view must address its target right now.
// Computed fresh per operation rather than stored: the answer changes
// underneath an open editor when the active profile moves (autoswitch, the
// CLI, another client). Must be called from the GTK main thread; the result
// is a value, safe to hand to a goroutine.
func (w *Window) editPlan() profileui.EditPlan {
	target := w.editProfile
	if target == "" {
		target = api.DefaultCustomProfile
	}
	return profileui.PlanEdit(w.state, target)
}

// updateEditorFloor recomputes the floor limit for the editor's target from
// fresh state. For a live target the applied PL1 can move underneath the
// editor (the CLI, another client); a stored target's own TDP only moves
// through this editor, but recomputing costs nothing.
func (w *Window) updateEditorFloor() {
	w.editorFloorPL1 = profileui.ForEditor(w.state, w.editPlan()).FloorPL1
}

// pwmPct renders a PWM value as a rounded percentage for display. Plain integer
// division reads 127 (the 50% floor) as "49%".
func pwmPct(pwm int) int {
	return (pwm*100 + limits.PWMMax/2) / limits.PWMMax
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
	if fc.w == nil || fc.w.backend == nil {
		return 1.0
	}
	if s := fc.w.backend.Scale(); s > 0 {
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
	if fc.w != nil {
		th = fc.w.colors
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

	// Current APU temperature indicator line.
	if fc.w != nil && fc.w.state != nil && fc.w.state.Temperature > 0 {
		apuTemp := fc.w.state.Temperature
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
func (w *Window) newFanCurveEditor() *fanCurveEditor {
	fc := &fanCurveEditor{
		dragging: -1,
		hovered:  -1,
		w:        w,
		points:   w.limits.DefaultCurve(),
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

// buildCustomView builds the custom TDP/fan curve view.
func (w *Window) buildCustomView() *gtk.Box {
	view := gtk.NewBox(gtk.OrientationVertical, 0)

	// Header: back button + title.
	w.customBackBtn = gtk.NewButton()
	w.customBackBtn.SetIconName("go-previous-symbolic")
	w.customBackBtn.AddCSSClass("view-back-btn")
	w.customBackBtn.ConnectClicked(func() { w.showMainView() })

	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	header.SetMarginTop(10)
	header.SetMarginBottom(6)
	header.SetMarginStart(14)
	header.Append(w.customBackBtn)
	lbl := gtk.NewLabel("Custom Profiles")
	lbl.SetHAlign(gtk.AlignStart)
	lbl.AddCSSClass("drawer-title")
	header.Append(lbl)
	view.Append(header)

	// Shown only for a stored edit — a target that is not running — where
	// nothing on this view touches hardware. Without it, Save doing nothing
	// observable reads as a dead button.
	w.editorNote = gtk.NewLabel("")
	w.editorNote.SetWrap(true)
	w.editorNote.SetHAlign(gtk.AlignStart)
	w.editorNote.AddCSSClass("scale-value")
	w.editorNote.SetMarginStart(14)
	w.editorNote.SetMarginEnd(14)
	w.editorNote.SetVisible(false)
	view.Append(w.editorNote)

	content := gtk.NewBox(gtk.OrientationVertical, 8)
	content.SetMarginTop(4)
	content.SetMarginBottom(12)
	content.SetMarginStart(12)
	content.SetMarginEnd(12)

	// --- PROFILE SELECTOR ---
	// The custom profiles live here rather than in the main view; everything
	// below edits whichever one this selects.
	content.Append(w.buildProfileSelector())
	content.Append(separator())

	// --- TELEMETRY ---
	content.Append(sectionLabel("TELEMETRY"))
	telRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	w.telemetryTempLabel = gtk.NewLabel("APU: --°C")
	w.telemetryTempLabel.SetHAlign(gtk.AlignStart)
	w.telemetryTempLabel.AddCSSClass("section-label")
	w.telemetryFanLabel = gtk.NewLabel("Fan: -- RPM")
	w.telemetryFanLabel.SetHAlign(gtk.AlignEnd)
	w.telemetryFanLabel.SetHExpand(true)
	w.telemetryFanLabel.AddCSSClass("section-label")
	telRow.Append(w.telemetryTempLabel)
	telRow.Append(w.telemetryFanLabel)
	content.Append(telRow)

	// --- TDP ---
	content.Append(sectionLabel("TDP"))

	// Advanced checkbox — placed above sliders so toggle swaps content in-place.
	w.tdpAdvancedCheck = gtk.NewCheckButtonWithLabel("Advanced")
	w.tdpAdvancedCheck.AddCSSClass("advanced-check")
	if w.gamescope {
		addTouchActivate(w.tdpAdvancedCheck, func() { w.tdpAdvancedCheck.SetActive(!w.tdpAdvancedCheck.Active()) })
	}
	content.Append(w.tdpAdvancedCheck)

	// Basic TDP box (visible by default).
	tdpBasicBox := gtk.NewBox(gtk.OrientationVertical, 4)
	w.tdpBasicScale = gtk.NewScaleWithRange(gtk.OrientationHorizontal, float64(w.limits.TDPMin), float64(w.limits.BasicSliderMax()), 1)
	w.tdpBasicScale.SetDigits(0)
	w.tdpBasicScale.SetDrawValue(false)
	w.tdpBasicScale.SetValue(float64(50))
	w.tdpBasicScale.SetFocusable(false)
	w.wheelScrollsView(w.tdpBasicScale)
	w.tdpBasicLabel = gtk.NewLabel("50 W")
	w.tdpBasicLabel.AddCSSClass("scale-value")
	w.tdpBasicScale.ConnectValueChanged(func() {
		w.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", int(w.tdpBasicScale.Value())))
	})
	tdpBasicBox.Append(w.tdpBasicScale)
	tdpBasicBox.Append(w.tdpBasicLabel)
	content.Append(tdpBasicBox)

	// Advanced box (hidden by default) — replaces basic slider in-place.
	w.tdpAdvancedBox = gtk.NewBox(gtk.OrientationVertical, 4)
	w.tdpAdvancedBox.SetVisible(false)

	w.tdpWarningLabel = gtk.NewLabel(fmt.Sprintf(
		"WARNING: Values above %dW may cause thermal throttling, instability, or hardware damage. Use at your own risk — we are not responsible for any damages.",
		w.limits.TDPMaxSafe))
	w.tdpWarningLabel.SetWrap(true)
	w.tdpWarningLabel.SetHAlign(gtk.AlignStart)
	w.tdpWarningLabel.AddCSSClass("tdp-warning")
	w.tdpAdvancedBox.Append(w.tdpWarningLabel)

	w.tdpPL1Scale, w.tdpPL1Label = w.buildTdpScale("PL1 (SPL)", "Sustained power limit — the long-term average power the CPU targets.")
	w.tdpPL2Scale, w.tdpPL2Label = w.buildTdpScale("PL2 (SPPT)", "Short boost — maximum power during brief burst workloads.")
	w.tdpPL3Scale, w.tdpPL3Label = w.buildTdpScale("PL3 (FPPT)", "Fast boost — peak instantaneous power for single-threaded spikes.")

	// --- UNDERVOLT (inside advanced box) ---
	w.uvBox = gtk.NewBox(gtk.OrientationVertical, 4)
	// Hidden by default; syncCustomView shows it when UndervoltAvailable.
	w.uvBox.SetVisible(false)

	w.uvBox.Append(sectionLabel("UNDERVOLT"))

	uvWarn := gtk.NewLabel("Undervolt offsets are only active while the Custom profile is selected. Switching to a stock profile resets them to 0. Unstable values may cause crashes.")
	uvWarn.SetWrap(true)
	uvWarn.SetHAlign(gtk.AlignStart)
	uvWarn.AddCSSClass("tdp-warning")
	w.uvBox.Append(uvWarn)

	w.uvCpuScale, w.uvCpuLabel = w.buildUvScale("CPU Curve Optimizer", -40, 0)

	// UV buttons: Save UV | Reset UV
	uvBtnRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	uvBtnRow.AddCSSClass("custom-actions")

	w.saveUvBtn = gtk.NewButtonWithLabel("Save UV")
	w.saveUvBtn.AddCSSClass("save-btn")
	w.saveUvBtn.SetHExpand(true)
	w.saveUvBtn.ConnectClicked(func() { w.saveUndervolt() })
	uvBtnRow.Append(w.saveUvBtn)

	w.resetUvBtn = gtk.NewButtonWithLabel("Reset UV")
	w.resetUvBtn.SetHExpand(true)
	w.resetUvBtn.ConnectClicked(func() { w.resetUndervolt() })
	uvBtnRow.Append(w.resetUvBtn)

	w.uvBox.Append(uvBtnRow)
	w.tdpAdvancedBox.Append(w.uvBox)

	content.Append(w.tdpAdvancedBox)

	w.tdpAdvancedCheck.ConnectToggled(func() {
		adv := w.tdpAdvancedCheck.Active()
		w.tdpAdvancedBox.SetVisible(adv)
		tdpBasicBox.SetVisible(!adv)
	})

	content.Append(separator())

	// --- FAN CURVE ---
	content.Append(sectionLabel("FAN CURVE"))
	w.fanCurve = w.newFanCurveEditor()
	content.Append(w.fanCurve.area)

	content.Append(separator())

	// --- BUTTONS ---
	// Save row: Save TDP | Save Fans | Save Both
	saveRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	saveRow.AddCSSClass("custom-actions")

	w.saveTdpBtn = gtk.NewButtonWithLabel("Save TDP")
	w.saveTdpBtn.AddCSSClass("save-btn")
	w.saveTdpBtn.SetHExpand(true)
	w.saveTdpBtn.ConnectClicked(func() { w.saveCustomTdp() })
	saveRow.Append(w.saveTdpBtn)

	w.saveFanBtn = gtk.NewButtonWithLabel("Save Fans")
	w.saveFanBtn.AddCSSClass("save-btn")
	w.saveFanBtn.SetHExpand(true)
	w.saveFanBtn.ConnectClicked(func() { w.saveCustomFanCurve() })
	saveRow.Append(w.saveFanBtn)

	w.saveBothBtn = gtk.NewButtonWithLabel("Save Both")
	w.saveBothBtn.AddCSSClass("save-btn")
	w.saveBothBtn.SetHExpand(true)
	w.saveBothBtn.ConnectClicked(func() { w.saveCustomBoth() })
	saveRow.Append(w.saveBothBtn)

	content.Append(saveRow)

	// Reset row: Reset TDP | Reset Fans
	resetRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	resetRow.AddCSSClass("custom-actions")

	w.resetTdpBtn = gtk.NewButtonWithLabel("Reset TDP")
	w.resetTdpBtn.SetHExpand(true)
	w.resetTdpBtn.ConnectClicked(func() { w.resetTdp() })
	resetRow.Append(w.resetTdpBtn)

	w.resetFanBtn = gtk.NewButtonWithLabel("Reset Fans")
	w.resetFanBtn.SetHExpand(true)
	w.resetFanBtn.ConnectClicked(func() { w.resetFanCurve() })
	resetRow.Append(w.resetFanBtn)

	content.Append(resetRow)

	// --- DELETE ---
	// Lives in the editor rather than on the profile row: the editor knows its
	// target, and a delete affordance on every row is an accidental tap away
	// from data loss. Two taps stand in for a confirm dialog (no popovers —
	// they do not composite under gamescope); sensitivity mirrors the daemon's
	// refusals via profileui.DeleteBlockFor.
	content.Append(separator())
	w.deleteBtn = gtk.NewButtonWithLabel("Delete Profile")
	w.deleteBtn.ConnectClicked(func() { w.deleteProfileClicked() })
	content.Append(w.deleteBtn)

	scroll := newDrawerScroll(content)
	w.customScroll = scroll
	view.Append(scroll)

	return view
}

// buildTdpScale creates a labeled TDP slider (5–93W) and appends it to the
// advanced box. Returns the scale and value label.
func (w *Window) buildTdpScale(label, desc string) (*gtk.Scale, *gtk.Label) {
	nameLabel := gtk.NewLabel(label)
	nameLabel.SetHAlign(gtk.AlignStart)
	nameLabel.AddCSSClass("scale-name")
	w.tdpAdvancedBox.Append(nameLabel)
	descLabel := gtk.NewLabel(desc)
	descLabel.SetHAlign(gtk.AlignStart)
	descLabel.SetWrap(true)
	descLabel.AddCSSClass("scale-value")
	w.tdpAdvancedBox.Append(descLabel)
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, float64(w.limits.TDPMin), float64(w.limits.TDPMaxForced), 1)
	sc.SetDigits(0)
	sc.SetDrawValue(false)
	sc.SetValue(50)
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	valLabel := gtk.NewLabel("50 W")
	valLabel.AddCSSClass("scale-value")
	sc.ConnectValueChanged(func() {
		valLabel.SetLabel(fmt.Sprintf("%d W", int(sc.Value())))
	})
	w.tdpAdvancedBox.Append(sc)
	w.tdpAdvancedBox.Append(valLabel)
	return sc, valLabel
}

// buildUvScale creates a labeled undervolt slider and appends it to uvBox.
func (w *Window) buildUvScale(label string, lo, hi float64) (*gtk.Scale, *gtk.Label) {
	nameLabel := gtk.NewLabel(label)
	nameLabel.SetHAlign(gtk.AlignStart)
	nameLabel.AddCSSClass("scale-name")
	w.uvBox.Append(nameLabel)
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, lo, hi, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(false)
	sc.SetValue(0)
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	valLabel := gtk.NewLabel(uvLabel(label, 0))
	valLabel.AddCSSClass("scale-value")
	sc.ConnectValueChanged(func() {
		valLabel.SetLabel(uvLabel(label, int(sc.Value())))
	})
	w.uvBox.Append(sc)
	w.uvBox.Append(valLabel)
	return sc, valLabel
}

// uvLabel formats an undervolt value label, e.g. "CPU Curve Optimizer: -20" or "... 0 (stock)".
func uvLabel(name string, val int) string {
	if val == 0 {
		return fmt.Sprintf("%s: 0 (stock)", name)
	}
	return fmt.Sprintf("%s: %d", name, val)
}

// syncCustomView populates the custom view widgets for the current edit
// target. Which values it shows — the live projections or a stored profile's
// own settings — is profileui.ForEditor's decision, driven by the edit plan.
func (w *Window) syncCustomView() {
	if w.state == nil {
		return
	}
	prev := w.syncing
	w.syncing = true
	defer func() { w.syncing = prev }()

	plan := w.editPlan()
	es := profileui.ForEditor(w.state, plan)
	w.editorFloorPL1 = es.FloorPL1

	// The selector names the target, so the header stays a fixed title.
	w.syncProfileSelector()
	if w.editorNote != nil {
		w.editorNote.SetVisible(!plan.Live)
		if !plan.Live {
			w.editorNote.SetLabel("Not active — changes are stored and apply when this profile is activated.")
		}
	}

	// TDP.
	if es.TDP != nil {
		tdp := es.TDP
		if w.tdpBasicScale != nil {
			v := float64(tdp.PL1SPL)
			if m := float64(w.limits.BasicSliderMax()); v > m {
				v = m
			}
			w.tdpBasicScale.SetValue(v)
			w.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", int(v)))
		}
		if w.tdpPL1Scale != nil {
			w.tdpPL1Scale.SetValue(float64(tdp.PL1SPL))
			w.tdpPL1Label.SetLabel(fmt.Sprintf("%d W", tdp.PL1SPL))
		}
		if w.tdpPL2Scale != nil {
			w.tdpPL2Scale.SetValue(float64(tdp.PL2SPPT))
			w.tdpPL2Label.SetLabel(fmt.Sprintf("%d W", tdp.PL2SPPT))
		}
		if w.tdpPL3Scale != nil {
			w.tdpPL3Scale.SetValue(float64(tdp.FPPT))
			w.tdpPL3Label.SetLabel(fmt.Sprintf("%d W", tdp.FPPT))
		}

		// Switch to the advanced view when the displayed TDP cannot be
		// expressed in basic mode. Otherwise the basic slider silently clamps
		// and its label reports the clamped number, so the drawer claims 70W
		// while the profile holds 80W. Only ever forced on, never off: once
		// the user unchecks it that is a deliberate choice to edit in basic
		// terms.
		if w.tdpAdvancedCheck != nil && !w.tdpAdvancedCheck.Active() &&
			w.limits.NeedsAdvanced(es.HasTDP, *tdp) {
			w.tdpAdvancedCheck.SetActive(true)
		}
	} else {
		// The target saves no TDP of its own. Reset the sliders, or the
		// previous target's values linger when the editor is retargeted.
		w.resetTdpWidgets()
	}

	// Fan curve. Whether the daemon's points are worth adopting is
	// profileui.CurveToShow's decision: for a live target only a curve
	// actually in force counts (the registers keep stale points after a
	// release); for a stored target the profile either saves one or not.
	// Redraw either way — the PWM floor line depends on the target's limit,
	// which may have just changed.
	if w.fanCurve != nil {
		if pts, ok := profileui.CurveToShow(plan, es.FanCurve); ok {
			copy(w.fanCurve.points[:], pts)
		} else {
			w.fanCurve.points = w.limits.DefaultCurve()
		}
		// A curve saved while the floor was off can sit below it once a high
		// TDP is applied. Lift it so what is drawn is what the daemon would
		// accept.
		w.fanCurve.enforceConstraints(0)
		w.fanCurve.area.QueueDraw()
	}

	w.syncFanResetSensitivity()

	// Undervolt. The slider position is profileui's decision: the applied
	// offset for a live target (0 while a stock profile is active — CO is
	// reset in hardware there), the profile's saved offset for a stored one.
	if w.uvBox != nil {
		w.uvBox.SetVisible(w.state.UndervoltAvailable)
	}
	if w.uvCpuScale != nil {
		w.uvCpuScale.SetValue(float64(es.CO))
		w.uvCpuLabel.SetLabel(uvLabel("CPU Curve Optimizer", es.CO))
	}

	// Delete affordance, mirroring the daemon's refusals.
	if w.deleteBtn != nil {
		w.disarmDelete()
		block := profileui.DeleteBlockFor(w.state, plan.Target)
		w.deleteBtn.SetSensitive(block == "")
		if block == "" {
			w.deleteBtn.SetTooltipText("Remove this saved profile")
		} else {
			w.deleteBtn.SetTooltipText("Cannot delete: " + block)
		}
	}

	// Telemetry.
	if w.telemetryTempLabel != nil {
		w.telemetryTempLabel.SetLabel(fmt.Sprintf("APU: %d°C", w.state.Temperature))
	}
	if w.telemetryFanLabel != nil {
		w.telemetryFanLabel.SetLabel(fmt.Sprintf("Fan: %d RPM", w.state.FanRPM))
	}
}

// resetTdpWidgets returns the TDP sliders to a neutral default, for a target
// that saves no TDP of its own.
func (w *Window) resetTdpWidgets() {
	const def = 50
	if w.tdpBasicScale != nil {
		w.tdpBasicScale.SetValue(def)
		w.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", def))
	}
	for _, sc := range []struct {
		scale *gtk.Scale
		label *gtk.Label
	}{
		{w.tdpPL1Scale, w.tdpPL1Label},
		{w.tdpPL2Scale, w.tdpPL2Label},
		{w.tdpPL3Scale, w.tdpPL3Label},
	} {
		if sc.scale != nil {
			sc.scale.SetValue(def)
			sc.label.SetLabel(fmt.Sprintf("%d W", def))
		}
	}
}

// syncFanResetSensitivity enables or disables Reset Fans according to the applied
// sustained limit.
//
// The daemon refuses a fan reset while the high-TDP floor is in force — firmware
// auto has no floor, so releasing the fans there would remove the protection the
// power limit requires. Reset TDP is the way out, which the tooltip says.
//
// Separate from syncCustomView because the telemetry poll also needs it: it
// refreshes w.state every second, so a TDP change made elsewhere (the voltaire CLI,
// or another client) moves the floor line while the button kept its old
// sensitivity until the next full sync.
func (w *Window) syncFanResetSensitivity() {
	if w.resetFanBtn == nil {
		return
	}
	floorMin := w.fanFloorPWM()
	floored := floorMin > 0
	w.resetFanBtn.SetSensitive(!floored)
	if floored {
		w.resetFanBtn.SetTooltipText(fmt.Sprintf(
			"Unavailable while sustained TDP is above %dW — fans must hold the floor curve shown in the editor (%d%% minimum). Use Reset TDP first.",
			w.limits.TDPMaxSafe, pwmPct(floorMin)))
		return
	}
	w.resetFanBtn.SetTooltipText("Reset fan curves to firmware auto")
}

// tdpRequest is a snapshot of the TDP widgets, taken on the GTK thread so the
// socket call can run in a goroutine without touching widgets from it.
type tdpRequest struct {
	watts, pl1, pl2, pl3 string
	force                bool
}

// readTdpRequest snapshots the TDP sliders. **Must be called from the GTK main
// thread** — GTK is not thread-safe, and reading a scale from a goroutine is
// undefined behaviour, not merely a stale value.
func (w *Window) readTdpRequest() tdpRequest {
	if w.tdpAdvancedCheck != nil && w.tdpAdvancedCheck.Active() {
		pl1v, pl2v, pl3v := w.tdpPL1Scale.Value(), w.tdpPL2Scale.Value(), w.tdpPL3Scale.Value()
		pl1 := fmt.Sprintf("%d", int(pl1v))
		maxPL := int(math.Max(pl1v, math.Max(pl2v, pl3v)))
		return tdpRequest{
			// watts doubles as the base value; the daemon parses it before it looks
			// at the PL fields and rejects the request outright if it is empty.
			watts: pl1,
			pl1:   pl1,
			pl2:   fmt.Sprintf("%d", int(pl2v)),
			pl3:   fmt.Sprintf("%d", int(pl3v)),
			force: w.limits.ForceRequired(maxPL),
		}
	}
	return tdpRequest{watts: fmt.Sprintf("%d", int(w.tdpBasicScale.Value()))}
}

// send performs the socket round-trip for the plan's target. Safe to call
// from a goroutine — it holds only plain strings.
func (r tdpRequest) send(plan profileui.EditPlan) error {
	return apiresult.Err(api.SendTdpSetFor(plan.WireProfile(), r.watts, r.pl1, r.pl2, r.pl3, r.force))
}

// readFanCurve snapshots the fan curve as its wire string. Must be called from
// the GTK main thread; returns "" when there is no editor to read.
func (w *Window) readFanCurve() string {
	if w.fanCurve == nil {
		return ""
	}
	return w.fanCurve.curveString()
}

// sendFanCurve sends a previously snapshotted curve for the plan's target.
// Safe from a goroutine.
func sendFanCurve(plan profileui.EditPlan, curve string) error {
	if curve == "" {
		return nil
	}
	return apiresult.Err(api.SendFanCurveSetFor(plan.WireProfile(), curve))
}

// probeStoredTarget guards every stored-target send. A daemon older than the
// profile field unmarshals the request, silently drops the field, applies the
// edit to the running machine, and answers ok — the exact opposite of what a
// stored edit means, behind a success response. SendProfileList is the
// capability probe (such a daemon answers unknown-command to it), the same
// probe the CLI runs before every --profile send. A live plan needs no guard:
// bare sends mean the same thing on every daemon.
//
// Safe from a goroutine — it is a socket round-trip on plain values.
func probeStoredTarget(plan profileui.EditPlan) error {
	if plan.WireProfile() == "" {
		return nil
	}
	handled, _, err := api.SendProfileList()
	if e := apiresult.Err(handled, err); e != nil {
		if errors.Is(e, apiresult.ErrNotRunning) {
			return e
		}
		return fmt.Errorf("this daemon does not support editing a profile that is not running — "+
			"restart it after upgrading (systemctl --user restart voltaire): %w", e)
	}
	return nil
}

// refreshState fetches daemon state and re-syncs the custom view, the profile
// section, the autoswitch section, and the header. Every profile operation
// and daemon event uses it rather than syncing one widget: the fan curve
// editor's PWM floor is derived from the target's limit, so a TDP change has
// to re-evaluate the whole view, not just a highlight — and a profile
// created or deleted by another client has to reshape the list.
// Safe to call from a background goroutine.
func (w *Window) refreshState() {
	ok, state, rawErr := api.SendGetState()
	// Report a failed read rather than returning quietly. This runs after a
	// successful write, so a failure here means the daemon went away in between
	// and everything on screen is now stale — worth saying, since the widgets
	// otherwise keep displaying values nothing is honouring.
	if err := apiresult.Err(ok, rawErr); err != nil {
		w.reportError("Read daemon state", err)
		return
	}
	if state == nil {
		return
	}
	glib.IdleAdd(func() {
		w.state = state
		w.syncCustomView()
		w.syncing = true
		w.syncProfiles()
		w.syncAutoswitch()
		w.syncing = false
		w.updateHeader()
	})
}

// saveCustomTdp commits only the TDP values.
func (w *Window) saveCustomTdp() {
	req := w.readTdpRequest() // widget reads stay on the GTK thread
	plan := w.editPlan()      // resolved on the GTK thread; the goroutine gets a value
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Save TDP", err)
			return
		}
		if err := req.send(plan); err != nil {
			w.reportError("Save TDP", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("custom TDP saved", "profile", plan.Target, "live", plan.Live)
		w.refreshState()
	}()
}

// saveCustomFanCurve commits only the fan curve.
func (w *Window) saveCustomFanCurve() {
	curve := w.readFanCurve() // widget read stays on the GTK thread
	plan := w.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Save fan curve", err)
			return
		}
		if err := sendFanCurve(plan, curve); err != nil {
			w.reportError("Save fan curve", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("custom fan curve saved", "profile", plan.Target, "live", plan.Live)
		w.refreshState()
	}()
}

// saveCustomBoth commits both TDP and fan curve.
func (w *Window) saveCustomBoth() {
	req := w.readTdpRequest() // widget reads stay on the GTK thread
	curve := w.readFanCurve()
	plan := w.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Save profile", err)
			return
		}
		tdpErr := req.send(plan)
		fanErr := sendFanCurve(plan, curve)
		switch {
		case tdpErr != nil:
			// TDP first: a rejected TDP is usually why the fan write failed too
			// (the daemon refuses a curve below the floor curve while PL1 is high).
			w.reportError("Save TDP", tdpErr)
		case fanErr != nil:
			w.reportError("Save fan curve", fanErr)
		default:
			w.clearErrorAsync()
			slog.Info("custom profile saved (TDP + fans)", "profile", plan.Target, "live", plan.Live)
		}
		w.refreshState()
	}()
}

// resetTdp resets TDP: to firmware defaults for a live target, or removes the
// stored TDP from a target that is not running.
func (w *Window) resetTdp() {
	plan := w.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Reset TDP", err)
			return
		}
		if err := apiresult.Err(api.SendTdpResetFor(plan.WireProfile())); err != nil {
			w.reportError("Reset TDP", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("tdp reset", "profile", plan.Target, "live", plan.Live)
		w.refreshState()
	}()
}

// resetFanCurve resets the fan curve: to firmware auto for a live target, or
// removes the stored curve from a target that is not running.
func (w *Window) resetFanCurve() {
	plan := w.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Reset fans", err)
			return
		}
		if err := apiresult.Err(api.SendFanCurveResetFor(plan.WireProfile())); err != nil {
			// The daemon refuses this while the target's sustained TDP is above
			// the safe max — firmware auto has no PWM floor. Reset TDP is the
			// way out.
			w.reportError("Reset fans", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("fan curve reset", "profile", plan.Target, "live", plan.Live)
		w.refreshState()
	}()
}

// saveUndervolt commits the current Curve Optimizer offset.
func (w *Window) saveUndervolt() {
	cpu := fmt.Sprintf("%d", int(w.uvCpuScale.Value())) // GTK thread
	plan := w.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Save undervolt", err)
			return
		}
		if err := apiresult.Err(api.SendUndervoltSetFor(plan.WireProfile(), cpu)); err != nil {
			w.reportError("Save undervolt", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("undervolt saved", "cpu", cpu, "profile", plan.Target, "live", plan.Live)
		w.refreshState()
	}()
}

// resetUndervolt resets the Curve Optimizer to stock for a live target, or
// removes the stored offset from a target that is not running.
func (w *Window) resetUndervolt() {
	plan := w.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Reset undervolt", err)
			return
		}
		if err := apiresult.Err(api.SendUndervoltResetFor(plan.WireProfile())); err != nil {
			w.reportError("Reset undervolt", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("undervolt reset", "profile", plan.Target, "live", plan.Live)
		w.refreshState()
	}()
}

// deleteProfileClicked deletes the editor's target profile, with a two-tap
// confirmation in the button itself: the first tap arms it and it disarms on
// its own after a few seconds, on any retarget, and on every sync.
func (w *Window) deleteProfileClicked() {
	if !w.deleteArmed {
		w.deleteArmed = true
		w.deleteBtn.SetLabel("Tap again to delete")
		time.AfterFunc(3*time.Second, func() {
			glib.IdleAdd(func() bool {
				w.disarmDelete()
				return false
			})
		})
		return
	}
	name := w.editProfile
	w.disarmDelete()
	go func() {
		if err := apiresult.Err(api.SendProfileDelete(name)); err != nil {
			w.reportError("Delete profile", err)
			return
		}
		w.clearErrorAsync()
		slog.Info("profile deleted", "profile", name)
		// The target no longer exists. Fall back to "custom", which is always
		// addressable, rather than leaving the view pointed at a profile the
		// next sync cannot find.
		glib.IdleAdd(func() bool {
			w.setEditTarget(api.DefaultCustomProfile)
			return false
		})
		w.refreshState()
	}()
}

// disarmDelete returns the delete button to its resting label.
func (w *Window) disarmDelete() {
	w.deleteArmed = false
	if w.deleteBtn != nil {
		w.deleteBtn.SetLabel("Delete Profile")
	}
}

// startTelemetryPolling begins polling the daemon for APU temp and fan RPM
// every second while the drawer is visible. Updates the header telemetry
// label on all views, and also updates custom view labels + fan curve
// indicator when the custom view is active.
func (w *Window) startTelemetryPolling() {
	w.telemetryGen++
	gen := w.telemetryGen
	glib.TimeoutAdd(1000, func() bool {
		if gen != w.telemetryGen || !w.visible.Load() {
			return false
		}
		// One request at a time. api commands carry a 10s deadline, so against a
		// slow daemon a goroutine per tick meant ten overlapping requests whose
		// replies could apply out of order and walk w.state backwards.
		if w.telemetryBusy {
			slog.Debug("telemetry: skipping tick, request still in flight")
			return true
		}
		w.telemetryBusy = true
		go func() {
			ok, state, err := api.SendGetState()
			glib.IdleAdd(func() {
				// Cleared unconditionally, including on the failure paths below:
				// leaving it set would stop the poll for good.
				w.telemetryBusy = false
				if gen != w.telemetryGen {
					return
				}
				// Deliberately silent, unlike every other daemon call: this is a
				// background poll the user did not ask for, and reporting it would
				// repaint the bar every second, overwriting whatever error they were
				// reading. Their next action reports it — see refreshState.
				if !ok || err != nil || state == nil {
					return
				}
				w.state = state

				// Header telemetry (visible on all views).
				w.updateHeader()

				// Custom view telemetry (only when active).
				if w.viewStack != nil && w.viewStack.VisibleChildName() == "custom" {
					if w.telemetryTempLabel != nil {
						w.telemetryTempLabel.SetLabel(fmt.Sprintf("APU: %d°C", state.Temperature))
					}
					if w.telemetryFanLabel != nil {
						w.telemetryFanLabel.SetLabel(fmt.Sprintf("Fan: %d RPM", state.FanRPM))
					}
					if w.fanCurve != nil {
						w.fanCurve.area.QueueDraw()
					}
					// The floor can move with fresh state (a live target's PL1
					// changed elsewhere), and the chart line and the Reset Fans
					// gate both read it.
					w.updateEditorFloor()
					w.syncFanResetSensitivity()
				}
			})
		}()
		return true
	})
}

// buildCustomFocusList builds the 2D focus grid for the custom profile view.
//
// Row numbers run off a counter rather than literals because the profile
// selector contributes one row per custom profile when expanded, so
// everything below it shifts as profiles are created and deleted.
// syncProfileSelector rebuilds this list whenever that set changes.
func (w *Window) buildCustomFocusList() {
	var items []focusItem

	// Row 0: back button.
	row := 0
	items = append(items, focusItem{
		widget: w.customBackBtn, row: row, col: 0,
		section:    "nav",
		onActivate: func() { w.showMainView() },
	})

	// Profile selector: the collapsed row, then one row per profile while it
	// is expanded, then the actions and the inline name entry.
	row++
	items = append(items, focusItem{
		widget: w.profileSelBtn, row: row, col: 0,
		section:    "profile",
		onActivate: func() { w.profileSelBtn.Activate() },
	})
	listVis := func() bool { return w.profileSelBox != nil && w.profileSelBox.IsVisible() }
	for _, r := range w.customRows {
		btn := w.profileSelBtns[r.Name]
		if btn == nil {
			continue
		}
		row++
		items = append(items, focusItem{
			widget: btn, row: row, col: 0,
			section: "profile", isVisible: listVis,
			onActivate: func() { btn.Activate() },
		})
	}
	row++
	for col, btn := range []*gtk.Button{w.activateBtn, w.newProfileBtn, w.saveAsBtn} {
		btn := btn
		items = append(items, focusItem{
			widget: btn, row: row, col: col,
			section:    "profile",
			onActivate: func() { btn.Activate() },
		})
	}
	row++
	nameVis := func() bool { return w.nameRow != nil && w.nameRow.IsVisible() }
	items = append(items, focusItem{
		widget: w.nameOKBtn, row: row, col: 0,
		section: "profile", isVisible: nameVis,
		onActivate: func() { w.nameOKBtn.Activate() },
	})
	items = append(items, focusItem{
		widget: w.nameCancelBtn, row: row, col: 1,
		section: "profile", isVisible: nameVis,
		onActivate: func() { w.nameCancelBtn.Activate() },
	})

	// Basic TDP slider.
	if w.tdpBasicScale != nil {
		row++
		oL, oR, gV, sV := scaleAdjust(w.tdpBasicScale, 5)
		items = append(items, focusItem{
			widget: w.tdpBasicScale, row: row, col: 0,
			section:  "tdp",
			editable: true,
			onLeft:   oL, onRight: oR,
			getValue: gV, setValue: sV,
			isVisible: func() bool { return w.tdpBasicScale.IsVisible() },
		})
	}

	// Advanced checkbox.
	if w.tdpAdvancedCheck != nil {
		row++
		items = append(items, focusItem{
			widget: w.tdpAdvancedCheck, row: row, col: 0,
			section:    "tdp",
			onActivate: func() { w.tdpAdvancedCheck.SetActive(!w.tdpAdvancedCheck.Active()) },
		})
	}

	// PL1/PL2/PL3 sliders.
	advVis := func() bool { return w.tdpAdvancedBox.IsVisible() }
	for _, sc := range []*gtk.Scale{w.tdpPL1Scale, w.tdpPL2Scale, w.tdpPL3Scale} {
		row++
		oL, oR, gV, sV := scaleAdjust(sc, 1)
		items = append(items, focusItem{
			widget: sc, row: row, col: 0,
			section:   "tdp",
			editable:  true,
			isVisible: advVis,
			onLeft:    oL, onRight: oR,
			getValue: gV, setValue: sV,
		})
	}

	// Fan curve (navigable, dragged by touch/mouse).
	if w.fanCurve != nil {
		row++
		items = append(items, focusItem{
			widget: w.fanCurve.area, row: row, col: 0,
			section: "fan",
		})
	}

	// Undervolt (visible only when available).
	uvVis := func() bool { return w.tdpAdvancedBox.IsVisible() && w.uvBox != nil && w.uvBox.IsVisible() }
	if w.uvCpuScale != nil {
		row++
		oL, oR, gV, sV := scaleAdjust(w.uvCpuScale, 1)
		items = append(items, focusItem{
			widget: w.uvCpuScale, row: row, col: 0,
			section: "undervolt", editable: true, isVisible: uvVis,
			onLeft: oL, onRight: oR, getValue: gV, setValue: sV,
		})
	}
	row++
	items = append(items, focusItem{
		widget: w.saveUvBtn, row: row, col: 0,
		section: "undervolt", isVisible: uvVis,
		onActivate: func() { w.saveUvBtn.Activate() },
	})
	items = append(items, focusItem{
		widget: w.resetUvBtn, row: row, col: 1,
		section: "undervolt", isVisible: uvVis,
		onActivate: func() { w.resetUvBtn.Activate() },
	})

	// Save buttons.
	row++
	for col, btn := range []*gtk.Button{w.saveTdpBtn, w.saveFanBtn, w.saveBothBtn} {
		btn := btn
		items = append(items, focusItem{
			widget: btn, row: row, col: col,
			section:    "actions",
			onActivate: func() { btn.Activate() },
		})
	}

	// Reset buttons.
	row++
	for col, btn := range []*gtk.Button{w.resetTdpBtn, w.resetFanBtn} {
		btn := btn
		items = append(items, focusItem{
			widget: btn, row: row, col: col,
			section:    "actions",
			onActivate: func() { btn.Activate() },
		})
	}

	// Delete (its two-tap arm makes it safe to reach by D-pad).
	row++
	items = append(items, focusItem{
		widget: w.deleteBtn, row: row, col: 0,
		section:    "actions",
		onActivate: func() { w.deleteBtn.Activate() },
	})

	items = append(items, w.errBarFocusItem())
	w.customFocusItems = items
}
