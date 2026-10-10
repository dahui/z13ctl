// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// customview.go — the custom profile view: the profile selector, TDP sliders,
// fan curve editor, undervolt, and the save/reset/delete actions.
//
// It is the full window's Profiles page and the only view that edits a *target*
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

	scroll *gtk.ScrolledWindow

	editProfile    string
	editorFloorPL1 int
	editorNote     *gtk.Label // "stored only" note; hidden for live edits

	// Profile selector and inline name entry (profiles.go).
	selDD *dropdown
	// profileActions is the selector's action row (Activate / + New /
	// Save As), kept so the window can seat Delete beside them.
	profileActions *gtk.Box
	activateBtn    *gtk.Button
	newProfileBtn  *gtk.Button
	saveAsBtn      *gtk.Button
	actionsNote    *gtk.Label // refusal reasons for Activate / Save As (.block-note)
	nameRow        *gtk.Box   // inline create/save-as name entry row, hidden until needed
	nameEntry      *gtk.Entry
	nameOKBtn      *gtk.Button
	nameCancelBtn  *gtk.Button
	nameMode       string // nameModeCreate or nameModeSaveAs while nameRow is up

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
	// tdpWarn names the safe maximum, so it is a field: applyLimits rewrites
	// it when the device's limits change under a running GUI.
	tdpWarn *gtk.Label

	fanCurve *fanCurveEditor

	// One button per device-declared preset, in declaration order. Empty when
	// the device declares none, which is also when no row was built.
	presetBtns []*gtk.Button

	// Undervolt; the box is hidden when the daemon reports no CO support.
	uvBox      *gtk.Box
	uvCpuScale *gtk.Scale
	uvCpuLabel *gtk.Label
	resetUvBtn *gtk.Button

	resetTdpBtn *gtk.Button
	resetFanBtn *gtk.Button
	resetNote   *gtk.Label // fan-floor refusal under the Reset row (.block-note)

	deleteBtn   *gtk.Button
	deleteNote  *gtk.Label // refusal reason under Delete Profile (.block-note)
	deleteArmed bool       // first tap of the two-tap delete confirmation

	// One-commit model (customcommit.go). The commit bar's button and
	// indicator, and the widget baseline
	// each sync captures so commitDirty can tell an edit from a re-display.
	commitBtn    *gtk.Button
	resetAllBtn  *gtk.Button
	commitNote   *gtk.Label
	baseBasic    int    // basic TDP slider at last sync
	baseAdv      [3]int // PL1/PL2/PL3 sliders at last sync
	baseCurve    string // fan curve wire string at last sync
	baseUv       int    // undervolt slider at last sync
	baseCaptured bool
}

// newCustomView builds the custom TDP/fan curve view for the given surface.
func newCustomView(w *Window, host viewHost) *customView {
	// The default target, so the selector has a name to show before
	// showCustomView resolves the running one. editPlan tolerates "" as well,
	// but the label built here would not.
	c := &customView{w: w, host: host, editProfile: api.DefaultCustomProfile}

	c.root = gtk.NewBox(gtk.OrientationVertical, 0)
	view := c.root

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

	// The sections are cards in the dashboard's visual language, laid out in
	// two columns — editing on the left, the fan curve and its actions on the
	// right — because a single column at window width is the drawer again,
	// only wider (Jeff, 2026-08-13). Side by side is also the arrangement the
	// content wants: the TDP that governs the fan floor sits beside the curve
	// it constrains.
	content.SetSpacing(12)
	columns := gtk.NewBox(gtk.OrientationHorizontal, 12)
	// Equal halves: the two columns hold different content, and letting the
	// wider side win would reflow the whole page every time the Advanced
	// toggle changes what the left column holds.
	columns.SetHomogeneous(true)
	leftCol := gtk.NewBox(gtk.OrientationVertical, 12)
	rightCol := gtk.NewBox(gtk.OrientationVertical, 12)
	columns.Append(leftCol)
	columns.Append(rightCol)
	content.Append(columns)
	// newSection returns a fresh .section-card in the given column for the
	// next section's widgets.
	newSection := func(col *gtk.Box) *gtk.Box {
		card := gtk.NewBox(gtk.OrientationVertical, 6)
		card.AddCSSClass("section-card")
		col.Append(card)
		return card
	}

	// --- PROFILE SELECTOR ---
	// The custom profiles live here rather than in the main view; everything
	// below edits whichever one this selects.
	sec := newSection(leftCol)
	profileSec := sec
	sec.Append(c.buildProfileSelector())

	// --- TDP / POWER ---
	sec = newSection(leftCol)
	powerSec := sec
	// The card holds the TDP limits and, in advanced mode, the undervolt —
	// every power-domain control, with its own actions at the bottom. "TDP"
	// would undersell that.
	sec.Append(sectionLabel("POWER"))

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
	c.tdpBasicScale.SetFocusable(false)
	w.wheelScrollsView(c.tdpBasicScale)
	// Labelled from the scale's own value (the bottom of the device's range
	// until the first sync), never from a number of its own that could fall
	// outside that range.
	c.tdpBasicLabel = gtk.NewLabel(fmt.Sprintf("%d W", int(c.tdpBasicScale.Value())))
	c.tdpBasicLabel.AddCSSClass("scale-value")
	c.tdpBasicScale.ConnectValueChanged(func() {
		c.tdpBasicLabel.SetLabel(fmt.Sprintf("%d W", int(c.tdpBasicScale.Value())))
		c.refreshCommitDirty()
	})
	// Value beside the slider, not centred beneath it: the desktop eye scans
	// a form row, where a touch column would stack for a thumb.
	basicRow := gtk.NewBox(gtk.OrientationHorizontal, 10)
	c.tdpBasicScale.SetHExpand(true)
	basicRow.Append(c.tdpBasicScale)
	basicRow.Append(c.tdpBasicLabel)
	tdpBasicBox.Append(basicRow)
	sec.Append(tdpBasicBox)

	// Advanced box (hidden by default) — replaces basic slider in-place.
	c.tdpAdvancedBox = gtk.NewBox(gtk.OrientationVertical, 4)
	c.tdpAdvancedBox.SetVisible(false)

	c.tdpWarn = gtk.NewLabel(tdpWarningText(w.limits))
	c.tdpWarn.SetWrap(true)
	c.tdpWarn.SetHAlign(gtk.AlignStart)
	c.tdpWarn.AddCSSClass("tdp-warning")
	c.tdpAdvancedBox.Append(c.tdpWarn)

	// Each slider spans its own limit's range: on asus-armoury PL2 and PL3 have
	// higher minimums and maximums than PL1, and PL1's range would offer values
	// the daemon raises or refuses.
	pl2lo, pl2hi := w.limits.PL2Range()
	pl3lo, pl3hi := w.limits.PL3Range()
	c.tdpPL1Scale, c.tdpPL1Label = c.buildTdpScale("PL1 (SPL)", "Sustained power limit — the long-term average power the CPU targets.",
		w.limits.TDPMin, w.limits.TDPMaxForced)
	c.tdpPL2Scale, c.tdpPL2Label = c.buildTdpScale("PL2 (SPPT)", "Short boost — maximum power during brief burst workloads.",
		pl2lo, pl2hi)
	c.tdpPL3Scale, c.tdpPL3Label = c.buildTdpScale("PL3 (FPPT)", "Fast boost — peak instantaneous power for single-threaded spikes.",
		pl3lo, pl3hi)

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

	c.uvCpuScale, c.uvCpuLabel = c.buildUvScale("CPU Curve Optimizer", float64(w.limits.UVMin), float64(w.limits.UVMax))

	// No per-domain save: the page's one commit button carries the offset
	// (customcommit.go), so only the reset is here, as a right-aligned
	// secondary action like the other cards' resets.
	uvBtnRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	uvBtnRow.AddCSSClass("custom-actions")
	uvBtnRow.SetHAlign(gtk.AlignEnd)

	c.resetUvBtn = gtk.NewButtonWithLabel("Reset UV")
	c.resetUvBtn.ConnectClicked(func() { c.resetUndervolt() })
	uvBtnRow.Append(c.resetUvBtn)

	c.uvBox.Append(uvBtnRow)
	c.tdpAdvancedBox.Append(c.uvBox)

	sec.Append(c.tdpAdvancedBox)

	c.tdpAdvancedCheck.ConnectToggled(func() {
		adv := c.tdpAdvancedCheck.Active()
		c.tdpAdvancedBox.SetVisible(adv)
		tdpBasicBox.SetVisible(!adv)
		// The toggle swaps which widgets a commit reads (and whether the
		// undervolt is reachable), so the dirty set can change without any
		// value moving.
		c.refreshCommitDirty()
	})

	// --- FAN CURVE ---
	sec = newSection(rightCol)
	fanSec := sec
	sec.Append(sectionLabel("FAN CURVE"))

	// Presets sit above the chart, and choosing one is an ordinary edit: the
	// points land in the editor, the commit bar goes dirty, and nothing has
	// been written. That is the whole design — a preset is a starting point
	// you can drag before committing, never a mode the profile remembers.
	//
	// The device declares them (rules in internal/limits), so nothing here
	// knows what "quiet" means; a device with none gets no row at all.
	if row, btns := c.buildPresetRow(); row != nil {
		c.presetBtns = btns
		sec.Append(row)
	}

	c.fanCurve = c.newFanCurveEditor()
	sec.Append(c.fanCurve.area)

	// AUTOSWITCH used to sit here, in a card under the fan editor, on the
	// reading that it selects profiles and so belongs with the profile
	// controls. It moved to the dashboard rail (Jeff, 2026-08-14) on a better
	// one: what this page edits is a profile's *contents*, and autoswitch
	// changes which profile the machine runs — a live control, like the
	// firmware profile buttons it picks between, neither of which is on this
	// page either.

	// --- ACTIONS ---
	// The page has exactly one commit button — the bar under the scroll
	// area, built after the scroller below — because per-domain saves beside
	// the profile operations read as "save the settings, then save them again
	// in the profile", a second step that does not exist (Jeff, 2026-08-14;
	// customcommit.go). Resets are not saves — they remove a subsystem from
	// the profile — so each card keeps its own, as a right-aligned secondary
	// action.
	c.resetTdpBtn = gtk.NewButtonWithLabel("Reset TDP")
	c.resetTdpBtn.ConnectClicked(func() { c.resetTdp() })

	c.resetFanBtn = gtk.NewButtonWithLabel("Reset Fans")
	w.setHint(c.resetFanBtn, "Reset fan curves to firmware auto")
	c.resetFanBtn.ConnectClicked(func() { c.resetFanCurve() })

	c.resetNote = blockNote()

	powerRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	powerRow.AddCSSClass("custom-actions")
	powerRow.SetHAlign(gtk.AlignEnd)
	powerRow.Append(c.resetTdpBtn)
	powerSec.Append(powerRow)

	fanRow := gtk.NewBox(gtk.OrientationHorizontal, 4)
	fanRow.AddCSSClass("custom-actions")
	fanRow.SetHAlign(gtk.AlignEnd)
	fanRow.Append(c.resetFanBtn)
	fanSec.Append(fanRow)
	fanSec.Append(c.resetNote)

	// --- DELETE ---
	// Lives in the editor rather than on the profile row: the editor knows its
	// target, and a delete affordance on every row is an accidental tap away
	// from data loss. Two taps stand in for a confirm dialog (no popovers —
	// they do not composite under gamescope); sensitivity mirrors the daemon's
	// refusals via profileui.DeleteBlockFor.
	c.deleteBtn = gtk.NewButtonWithLabel("Delete Profile")
	w.setHint(c.deleteBtn, "Remove this saved profile")
	c.deleteBtn.ConnectClicked(func() { c.deleteProfileClicked() })
	c.deleteNote = blockNote()
	// Delete joins the profile card's action row: it is a profile operation
	// exactly like the three beside it, its two-tap arm and sensitivity
	// guards carry the danger, and a lone card holding one button unbalances
	// the column.
	c.profileActions.Append(c.deleteBtn)
	profileSec.Append(c.deleteNote)

	c.scroll = newDrawerScroll(content)
	view.Append(c.scroll)
	view.Append(c.buildCommitBar())

	c.buildFocusList()
	return c
}

// buildTdpScale creates a labeled TDP slider and appends it to the advanced
// box. Returns the scale and value label.
func (c *customView) buildTdpScale(label, desc string, lo, hi int) (*gtk.Scale, *gtk.Label) {
	w := c.w
	nameLabel := gtk.NewLabel(label)
	nameLabel.SetHAlign(gtk.AlignStart)
	nameLabel.AddCSSClass("scale-name")
	descLabel := gtk.NewLabel(desc)
	descLabel.SetHAlign(gtk.AlignStart)
	descLabel.SetWrap(true)
	descLabel.AddCSSClass("scale-value")
	sc := gtk.NewScaleWithRange(gtk.OrientationHorizontal, float64(lo), float64(hi), 1)
	sc.SetDigits(0)
	sc.SetDrawValue(false)
	sc.SetFocusable(false)
	w.wheelScrollsView(sc)
	valLabel := gtk.NewLabel(fmt.Sprintf("%d W", lo))
	valLabel.AddCSSClass("scale-value")
	sc.ConnectValueChanged(func() {
		valLabel.SetLabel(fmt.Sprintf("%d W", int(sc.Value())))
		c.refreshCommitDirty()
	})
	// Desktop form row: name and value share a header line, description
	// beneath, slider last — the shape every settings app uses.
	head := gtk.NewBox(gtk.OrientationHorizontal, 8)
	head.Append(nameLabel)
	valLabel.SetHAlign(gtk.AlignEnd)
	valLabel.SetHExpand(true)
	head.Append(valLabel)
	c.tdpAdvancedBox.Append(head)
	c.tdpAdvancedBox.Append(descLabel)
	c.tdpAdvancedBox.Append(sc)
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
	// Stock is the top of the range (0 on every device so far); starting there
	// rather than at a literal 0 keeps the value inside whatever the device says.
	sc.SetValue(hi)
	sc.SetFocusable(false)
	c.w.wheelScrollsView(sc)
	valLabel := gtk.NewLabel(uvText(int(hi)))
	valLabel.AddCSSClass("scale-value")
	sc.ConnectValueChanged(func() {
		valLabel.SetLabel(uvText(int(sc.Value())))
		c.refreshCommitDirty()
	})
	// Same desktop form row as buildTdpScale.
	head := gtk.NewBox(gtk.OrientationHorizontal, 8)
	head.Append(nameLabel)
	valLabel.SetHAlign(gtk.AlignEnd)
	valLabel.SetHExpand(true)
	head.Append(valLabel)
	c.uvBox.Append(head)
	c.uvBox.Append(sc)
	return sc, valLabel
}

// uvText formats an undervolt value. The slider's header row already shows
// the name on the same line, so the value stands bare ("-20", "0 (stock)").
func uvText(val int) string {
	if val == 0 {
		return "0 (stock)"
	}
	return fmt.Sprintf("%d", val)
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

// pwmPct renders a PWM value as a rounded percentage of the device's ceiling
// for display. Plain integer division reads 127 (the 50% floor) as "49%".
func pwmPct(pwm, pwmMax int) int {
	return (pwm*100 + pwmMax/2) / pwmMax
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
		// FitCurve refuses a curve sized for other hardware whole: the old
		// copy into a fixed array truncated a longer one and left the
		// previous curve's tail under a shorter one.
		pts, ok := profileui.CurveToShow(plan, es.FanCurve)
		if fitted, fits := w.limits.FitCurve(pts); ok && fits {
			c.fanCurve.points = fitted
		} else {
			c.fanCurve.points = w.limits.DefaultCurve()
		}
		// A curve saved while the floor was off can sit below it once a high
		// TDP is applied. Lift it so what is drawn is what the daemon would
		// accept.
		c.fanCurve.enforceConstraints(0)
		c.fanCurve.area.QueueDraw()
		// The profile's own curve may happen to be a preset — that is how a
		// preset applied earlier still reads as one after a restart.
		c.syncPresetHighlight()
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
		c.uvCpuLabel.SetLabel(uvText(es.CO))
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

	// One-commit model: the widgets now show the target's own
	// values, which is the baseline the commit button measures edits against.
	c.syncCommitBar(plan)
}

// pollTick refreshes what reads the latest state (already in w.state) while
// this view is the visible one: the chart's live temperature marker and the
// fan floor. Deliberately narrower than a full sync: the poll runs every second
// and must not rebuild anything the user could be interacting with.
func (c *customView) pollTick() {
	if c == nil {
		return
	}
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
			c.w.limits.TDPMaxSafe, pwmPct(floorMin, c.w.limits.PWMMax)))
		return
	}
	setBlockNote(c.resetNote, "")
}

// buildFocusList builds the 2D focus grid for the custom profile view.
// Called exactly once, when the view is built: the profile list lives
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
	// visibleButtonLine is buttonLine with a shared visibility guard, for rows
	// that come and go (the inline name entry, the undervolt actions).
	visibleButtonLine := func(vis func() bool, btns ...*gtk.Button) {
		for i, fc := range b.Line(len(btns)) {
			btn := btns[i]
			items = append(items, focusItem{
				widget: btn, row: fc.Row, col: fc.Col, section: fc.Section,
				isVisible:  vis,
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

	nameVis := func() bool { return c.nameRow != nil && c.nameRow.IsVisible() }
	advVis := func() bool { return c.tdpAdvancedBox.IsVisible() }
	uvVis := func() bool { return c.tdpAdvancedBox.IsVisible() && c.uvBox != nil && c.uvBox.IsVisible() }
	var fc focusgrid.Coord

	// The list follows the two-column page in reading order — the PROFILE
	// card (Delete sits with the profile operations there), the POWER card
	// with its own reset, the FAN CURVE card with its own, then the commit
	// bar.
	b.Section("profile")
	fc = b.One()
	items = append(items, focusItem{
		widget: c.selDD.btn, row: fc.Row, col: fc.Col, section: fc.Section,
		onActivate: func() { c.selDD.btn.Activate() },
	})
	buttonLine(c.activateBtn, c.newProfileBtn, c.saveAsBtn, c.deleteBtn)
	visibleButtonLine(nameVis, c.nameOKBtn, c.nameCancelBtn)

	b.Section("power")
	fc = b.One()
	items = append(items, focusItem{
		widget: c.tdpAdvancedCheck, row: fc.Row, col: fc.Col, section: fc.Section,
		onActivate: func() { c.tdpAdvancedCheck.SetActive(!c.tdpAdvancedCheck.Active()) },
	})
	sliderLine(c.tdpBasicScale, 5, func() bool { return c.tdpBasicScale.IsVisible() })
	for _, sc := range []*gtk.Scale{c.tdpPL1Scale, c.tdpPL2Scale, c.tdpPL3Scale} {
		sliderLine(sc, 1, advVis)
	}
	sliderLine(c.uvCpuScale, 1, uvVis)
	visibleButtonLine(uvVis, c.resetUvBtn)
	buttonLine(c.resetTdpBtn)

	b.Section("fan")
	// The preset row is above the chart on screen, so it is above it here:
	// a focus order that disagrees with the reading order is the failure
	// nobody notices with a pointer in their hand.
	if len(c.presetBtns) > 0 {
		buttonLine(c.presetBtns...)
	}
	fc = b.One()
	items = append(items, focusItem{
		widget: c.fanCurve.area, row: fc.Row, col: fc.Col, section: fc.Section,
	})
	buttonLine(c.resetFanBtn)

	b.Section("commit")
	// Same line as Commit because they share the bar. A focus line that did
	// not match the visual row is the thing the D-pad user cannot see.
	buttonLine(c.resetAllBtn, c.commitBtn)

	items = append(items, c.host.errBar.focusItem())
	logFocusList(c.host.focusName("custom"), items)
	c.focusItems = items
}

// Window-level entry points. Each nil-guards the view, which is built lazily
// on first navigation.

// customViews returns every custom profile editor that has been built.
//
// There is at most one today — the full window's — since the drawer stopped
// carrying an editor and switches profiles from a picker instead. It stays a
// slice rather than collapsing to a pointer because that is the shape every
// caller wants (a loop that does nothing when nothing is built), and because
// "every editor there is" is the property the callers actually depend on: both
// would edit the same daemon state, so anything that refreshes one has to
// refresh all of them. A stale second editor showing a profile's old power
// limits is exactly the kind of thing nobody notices until they save from it.
func (w *Window) customViews() []*customView {
	out := make([]*customView, 0, 1)
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

// tdpWarningText is the advanced view's warning, naming the device's safe
// maximum.
func tdpWarningText(l limits.Limits) string {
	return fmt.Sprintf("WARNING: Values above %dW may cause thermal throttling, instability, or hardware damage. Use at your own risk — we are not responsible for any damages.",
		l.TDPMaxSafe)
}

// applyLimits moves every bound this editor took from w.limits at
// construction onto the current value, in place — the slider ranges, the
// warning's safe maximum, and the fan chart (which reads its bounds and floor
// on every draw, so a repaint is all it needs). The caller holds w.syncing:
// SetRange clamps a value that falls outside the new range, and the
// value-changed handlers it fires must not read as edits. The values
// themselves come back from the sync that follows.
func (c *customView) applyLimits() {
	l := c.w.limits
	c.tdpBasicScale.SetRange(float64(l.TDPMin), float64(l.BasicSliderMax()))
	c.tdpPL1Scale.SetRange(float64(l.TDPMin), float64(l.TDPMaxForced))
	lo, hi := l.PL2Range()
	c.tdpPL2Scale.SetRange(float64(lo), float64(hi))
	lo, hi = l.PL3Range()
	c.tdpPL3Scale.SetRange(float64(lo), float64(hi))
	c.uvCpuScale.SetRange(float64(l.UVMin), float64(l.UVMax))
	c.tdpWarn.SetLabel(tdpWarningText(l))
	if c.fanCurve != nil {
		// A curve of the old length cannot be edited or sent against the new
		// shape; the placeholder is honest until the next sync adopts the
		// daemon's curve again.
		if len(c.fanCurve.points) != l.Points {
			c.fanCurve.points = l.DefaultCurve()
			c.fanCurve.enforceConstraints(0)
			c.syncPresetHighlight()
		}
		c.fanCurve.area.QueueDraw()
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
