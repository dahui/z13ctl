// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// edit.go — how the custom-profile editor addresses a target profile, and
// which values it displays for it.

import (
	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/limits"
)

// EditPlan says how the editor must send edits for a target profile.
//
// Live means bare sends: the edit applies to the running machine, exactly as
// `voltaire tdp --set` with no --profile does. Not-live means the ...For send
// variants carrying the profile name: the edit is stored in that profile and
// no hardware is touched — which is the only correct way to edit a profile
// that is not running.
type EditPlan struct {
	Target string // the profile being edited; never empty
	Live   bool   // true when edits apply to hardware via bare sends
}

// WireProfile returns the value for the api ...For functions' profile
// parameter: empty for a live edit (a bare send), the target name otherwise.
func (p EditPlan) WireProfile() string {
	if p.Live {
		return ""
	}
	return p.Target
}

// PlanEdit decides how the editor must address target given the active
// profile. A bare send edits the active profile — and, made while a firmware
// profile is active, creates and activates "custom" — so the plan is live in
// exactly two cases:
//
//   - target is the active profile: editing what is running is a live edit by
//     definition.
//   - target is "custom" while a firmware profile is active: a bare send has
//     the create-and-activate semantics the drawer's Custom editor has always
//     had, and preserving it here keeps that flow byte-identical on old
//     daemons that do not understand the profile field at all.
//
// Every other target — a named profile that is not running, or "custom" while
// a *named* profile is running — must go through the ...For variants: a bare
// send there would edit the wrong profile, and worse, apply it to hardware.
func PlanEdit(s *api.State, target string) EditPlan {
	if s != nil && s.Profile == target {
		return EditPlan{Target: target, Live: true}
	}
	if target == api.DefaultCustomProfile && (s == nil || !s.InCustomProfile()) {
		return EditPlan{Target: target, Live: true}
	}
	return EditPlan{Target: target, Live: false}
}

// EditorState is everything the custom view needs to display one target.
type EditorState struct {
	TDP       *api.TDPState
	FanCurve  *api.FanCurveState
	Undervolt *api.UndervoltState

	// FloorPL1 is the sustained limit the fan floor must be evaluated
	// against for this target. For a live edit that is the applied hardware
	// limit; for a stored edit it is the profile's own saved TDP — hardware
	// says nothing about a profile that is not running, and the daemon's
	// edit-time checks (CheckCurveAgainstTDP, CheckFanFloorReleaseAt) use the
	// profile's own limit for exactly that reason. Zero means no limit is
	// known, which the limits package reads as "no floor".
	FloorPL1 int

	// CO is the undervolt slider position. For a live edit it is the applied
	// offset — shown as 0 while a stock profile is active, since CO is reset
	// in hardware there; for a stored edit it is the profile's saved offset.
	CO int

	// HasTDP feeds limits.NeedsAdvanced's isCustom parameter: whether the
	// displayed TDP is a custom one that could require the advanced sliders.
	HasTDP bool
}

// ForEditor returns the values the editor should display for plan's target.
//
// A live edit displays the daemon's projected state — s.TDP, s.FanCurve and
// s.Undervolt are the active custom profile's settings (or, on a firmware
// profile, the default "custom" profile's: the same values a bare send would
// edit), with TDP and fan curve read back from hardware. A stored edit
// displays the target profile's saved settings and nothing else; nil fields
// mean the profile does not control that subsystem.
func ForEditor(s *api.State, plan EditPlan) EditorState {
	if s == nil {
		return EditorState{}
	}
	if plan.Live {
		es := EditorState{
			TDP:       s.TDP,
			FanCurve:  s.FanCurve,
			Undervolt: s.Undervolt,
			HasTDP:    s.InCustomProfile(),
		}
		if s.TDP != nil {
			es.FloorPL1 = s.TDP.PL1SPL
		}
		if s.Undervolt != nil && s.InCustomProfile() {
			es.CO = s.Undervolt.CPUCO
		}
		return es
	}
	p := s.CustomProfiles[plan.Target]
	es := EditorState{
		TDP:       p.TDP,
		FanCurve:  p.FanCurve,
		Undervolt: p.Undervolt,
		HasTDP:    p.TDP != nil,
	}
	if p.TDP != nil {
		es.FloorPL1 = p.TDP.PL1SPL
	}
	if p.Undervolt != nil {
		es.CO = p.Undervolt.CPUCO
	}
	return es
}

// CurveToShow returns the fan-curve points the editor should adopt, or false
// when it should fall back to the default placeholder curve.
//
// The rule differs by plan, and the difference is the point. Live: only a
// curve that is actually in force (limits.FanCurveIsCustom — the mode says the
// hardware is following it) is worth adopting; the curve registers keep the
// last written points after a release, so the points alone prove nothing.
// Stored: there is no hardware to consult — the profile either saves a curve
// or it does not, and a saved curve's mode field is bookkeeping, not evidence.
func CurveToShow(plan EditPlan, fc *api.FanCurveState) ([]api.FanCurvePoint, bool) {
	if plan.Live {
		if limits.FanCurveIsCustom(fc) {
			return fc.Points, true
		}
		return nil, false
	}
	if fc != nil && len(fc.Points) > 0 {
		return fc.Points, true
	}
	return nil, false
}
