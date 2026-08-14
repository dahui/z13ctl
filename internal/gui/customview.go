// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// customview.go — the custom profile view: the profile selector, TDP sliders,
// fan curve editor, undervolt, and the save/reset/delete actions.
//
// It is the largest view in the drawer and the only one that edits a *target*
// rather than the machine: the selector picks which saved profile everything
// below writes to, and whether a write reaches hardware at all is
// profileui.PlanEdit's decision, resolved per operation (editPlan) because the
// active profile can move underneath an open editor.
//
// The rules live in pure packages — internal/limits for the TDP and curve
// bounds, internal/profileui for targeting, naming and what the widgets should
// show. This file is the widgets and the daemon calls. The fan curve chart is
// its own type in fancurve.go; the selector and inline name entry are in
// profiles.go, on this same struct.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/focusgrid"
	"github.com/dahui/voltaire/v2/internal/limits"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// customView is the custom profile editor.
//
// editProfile is the profile being edited; showCustomView resolves it on every
// entry and the selector re-points it. Whether edits are live (bare sends,
// applied to hardware) or stored (the ...For variants) is resolved per
// operation by editPlan.
//
// editorFloorPL1 is the sustained limit the fan floor is evaluated against for
// the current target: the applied hardware limit for a live edit, the profile's
// own saved TDP for a stored one — hardware says nothing about a profile that
// is not running. The chart's floor line, the drag clamping and the Reset Fans
// gate all read it.
type customView struct {
	w    *Window
	host viewHost

	root       *gtk.Box
	focusItems []focusItem

	scroll  *gtk.ScrolledWindow
	backBtn *gtk.Button

	editProfile    string
	editorFloorPL1 int
	editorNote     *gtk.Label // "stored only" note; hidden for live edits

	// Profile selector and inline name entry (profiles.go).
	selDD         *dropdown
	activateBtn   *gtk.Button
	newProfileBtn *gtk.Button
	saveAsBtn     *gtk.Button
	actionsNote   *gtk.Label // refusal reasons for Activate / Save As (.block-note)
	nameRow       *gtk.Box   // inline create/save-as name entry row, hidden until needed
	nameEntry     *gtk.Entry
	nameOKBtn     *gtk.Button
	nameCancelBtn *gtk.Button
	nameMode      string // nameModeCreate or nameModeSaveAs while nameRow is up

	// TDP.
	tdpBasicScale    *gtk.Scale
	tdpBasicLabel    *gtk.Label
	tdpAdvancedCheck *gtk.CheckButton
	tdpAdvancedBox   *gtk.Box
	tdpPL1Scale      *gtk.Scale
	tdpPL2Scale      *gtk.Scale
	tdpPL3Scale      *gtk.Scale
	tdpPL1Label      *gtk.Label
	tdpPL2Label      *gtk.Label
	tdpPL3Label      *gtk.Label

	fanCurve *fanCurveEditor

	// Undervolt; the box is hidden when the daemon reports no CO support.
	uvBox      *gtk.Box
	uvCpuScale *gtk.Scale
	uvCpuLabel *gtk.Label
	saveUvBtn  *gtk.Button
	resetUvBtn *gtk.Button

	telemetryTempLabel *gtk.Label
	telemetryFanLabel  *gtk.Label

	saveTdpBtn  *gtk.Button
	saveFanBtn  *gtk.Button
	saveBothBtn *gtk.Button
	resetTdpBtn *gtk.Button
	resetFanBtn *gtk.Button
	resetNote   *gtk.Label // fan-floor refusal under the Reset row (.block-note)

	deleteBtn   *gtk.Button
	deleteNote  *gtk.Label // refusal reason under Delete Profile (.block-note)
	deleteArmed bool       // first tap of the two-tap delete confirmation
}

// hosted reports whether this instance is the full window's — host.back ==
// nil on both the desktop toplevel and the gamescope page. That surface's
// widgets take desktop shapes (inline values, natural-width buttons, plain
// checkboxes) where the drawer's take touch ones.
func (c *customView) hosted() bool { return c.host.back == nil }

// compactRow reshapes an action row for the hosted surface: natural-width
// buttons aligned start, instead of the drawer's full-width touch slabs. A
// no-op in the drawer.
func (c *customView) compactRow(row *gtk.Box, btns ...*gtk.Button) {
	if !c.hosted() {
		return
	}
	row.SetHAlign(gtk.AlignStart)
	for _, b := range btns {
		b.SetHExpand(false)
	}
}

// newCustomView builds the custom TDP/fan curve view for the given surface.
func newCustomView(w *Window, host viewHost) *customView {
	// The default target, so the selector has a name to show before
	// showCustomView resolves the running one. editPlan tolerates "" as well,
	// but the label built here would not.
	c := &customView{w: w, host: host, editProfile: api.DefaultCustomProfile}

	c.root = gtk.NewBox(gtk.OrientationVertical, 0)
	view := c.root

	// Header: back button + title. The full window supplies no back — its tab
	// bar names the page — so it gets no header either.
	if host.back != nil {
		c.backBtn = gtk.NewButton()
		c.backBtn.SetIconName("go-previous-symbolic")
		c.backBtn.AddCSSClass("view-back-btn")
		c.backBtn.ConnectClicked(host.back)

		header := gtk.NewBox(gtk.OrientationHorizontal, 8)
		header.SetMarginTop(10)
		header.SetMarginBottom(6)
		header.SetMarginStart(14)
		header.Append(c.backBtn)
		lbl := gtk.NewLabel("Custom Profiles")
		lbl.SetHAlign(gtk.AlignStart)
		lbl.AddCSSClass("drawer-title")
		header.Append(lbl)
		view.Append(header)
	}

	// Shown only for a stored edit — a target that is not running — where
	// nothing on this view touches hardware. Without it, Save doing nothing
	// observable reads as a dead button.
	c.editorNote = gtk.NewLabel("")
	c.editorNote.SetWrap(true)
	c.editorNote.SetHAlign(gtk.AlignStart)
	c.editorNote.AddCSSClass("scale-value")
	c.editorNote.SetMarginStart(14)
	c.editorNote.SetMarginEnd(14)
	c.editorNote.SetVisible(false)
	view.Append(c.editorNote)

	content := gtk.NewBox(gtk.OrientationVertical, 8)
	content.SetMarginTop(4)
	content.SetMarginBottom(12)
	content.SetMarginStart(12)
	content.SetMarginEnd(12)

	// hosted is the full-window presentation — host.back == nil on both the
	// desktop toplevel and the gamescope page. There the sections become
	// cards in the dashboard's visual language, laid out in two columns —
	// editing on the left, the fan curve and its save/reset actions on the
	// right — because a single column at window width is the drawer again,
	// only wider (Jeff, 2026-08-13). Side by side is also the arrangement
	// the content wants: the TDP that governs the fan floor sits beside the
	// curve it constrains. The drawer path stays byte-for-byte the layout it
	// always had.
	hosted := c.hosted()
	var leftCol, rightCol *gtk.Box
	if hosted {
		content.SetSpacing(12)
		columns := gtk.NewBox(gtk.OrientationHorizontal, 12)
		// Equal halves: the two columns hold different content, and letting
		// the wider side win would reflow the whole page every time the
		// Advanced toggle changes what the left column holds.
		columns.SetHomogeneous(true)
		leftCol = gtk.NewBox(gtk.OrientationVertical, 12)
		rightCol = gtk.NewBox(gtk.OrientationVertical, 12)
		columns.Append(leftCol)
		columns.Append(rightCol)
		content.Append(columns)
	}
	// newSection returns the container the next section's widgets land in: a
	// fresh .section-card in the given column on the hosted surface, or the
	// shared flat column (whose callers keep their historical separator
	// rhythm) in the drawer, where col is ignored.
	newSection := func(col *gtk.Box) *gtk.Box {
		if !hosted {
			return content
		}
		card := gtk.NewBox(gtk.OrientationVertical, 6)
		card.AddCSSClass("section-card")
		col.Append(card)
		return card
	}

	// --- PROFILE SELECTOR ---
	// The custom profiles live here rather than in the main view; everything
	// below edits whichever one this selects.
	sec := newSection(leftCol)
	sec.Append(c.buildProfileSelector())
	if !hosted {
		content.Append(separator())
	}

	// --- TELEMETRY ---
	// Drawer only. In the window the dashboard is one tab away with the same
	// numbers and their history behind them, and the fan curve editor already
	// draws the live temperature as its dashed marker — a readout card here
	// was duplication, not glanceability (Jeff, 2026-08-13). syncTelemetry
	// nil-guards the labels, so the hosted instance simply never builds them.
	if !hosted {
		sec = newSection(leftCol)
		sec.Append(sectionLabel("TELEMETRY"))
		telRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
		c.telemetryTempLabel = gtk.NewLabel("APU: --°C")
		c.telemetryTempLabel.SetHAlign(gtk.AlignStart)
		c.telemetryTempLabel.AddCSSClass("section-label")
		c.telemetryFanLabel = gtk.NewLabel("Fan: -- RPM")
		c.telemetryFanLabel.SetHAlign(gtk.AlignEnd)
		c.telemetryFanLabel.SetHExpand(true)
		c.telemetryFanLabel.AddCSSClass("section-label")
		telRow.Append(c.telemetryTempLabel)
		telRow.Append(c.telemetryFanLabel)
		sec.Append(telRow)
	}

	// --- TDP ---
	sec = newSection(leftCol)
	sec.Append(sectionLabel("TDP"))

	// Advanced checkbox — placed above sliders so toggle swaps content in-place.
	c.tdpAdvancedCheck = gtk.NewCheckButtonWithLabel("Advanced")
	c.tdpAdvancedCheck.AddCSSClass("advanced-check")
	if w.gamescope {
		addTouchActivate(c.tdpAdvancedCheck, func() { c.tdpAdvancedCheck.SetActive(!c.tdpAdvancedCheck.Active()) })
	}
	sec.Append(c.tdpAdvancedCheck)

	// Basic TDP box (visible by default).
	tdpBasicBox := gtk.NewBox(gtk.OrientationVertical, 4)
	c.tdpBasicScale = gtk.NewScaleWithRange(gtk.OrientationHorizontal, float64(w.limits.TDPMin), float64(w.limits.BasicSliderMax()), 1)
	c.tdpBasicScale.SetDigits(0)
	c.tdpBasicScale.SetDrawValue(false)
	c.tdpBasicScale.SetValue(float64(50))
	c.tdpBasicScale.SetFocusable(false)
	w.wheelScrollsView(c.tdpBasicScale)
	c.tdpBasicLabel = gtk.NewLabel("50 W")
	c.tdpBasicLabel.AddCSSClass("scale-value")
	c.tdpBasicScale.ConnectValueChanged(func() {
		c.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", int(c.tdpBasicScale.Value())))
	})
	if hosted {
		// Value beside the slider, not centred beneath it: the desktop eye
		// scans a form row, where the touch column stacks for a thumb.
		basicRow := gtk.NewBox(gtk.OrientationHorizontal, 10)
		c.tdpBasicScale.SetHExpand(true)
		basicRow.Append(c.tdpBasicScale)
		basicRow.Append(c.tdpBasicLabel)
		tdpBasicBox.Append(basicRow)
	} else {
		tdpBasicBox.Append(c.tdpBasicScale)
		tdpBasicBox.Append(c.tdpBasicLabel)
	}
	sec.Append(tdpBasicBox)

	// Advanced box (hidden by default) — replaces basic slider in-place.
	c.tdpAdvancedBox = gtk.NewBox(gtk.OrientationVertical, 4)
	c.tdpAdvancedBox.SetVisible(false)

	tdpWarn := gtk.NewLabel(fmt.Sprintf(
		"WARNING: Values above %dW may cause thermal throttling, instability, or hardware damage. Use at your own risk — we are not responsible for any damages.",
		w.limits.TDPMaxSafe))
	tdpWarn.SetWrap(true)
	tdpWarn.SetHAlign(gtk.AlignStart)
	tdpWarn.AddCSSClass("tdp-warning")
	c.tdpAdvancedBox.Append(tdpWarn)

	c.tdpPL1Scale, c.tdpPL1Label = c.buildTdpScale("PL1 (SPL)", "Sustained power limit — the long-term average power the CPU targets.")
	c.tdpPL2Scale, c.tdpPL2Label = c.buildTdpScale("PL2 (SPPT)", "Short boost — maximum power during brief burst workloads.")
	c.tdpPL3Scale, c.tdpPL3Label = c.buildTdpScale("PL3 (FPPT)", "Fast boost — peak instantaneous power for single-threaded spikes.")

	// --- UNDERVOLT (inside advanced box) ---
	c.uvBox = gtk.NewBox(gtk.OrientationVertical, 4)
	// Hidden by default; sync shows it when UndervoltAvailable.
	c.uvBox.SetVisible(false)

	c.uvBox.Append(sectionLabel("UNDERVOLT"))

	uvWarn := gtk.NewLabel("Undervolt offsets are only active while the Custom profile is selected. Switching to a stock profile resets them to 0. Unstable values may cause crashes.")
	uvWarn.SetWrap(true)
	uvWarn.SetHAlign(gtk.AlignStart)
	uvWarn.AddCSSClass("tdp-warning")
	c.uvBox.Append(uvWarn)

	c.uvCpuScale, c.uvCpuLabel = c.buildUvScale("CPU Curve Optimizer", -40, 0)

	// UV buttons: Save UV | Reset UV
	uvBtnRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	uvBtnRow.AddCSSClass("custom-actions")

	c.saveUvBtn = gtk.NewButtonWithLabel("Save UV")
	c.saveUvBtn.AddCSSClass("save-btn")
	c.saveUvBtn.SetHExpand(true)
	c.saveUvBtn.ConnectClicked(func() { c.saveUndervolt() })
	uvBtnRow.Append(c.saveUvBtn)

	c.resetUvBtn = gtk.NewButtonWithLabel("Reset UV")
	c.resetUvBtn.SetHExpand(true)
	c.resetUvBtn.ConnectClicked(func() { c.resetUndervolt() })
	uvBtnRow.Append(c.resetUvBtn)
	c.compactRow(uvBtnRow, c.saveUvBtn, c.resetUvBtn)

	c.uvBox.Append(uvBtnRow)
	c.tdpAdvancedBox.Append(c.uvBox)

	sec.Append(c.tdpAdvancedBox)

	c.tdpAdvancedCheck.ConnectToggled(func() {
		adv := c.tdpAdvancedCheck.Active()
		c.tdpAdvancedBox.SetVisible(adv)
		tdpBasicBox.SetVisible(!adv)
	})

	if !hosted {
		content.Append(separator())
	}

	// --- FAN CURVE ---
	sec = newSection(rightCol)
	sec.Append(sectionLabel("FAN CURVE"))
	c.fanCurve = c.newFanCurveEditor()
	sec.Append(c.fanCurve.area)

	if !hosted {
		content.Append(separator())
	}

	// --- BUTTONS ---
	// In the right column, under the curve: save and reset act on what both
	// columns show, but they sit with the editor's big visual so committing
	// is where the eye already is.
	sec = newSection(rightCol)
	// Save row: Save TDP | Save Fans | Save Both
	saveRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	saveRow.AddCSSClass("custom-actions")

	c.saveTdpBtn = gtk.NewButtonWithLabel("Save TDP")
	c.saveTdpBtn.AddCSSClass("save-btn")
	c.saveTdpBtn.SetHExpand(true)
	c.saveTdpBtn.ConnectClicked(func() { c.saveCustomTdp() })
	saveRow.Append(c.saveTdpBtn)

	c.saveFanBtn = gtk.NewButtonWithLabel("Save Fans")
	c.saveFanBtn.AddCSSClass("save-btn")
	c.saveFanBtn.SetHExpand(true)
	c.saveFanBtn.ConnectClicked(func() { c.saveCustomFanCurve() })
	saveRow.Append(c.saveFanBtn)

	c.saveBothBtn = gtk.NewButtonWithLabel("Save Both")
	c.saveBothBtn.AddCSSClass("save-btn")
	c.saveBothBtn.SetHExpand(true)
	c.saveBothBtn.ConnectClicked(func() { c.saveCustomBoth() })
	saveRow.Append(c.saveBothBtn)
	c.compactRow(saveRow, c.saveTdpBtn, c.saveFanBtn, c.saveBothBtn)

	sec.Append(saveRow)

	// Reset row: Reset TDP | Reset Fans
	resetRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	resetRow.AddCSSClass("custom-actions")

	c.resetTdpBtn = gtk.NewButtonWithLabel("Reset TDP")
	c.resetTdpBtn.SetHExpand(true)
	c.resetTdpBtn.ConnectClicked(func() { c.resetTdp() })
	resetRow.Append(c.resetTdpBtn)

	c.resetFanBtn = gtk.NewButtonWithLabel("Reset Fans")
	c.resetFanBtn.SetHExpand(true)
	w.setHint(c.resetFanBtn, "Reset fan curves to firmware auto")
	c.resetFanBtn.ConnectClicked(func() { c.resetFanCurve() })
	resetRow.Append(c.resetFanBtn)
	c.compactRow(resetRow, c.resetTdpBtn, c.resetFanBtn)

	sec.Append(resetRow)
	c.resetNote = blockNote()
	sec.Append(c.resetNote)

	// --- DELETE ---
	// Lives in the editor rather than on the profile row: the editor knows its
	// target, and a delete affordance on every row is an accidental tap away
	// from data loss. Two taps stand in for a confirm dialog (no popovers —
	// they do not composite under gamescope); sensitivity mirrors the daemon's
	// refusals via profileui.DeleteBlockFor.
	if !hosted {
		content.Append(separator())
	}
	// Bottom of the left column, away from the save actions: destructive and
	// constructive should not be neighbours.
	sec = newSection(leftCol)
	c.deleteBtn = gtk.NewButtonWithLabel("Delete Profile")
	w.setHint(c.deleteBtn, "Remove this saved profile")
	c.deleteBtn.ConnectClicked(func() { c.deleteProfileClicked() })
	if hosted {
		// A destructive action is a small deliberate target on a desktop, not
		// a full-width bar inviting the tap it exists to survive.
		c.deleteBtn.SetHAlign(gtk.AlignStart)
	}
	sec.Append(c.deleteBtn)
	c.deleteNote = blockNote()
	sec.Append(c.deleteNote)

	c.scroll = newDrawerScroll(content)
	view.Append(c.scroll)

	c.buildFocusList()
	return c
}

// buildTdpScale creates a labeled TDP slider and appends it to the advanced
// box. Returns the scale and value label.
func (c *customView) buildTdpScale(label, desc string) (*gtk.Scale, *gtk.Label) {
	w := c.w
	nameLabel := gtk.NewLabel(label)
	nameLabel.SetHAlign(gtk.AlignStart)
	nameLabel.AddCSSClass("scale-name")
	descLabel := gtk.NewLabel(desc)
	descLabel.SetHAlign(gtk.AlignStart)
	descLabel.SetWrap(true)
	descLabel.AddCSSClass("scale-value")
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
	if c.hosted() {
		// Desktop form row: name and value share a header line, description
		// beneath, slider last — the shape every settings app uses. The touch
		// column below stacks each on its own line for a thumb-sized target.
		head := gtk.NewBox(gtk.OrientationHorizontal, 8)
		head.Append(nameLabel)
		valLabel.SetHAlign(gtk.AlignEnd)
		valLabel.SetHExpand(true)
		head.Append(valLabel)
		c.tdpAdvancedBox.Append(head)
		c.tdpAdvancedBox.Append(descLabel)
		c.tdpAdvancedBox.Append(sc)
	} else {
		c.tdpAdvancedBox.Append(nameLabel)
		c.tdpAdvancedBox.Append(descLabel)
		c.tdpAdvancedBox.Append(sc)
		c.tdpAdvancedBox.Append(valLabel)
	}
	return sc, valLabel
}

// buildUvScale creates a labeled undervolt slider and appends it to uvBox.
func (c *customView) buildUvScale(label string, lo, hi float64) (*gtk.Scale, *gtk.Label) {
	nameLabel := gtk.NewLabel(label)
	nameLabel.SetHAlign(gtk.AlignStart)
	nameLabel.AddCSSClass("scale-name")
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, lo, hi, 1)
	sc.SetDigits(0)
	sc.SetDrawValue(false)
	sc.SetValue(0)
	sc.SetFocusable(false)
	c.w.wheelScrollsView(sc)
	valLabel := gtk.NewLabel(c.uvText(label, 0))
	valLabel.AddCSSClass("scale-value")
	sc.ConnectValueChanged(func() {
		valLabel.SetLabel(c.uvText(label, int(sc.Value())))
	})
	if c.hosted() {
		// Same desktop form row as buildTdpScale.
		head := gtk.NewBox(gtk.OrientationHorizontal, 8)
		head.Append(nameLabel)
		valLabel.SetHAlign(gtk.AlignEnd)
		valLabel.SetHExpand(true)
		head.Append(valLabel)
		c.uvBox.Append(head)
		c.uvBox.Append(sc)
	} else {
		c.uvBox.Append(nameLabel)
		c.uvBox.Append(sc)
		c.uvBox.Append(valLabel)
	}
	return sc, valLabel
}

// uvText formats an undervolt value for this surface: the drawer's stacked
// label repeats the name ("CPU Curve Optimizer: -20") because the value sits
// alone under the slider; the window's header row already shows the name on
// the same line, so the value stands bare ("-20", "0 (stock)").
func (c *customView) uvText(name string, val int) string {
	if c.hosted() {
		if val == 0 {
			return "0 (stock)"
		}
		return fmt.Sprintf("%d", val)
	}
	return uvLabel(name, val)
}

// uvLabel formats an undervolt value label, e.g. "CPU Curve Optimizer: -20" or "... 0 (stock)".
func uvLabel(name string, val int) string {
	if val == 0 {
		return fmt.Sprintf("%s: 0 (stock)", name)
	}
	return fmt.Sprintf("%s: %d", name, val)
}

// showCustomView switches the view stack to the custom profile view, opening
// on the running custom profile when there is one and "custom" otherwise.
// Lazy-builds the view on first access; a second tap returns to the main view.
func (w *Window) showCustomView() {
	if w.viewStack == nil {
		return
	}
	w.closePopup()
	if w.viewStack.VisibleChildName() == "custom" {
		w.showMainView()
		return
	}
	if w.custom == nil {
		w.custom = newCustomView(w, w.drawerHost("custom"))
		w.viewStack.AddNamed(w.custom.root, "custom")
	}
	w.custom.editProfile = profileui.DefaultEditTarget(w.state)
	w.custom.disarmDelete()
	w.custom.sync()
	w.viewStack.SetVisibleChildName("custom")
	w.swapFocusList(w.custom.focusItems)
	w.startTelemetryPolling()
}

// Edit target and fan floor.

// updateEditorFloor recomputes the floor limit for the editor's target from
// fresh state. For a live target the applied PL1 can move underneath the
// editor (the CLI, another client); a stored target's own TDP only moves
// through this editor, but recomputing costs nothing.
func (c *customView) updateEditorFloor() {
	c.editorFloorPL1 = profileui.ForEditor(c.w.state, c.editPlan()).FloorPL1
}

// fanFloorPWM returns the minimum fan PWM the daemon will accept for the
// editor's current target.
//
// Derived from editorFloorPL1 rather than the slider position: the daemon
// validates against the applied limit (live target) or the profile's own
// saved TDP (stored target), and a slider the user has moved but not saved is
// neither. Must be called from the GTK main thread.
func (c *customView) fanFloorPWM() int {
	return c.w.limits.FanFloorPWM(c.editorFloorPL1)
}

// pwmPct renders a PWM value as a rounded percentage for display. Plain integer
// division reads 127 (the 50% floor) as "49%".
func pwmPct(pwm int) int {
	return (pwm*100 + limits.PWMMax/2) / limits.PWMMax
}

// sync populates the custom view widgets for the current edit target. Which
// values it shows — the live projections or a stored profile's own settings —
// is profileui.ForEditor's decision, driven by the edit plan.
func (c *customView) sync() {
	w := c.w
	if w.state == nil {
		return
	}
	if w.syncsSuppressed() {
		return
	}
	prev := w.syncing
	w.syncing = true
	defer func() { w.syncing = prev }()

	plan := c.editPlan()
	es := profileui.ForEditor(w.state, plan)
	c.editorFloorPL1 = es.FloorPL1

	// The selector names the target, so the header stays a fixed title.
	c.syncProfileSelector()
	if c.editorNote != nil {
		c.editorNote.SetVisible(!plan.Live)
		if !plan.Live {
			c.editorNote.SetLabel("Not active — changes are stored and apply when this profile is activated.")
		}
	}

	// TDP.
	if es.TDP != nil {
		tdp := es.TDP
		if c.tdpBasicScale != nil {
			v := float64(tdp.PL1SPL)
			if m := float64(w.limits.BasicSliderMax()); v > m {
				v = m
			}
			c.tdpBasicScale.SetValue(v)
			c.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", int(v)))
		}
		if c.tdpPL1Scale != nil {
			c.tdpPL1Scale.SetValue(float64(tdp.PL1SPL))
			c.tdpPL1Label.SetLabel(fmt.Sprintf("%d W", tdp.PL1SPL))
		}
		if c.tdpPL2Scale != nil {
			c.tdpPL2Scale.SetValue(float64(tdp.PL2SPPT))
			c.tdpPL2Label.SetLabel(fmt.Sprintf("%d W", tdp.PL2SPPT))
		}
		if c.tdpPL3Scale != nil {
			c.tdpPL3Scale.SetValue(float64(tdp.FPPT))
			c.tdpPL3Label.SetLabel(fmt.Sprintf("%d W", tdp.FPPT))
		}

		// Switch to the advanced view when the displayed TDP cannot be
		// expressed in basic mode. Otherwise the basic slider silently clamps
		// and its label reports the clamped number, so the drawer claims 70W
		// while the profile holds 80W. Only ever forced on, never off: once
		// the user unchecks it that is a deliberate choice to edit in basic
		// terms.
		if c.tdpAdvancedCheck != nil && !c.tdpAdvancedCheck.Active() &&
			w.limits.NeedsAdvanced(es.HasTDP, *tdp) {
			c.tdpAdvancedCheck.SetActive(true)
		}
	} else {
		// The target saves no TDP of its own. Reset the sliders, or the
		// previous target's values linger when the editor is retargeted.
		c.resetTdpWidgets()
	}

	// Fan curve. Whether the daemon's points are worth adopting is
	// profileui.CurveToShow's decision: for a live target only a curve
	// actually in force counts (the registers keep stale points after a
	// release); for a stored target the profile either saves one or not.
	// Redraw either way — the PWM floor line depends on the target's limit,
	// which may have just changed.
	if c.fanCurve != nil {
		if pts, ok := profileui.CurveToShow(plan, es.FanCurve); ok {
			copy(c.fanCurve.points[:], pts)
		} else {
			c.fanCurve.points = w.limits.DefaultCurve()
		}
		// A curve saved while the floor was off can sit below it once a high
		// TDP is applied. Lift it so what is drawn is what the daemon would
		// accept.
		c.fanCurve.enforceConstraints(0)
		c.fanCurve.area.QueueDraw()
	}

	c.syncFanResetSensitivity()

	// Undervolt. The slider position is profileui's decision: the applied
	// offset for a live target (0 while a stock profile is active — CO is
	// reset in hardware there), the profile's saved offset for a stored one.
	if c.uvBox != nil {
		c.uvBox.SetVisible(w.state.UndervoltAvailable)
	}
	if c.uvCpuScale != nil {
		c.uvCpuScale.SetValue(float64(es.CO))
		c.uvCpuLabel.SetLabel(c.uvText("CPU Curve Optimizer", es.CO))
	}

	// Delete affordance, mirroring the daemon's refusals. The reason goes to
	// the block note beneath the button, where touch and controller users can
	// read it; a tooltip would be invisible to both.
	if c.deleteBtn != nil {
		c.disarmDelete()
		block := profileui.DeleteBlockFor(w.state, plan.Target)
		c.deleteBtn.SetSensitive(block == "")
		if block != "" {
			block = "Cannot delete: " + block
		}
		setBlockNote(c.deleteNote, block)
	}

	// Telemetry.
	c.syncTelemetry(w.state)
}

// syncTelemetry sets the view's APU temperature and fan speed labels. Shared
// with the telemetry poll, which refreshes them every second while this view
// is the visible one.
func (c *customView) syncTelemetry(st *api.State) {
	if st == nil {
		return
	}
	if c.telemetryTempLabel != nil {
		c.telemetryTempLabel.SetLabel(fmt.Sprintf("APU: %d°C", st.Temperature))
	}
	if c.telemetryFanLabel != nil {
		c.telemetryFanLabel.SetLabel(fmt.Sprintf("Fan: %d RPM", st.FanRPM))
	}
}

// pollTick applies one telemetry sample while this view is the visible one.
// Deliberately narrower than a full sync: the poll runs every second and must
// not rebuild anything the user could be interacting with.
func (c *customView) pollTick(st *api.State) {
	if c == nil {
		return
	}
	c.syncTelemetry(st)
	if c.fanCurve != nil {
		c.fanCurve.area.QueueDraw()
	}
	// The floor can move with fresh state (a live target's PL1 changed
	// elsewhere), and the chart line and the Reset Fans gate both read it.
	c.updateEditorFloor()
	c.syncFanResetSensitivity()
}

// resetTdpWidgets returns the TDP sliders to a neutral default, for a target
// that saves no TDP of its own.
func (c *customView) resetTdpWidgets() {
	const def = 50
	if c.tdpBasicScale != nil {
		c.tdpBasicScale.SetValue(def)
		c.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", def))
	}
	for _, sc := range []struct {
		scale *gtk.Scale
		label *gtk.Label
	}{
		{c.tdpPL1Scale, c.tdpPL1Label},
		{c.tdpPL2Scale, c.tdpPL2Label},
		{c.tdpPL3Scale, c.tdpPL3Label},
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
// power limit requires. Reset TDP is the way out, which the block note says.
//
// Separate from sync because the telemetry poll also needs it: it refreshes
// w.state every second, so a TDP change made elsewhere (the voltaire CLI, or
// another client) moves the floor line while the button kept its old
// sensitivity until the next full sync.
func (c *customView) syncFanResetSensitivity() {
	if c.resetFanBtn == nil {
		return
	}
	floorMin := c.fanFloorPWM()
	floored := floorMin > 0
	c.resetFanBtn.SetSensitive(!floored)
	if floored {
		setBlockNote(c.resetNote, fmt.Sprintf(
			"Reset Fans unavailable while sustained TDP is above %dW — fans must hold the floor curve shown in the editor (%d%% minimum). Use Reset TDP first.",
			c.w.limits.TDPMaxSafe, pwmPct(floorMin)))
		return
	}
	setBlockNote(c.resetNote, "")
}

// buildCustomFocusList builds the 2D focus grid for the custom profile view.
// Called exactly once, when the view is first built: the profile list lives
// in the selector's popup (with its own focus frame), so nothing in this
// grid shifts when profiles are created or deleted.
func (c *customView) buildFocusList() {
	var items []focusItem
	b := focusgrid.NewBuilder(focusgrid.Vertical)

	// buttonLine appends n buttons across one line in the current section.
	buttonLine := func(btns ...*gtk.Button) {
		for i, fc := range b.Line(len(btns)) {
			btn := btns[i]
			items = append(items, focusItem{
				widget: btn, row: fc.Row, col: fc.Col, section: fc.Section,
				onActivate: func() { btn.Activate() },
			})
		}
	}
	// sliderLine appends one editable slider on its own line.
	sliderLine := func(sc *gtk.Scale, step float64, vis func() bool) {
		oL, oR, gV, sV := scaleAdjust(sc, step)
		fc := b.One()
		items = append(items, focusItem{
			widget: sc, row: fc.Row, col: fc.Col, section: fc.Section,
			editable: true, isVisible: vis,
			onLeft: oL, onRight: oR,
			getValue: gV, setValue: sV,
		})
	}

	// Back button.
	var fc focusgrid.Coord
	if c.backBtn != nil {
		fc = b.Section("nav").One()
		items = append(items, focusItem{
			widget: c.backBtn, row: fc.Row, col: fc.Col, section: fc.Section,
			onActivate: c.host.back,
		})
	}

	// Profile selector dropdown, then the actions and the inline name entry.
	b.Section("profile")
	fc = b.One()
	items = append(items, focusItem{
		widget: c.selDD.btn, row: fc.Row, col: fc.Col, section: fc.Section,
		onActivate: func() { c.selDD.btn.Activate() },
	})
	buttonLine(c.activateBtn, c.newProfileBtn, c.saveAsBtn)

	nameVis := func() bool { return c.nameRow != nil && c.nameRow.IsVisible() }
	nameBtns := []*gtk.Button{c.nameOKBtn, c.nameCancelBtn}
	for i, nc := range b.Line(len(nameBtns)) {
		btn := nameBtns[i]
		items = append(items, focusItem{
			widget: btn, row: nc.Row, col: nc.Col, section: nc.Section,
			isVisible:  nameVis,
			onActivate: func() { btn.Activate() },
		})
	}

	// Basic TDP slider.
	b.Section("tdp")
	if c.tdpBasicScale != nil {
		sliderLine(c.tdpBasicScale, 5, func() bool { return c.tdpBasicScale.IsVisible() })
	}

	// Advanced checkbox.
	if c.tdpAdvancedCheck != nil {
		fc = b.One()
		items = append(items, focusItem{
			widget: c.tdpAdvancedCheck, row: fc.Row, col: fc.Col, section: fc.Section,
			onActivate: func() { c.tdpAdvancedCheck.SetActive(!c.tdpAdvancedCheck.Active()) },
		})
	}

	// PL1/PL2/PL3 sliders.
	advVis := func() bool { return c.tdpAdvancedBox.IsVisible() }
	for _, sc := range []*gtk.Scale{c.tdpPL1Scale, c.tdpPL2Scale, c.tdpPL3Scale} {
		sliderLine(sc, 1, advVis)
	}

	// Fan curve (navigable, dragged by touch/mouse).
	if c.fanCurve != nil {
		fc = b.Section("fan").One()
		items = append(items, focusItem{
			widget: c.fanCurve.area, row: fc.Row, col: fc.Col, section: fc.Section,
		})
	}

	// Undervolt (visible only when available).
	uvVis := func() bool { return c.tdpAdvancedBox.IsVisible() && c.uvBox != nil && c.uvBox.IsVisible() }
	b.Section("undervolt")
	if c.uvCpuScale != nil {
		sliderLine(c.uvCpuScale, 1, uvVis)
	}
	uvBtns := []*gtk.Button{c.saveUvBtn, c.resetUvBtn}
	for i, uc := range b.Line(len(uvBtns)) {
		btn := uvBtns[i]
		items = append(items, focusItem{
			widget: btn, row: uc.Row, col: uc.Col, section: uc.Section,
			isVisible:  uvVis,
			onActivate: func() { btn.Activate() },
		})
	}

	// Save, reset, and delete. Delete's two-tap arm makes it safe to reach by
	// D-pad.
	b.Section("actions")
	buttonLine(c.saveTdpBtn, c.saveFanBtn, c.saveBothBtn)
	buttonLine(c.resetTdpBtn, c.resetFanBtn)
	buttonLine(c.deleteBtn)

	items = append(items, c.host.errBar.focusItem())
	logFocusList(c.host.focusName("custom"), items)
	c.focusItems = items
}

// Window-level entry points. Each nil-guards the view, which is built lazily
// on first navigation.

// customViews returns every custom profile editor that has been built: the
// drawer's, and the full window's when it exists. Both edit the same daemon
// state, so anything that refreshes one refreshes both — a stale second editor
// showing a profile's old power limits is exactly the kind of thing nobody
// notices until they save from it.
func (w *Window) customViews() []*customView {
	out := make([]*customView, 0, 2)
	if w.custom != nil {
		out = append(out, w.custom)
	}
	if w.mainWin != nil && w.mainWin.custom != nil {
		out = append(out, w.mainWin.custom)
	}
	return out
}

// syncCustomView re-syncs every custom profile editor that has been built.
func (w *Window) syncCustomView() {
	for _, c := range w.customViews() {
		c.sync()
	}
}

// redrawFanCurve repaints the fan curve chart after a theme change. It is drawn
// with Cairo from w.colors rather than styled by CSS, so swapping the CSS
// provider alone leaves it in the previous theme's colours.
func (w *Window) redrawFanCurve() {
	for _, c := range w.customViews() {
		if c.fanCurve != nil {
			c.fanCurve.area.QueueDraw()
		}
	}
}
