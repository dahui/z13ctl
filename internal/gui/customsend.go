// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// customsend.go — how the custom view addresses its target over the socket.
//
// Two rules hold this file together, and both have cost a bug when they were
// not followed. The first is threading: every widget read is snapshotted on
// the GTK main thread into a plain value (tdpRequest, a curve string, an edit
// plan) before any goroutine starts, because GTK is not thread-safe and
// reading a scale off the main thread is undefined behaviour rather than
// merely a stale number. The second is targeting: a plan resolved by
// profileui.PlanEdit says whether this edit reaches hardware (bare sends) or
// is only stored against a profile that is not running (the ...For variants),
// and every stored send is gated on probeStoredTarget.

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// editPlan resolves how the view must address its target right now. Computed
// fresh per operation rather than stored: the answer changes underneath an open
// editor when the active profile moves (autoswitch, the CLI, another client).
// Must be called from the GTK main thread; the result is a value, safe to hand
// to a goroutine.
func (c *customView) editPlan() profileui.EditPlan {
	target := c.editProfile
	if target == "" {
		target = api.DefaultCustomProfile
	}
	return profileui.PlanEdit(c.w.state, target)
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
func (c *customView) readTdpRequest() tdpRequest {
	if c.tdpAdvancedCheck != nil && c.tdpAdvancedCheck.Active() {
		pl1v, pl2v, pl3v := c.tdpPL1Scale.Value(), c.tdpPL2Scale.Value(), c.tdpPL3Scale.Value()
		pl1 := fmt.Sprintf("%d", int(pl1v))
		maxPL := int(math.Max(pl1v, math.Max(pl2v, pl3v)))
		return tdpRequest{
			// watts doubles as the base value; the daemon parses it before it looks
			// at the PL fields and rejects the request outright if it is empty.
			watts: pl1,
			pl1:   pl1,
			pl2:   fmt.Sprintf("%d", int(pl2v)),
			pl3:   fmt.Sprintf("%d", int(pl3v)),
			force: c.w.limits.ForceRequired(maxPL),
		}
	}
	return tdpRequest{watts: fmt.Sprintf("%d", int(c.tdpBasicScale.Value()))}
}

// send performs the socket round-trip for the plan's target. Safe to call
// from a goroutine — it holds only plain strings.
func (r tdpRequest) send(plan profileui.EditPlan) error {
	return apiresult.Err(api.SendTdpSetFor(plan.WireProfile(), r.watts, r.pl1, r.pl2, r.pl3, r.force))
}

// readFanCurve snapshots the fan curve as its wire string. Must be called from
// the GTK main thread; returns "" when there is no editor to read.
func (c *customView) readFanCurve() string {
	if c.fanCurve == nil {
		return ""
	}
	return c.fanCurve.curveString()
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

// saveCustomTdp commits only the TDP values.
func (c *customView) saveCustomTdp() {
	w := c.w
	req := c.readTdpRequest() // widget reads stay on the GTK thread
	plan := c.editPlan()      // resolved on the GTK thread; the goroutine gets a value
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
func (c *customView) saveCustomFanCurve() {
	w := c.w
	curve := c.readFanCurve() // widget read stays on the GTK thread
	plan := c.editPlan()
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
func (c *customView) saveCustomBoth() {
	w := c.w
	req := c.readTdpRequest() // widget reads stay on the GTK thread
	curve := c.readFanCurve()
	plan := c.editPlan()
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
func (c *customView) resetTdp() {
	w := c.w
	plan := c.editPlan()
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
func (c *customView) resetFanCurve() {
	w := c.w
	plan := c.editPlan()
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
func (c *customView) saveUndervolt() {
	w := c.w
	cpu := fmt.Sprintf("%d", int(c.uvCpuScale.Value())) // GTK thread
	plan := c.editPlan()
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
func (c *customView) resetUndervolt() {
	w := c.w
	plan := c.editPlan()
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
func (c *customView) deleteProfileClicked() {
	w := c.w
	if !c.deleteArmed {
		c.deleteArmed = true
		c.deleteBtn.SetLabel("Tap again to delete")
		time.AfterFunc(3*time.Second, func() {
			glib.IdleAdd(func() bool {
				c.disarmDelete()
				return false
			})
		})
		return
	}
	name := c.editProfile
	c.disarmDelete()
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
			c.setEditTarget(api.DefaultCustomProfile)
			return false
		})
		w.refreshState()
	}()
}

// disarmDelete returns the delete button to its resting label.
func (c *customView) disarmDelete() {
	c.deleteArmed = false
	if c.deleteBtn != nil {
		c.deleteBtn.SetLabel("Delete Profile")
	}
}
