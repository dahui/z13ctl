// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// customcommit.go — the full window's one-commit model for the Profiles page.
//
// The drawer commits per domain (Save TDP / Save Fans / Save UV): three
// buttons, because a 320px touch column shows one domain at a time and each
// needs its commit within thumb's reach. On the desktop surface, sitting
// beside Activate and Save As, those read as "save the settings, then save
// the profile" — a second step that does not exist, since the daemon has no
// working slot and a send *is* the save into the profile (Jeff, 2026-08-14).
// The window therefore has exactly one commit button, in a bar fixed under
// the scroll area, and this file is its mechanics: a baseline of every
// editable widget captured at the end of each sync, dirty = differs from
// that baseline, and one click that sends only the dirty domains. Everything
// is nil-guarded on commitBtn, which the drawer never builds, so the drawer
// path is untouched by construction.
//
// TDP dirtiness is judged in the *active mode only*: toggling the Advanced
// checkbox swaps which widgets a commit would read, but a toggle with no
// value moved must not read as an edit — and moving PL2 in advanced mode
// then returning to basic means basic semantics, exactly as clicking the
// drawer's Save TDP in basic mode always has.
//
// The label rules (Apply vs Save, the unsaved summary) live in
// internal/profileui, where make test reaches them.

import (
	"fmt"
	"log/slog"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// buildCommitBar builds the window's commit bar: the unsaved-changes
// indicator and the one button that commits every dirty domain. Appended
// below the scroll area so it cannot scroll out of reach while the advanced
// controls are open.
func (c *customView) buildCommitBar() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 8)
	bar.AddCSSClass("commit-bar")

	// Reset All sits at the bar's left edge, away from Commit, because the two
	// are opposites and this one is destructive. It is a whole-profile action
	// like Commit and Delete — the per-card "Reset TDP"/"Reset Fans"/"Reset UV"
	// each remove one subsystem, while this removes all three and lands the
	// machine on balanced — so it belongs in the bar rather than in a card.
	//
	// One daemon command, not three sends: each individual reset has to lower
	// power before releasing the fans, so a button that issued them in sequence
	// would put that ordering in the GUI. See api.SendTuningReset.
	c.resetAllBtn = gtk.NewButtonWithLabel("Reset All")
	c.resetAllBtn.SetHAlign(gtk.AlignStart)
	c.w.setHint(c.resetAllBtn, "Clear the fan curve, power limits and undervolt from this profile")
	c.resetAllBtn.ConnectClicked(func() { c.resetAllTuning() })
	bar.Append(c.resetAllBtn)

	c.commitNote = gtk.NewLabel("")
	c.commitNote.AddCSSClass("scale-value")
	c.commitNote.SetHAlign(gtk.AlignStart)
	c.commitNote.SetVAlign(gtk.AlignCenter)
	c.commitNote.SetHExpand(true)
	c.commitNote.SetVisible(false)
	bar.Append(c.commitNote)

	c.commitBtn = gtk.NewButtonWithLabel(profileui.CommitLabel(true))
	c.commitBtn.AddCSSClass("save-btn")
	// Pinned to the bar's right edge by its own expand+align rather than by
	// the note's HExpand: the note is hidden while nothing is dirty, and a
	// hidden widget expands nothing.
	c.commitBtn.SetHExpand(true)
	c.commitBtn.SetHAlign(gtk.AlignEnd)
	c.commitBtn.SetSensitive(false)
	c.w.setHint(c.commitBtn, "Commit the changed settings to this profile")
	c.commitBtn.ConnectClicked(func() { c.commitChanges() })
	bar.Append(c.commitBtn)
	return bar
}

// syncCommitBar re-labels the commit button for the current plan and
// recaptures the widget baseline. Called at the end of sync, when every
// widget shows the target's own values — which is the definition of "clean".
func (c *customView) syncCommitBar(plan profileui.EditPlan) {
	if c.commitBtn == nil {
		return
	}
	c.commitBtn.SetLabel(profileui.CommitLabel(plan.Live))
	c.baseBasic = int(c.tdpBasicScale.Value())
	c.baseAdv = [3]int{
		int(c.tdpPL1Scale.Value()),
		int(c.tdpPL2Scale.Value()),
		int(c.tdpPL3Scale.Value()),
	}
	c.baseCurve = c.readFanCurve()
	c.baseUv = int(c.uvCpuScale.Value())
	c.baseCaptured = true
	c.applyCommitDirty()
}

// uvEditable reports whether the undervolt slider is part of the page as
// laid out: the daemon offers CO (uvBox) and the advanced controls are open.
// A hidden slider's value is not an edit — it is unreachable.
func (c *customView) uvEditable() bool {
	return c.uvCpuScale != nil && c.uvBox != nil &&
		c.uvBox.Visible() && c.tdpAdvancedBox.Visible()
}

// commitDirty reports which domains differ from the baseline. GTK main
// thread only — it reads widgets.
func (c *customView) commitDirty() (tdp, fans, uv bool) {
	if !c.baseCaptured {
		return false, false, false
	}
	if c.tdpAdvancedCheck.Active() {
		tdp = [3]int{
			int(c.tdpPL1Scale.Value()),
			int(c.tdpPL2Scale.Value()),
			int(c.tdpPL3Scale.Value()),
		} != c.baseAdv
	} else {
		tdp = int(c.tdpBasicScale.Value()) != c.baseBasic
	}
	fans = c.readFanCurve() != c.baseCurve
	uv = c.uvEditable() && int(c.uvCpuScale.Value()) != c.baseUv
	return tdp, fans, uv
}

// refreshCommitDirty re-evaluates the commit button after a widget edit. It
// is wired into every value-changed handler, so it must no-op in the drawer
// (no commit button) and during sync, whose programmatic SetValue calls fire
// those same handlers before the baseline is recaptured.
func (c *customView) refreshCommitDirty() {
	if c.commitBtn == nil || !c.baseCaptured || c.w.syncing {
		return
	}
	c.applyCommitDirty()
}

// applyCommitDirty sets the button and indicator from the current dirty set.
func (c *customView) applyCommitDirty() {
	tdp, fans, uv := c.commitDirty()
	summary := profileui.UnsavedSummary(tdp, fans, uv)
	c.commitBtn.SetSensitive(summary != "")
	c.commitNote.SetLabel(summary)
	c.commitNote.SetVisible(summary != "")
}

// commitChanges commits every dirty domain to the plan's target in one
// operation: TDP first, then the fan curve, then the undervolt — the order
// saveCustomBoth established, because a rejected TDP is usually why a fan
// write failed too. Each dirty domain is attempted even when an earlier one
// failed; the first error by that priority is reported. The refreshState at
// the end re-syncs the widgets from daemon state, which recaptures the
// baseline and disarms the button.
func (c *customView) commitChanges() {
	w := c.w
	tdpDirty, fanDirty, uvDirty := c.commitDirty()
	if !tdpDirty && !fanDirty && !uvDirty {
		return
	}
	req := c.readTdpRequest() // widget reads stay on the GTK thread
	curve := c.readFanCurve()
	uv := fmt.Sprintf("%d", int(c.uvCpuScale.Value()))
	plan := c.editPlan()
	go func() {
		if err := probeStoredTarget(plan); err != nil {
			w.reportError("Apply changes", err)
			return
		}
		var tdpErr, fanErr, uvErr error
		if tdpDirty {
			tdpErr = req.send(plan)
		}
		if fanDirty {
			fanErr = sendFanCurve(plan, curve)
		}
		if uvDirty {
			uvErr = apiresult.Err(api.SendUndervoltSetFor(plan.WireProfile(), uv))
		}
		switch {
		case tdpErr != nil:
			w.reportError("Apply TDP", tdpErr)
		case fanErr != nil:
			w.reportError("Apply fan curve", fanErr)
		case uvErr != nil:
			w.reportError("Apply undervolt", uvErr)
		default:
			w.clearErrorAsync()
			slog.Info("profile changes committed",
				"profile", plan.Target, "live", plan.Live,
				"tdp", tdpDirty, "fans", fanDirty, "uv", uvDirty)
		}
		w.refreshState()
	}()
}
