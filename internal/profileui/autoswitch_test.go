// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"reflect"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

func TestAutoswitchNilSafe(t *testing.T) {
	if got := profileui.Autoswitch(nil); got.Enabled || got.AC != "" || got.Battery != "" {
		t.Errorf("Autoswitch(nil) = %+v, want the zero value", got)
	}
	s := stateWith("balanced")
	s.Autoswitch = &api.AutoswitchState{Enabled: true, AC: "performance", Battery: "quiet"}
	if got := profileui.Autoswitch(s); !got.Enabled || got.AC != "performance" || got.Battery != "quiet" {
		t.Errorf("Autoswitch = %+v", got)
	}
}

func TestTargetOptions(t *testing.T) {
	s := stateWith("balanced",
		api.CustomProfile{Name: "gaming", TDP: tdp(60)},
		api.CustomProfile{Name: "hollow"}, // empty: a target that could never activate
	)
	got := profileui.TargetOptions(s)
	want := []string{profileui.LeaveAlone, "quiet", "balanced", "performance", "gaming"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TargetOptions = %v, want %v (empty profiles are traps, not options)", got, want)
	}

	// Populated "custom" joins the options.
	s2 := stateWith("balanced", api.CustomProfile{Name: "custom", TDP: tdp(50)})
	got2 := profileui.TargetOptions(s2)
	want2 := []string{profileui.LeaveAlone, "quiet", "balanced", "performance", "custom"}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("TargetOptions = %v, want %v", got2, want2)
	}
}

func TestTargetLabel(t *testing.T) {
	if got := profileui.TargetLabel(profileui.LeaveAlone); got != "(don't change)" {
		t.Errorf("TargetLabel(leave alone) = %q", got)
	}
	if got := profileui.TargetLabel("quiet"); got != "Quiet" {
		t.Errorf("TargetLabel(quiet) = %q", got)
	}
	if got := profileui.TargetLabel("gaming"); got != "gaming" {
		t.Errorf("TargetLabel(gaming) = %q", got)
	}
}

func TestPowerLabel(t *testing.T) {
	if got := profileui.PowerLabel(nil); got != "" {
		t.Errorf("PowerLabel(nil) = %q, want \"\"", got)
	}
	// OnAC false with SourceKnown false is "no mains supply to read", not
	// battery — a pre-2.0 daemon, a VM, a desktop. Claiming Battery there is
	// the exact mistake SourceKnown exists to prevent.
	unknown := &api.State{OnAC: false, SourceKnown: false}
	if got := profileui.PowerLabel(unknown); got != "" {
		t.Errorf("PowerLabel(unknown source) = %q, want \"\"", got)
	}
	ac := &api.State{OnAC: true, SourceKnown: true}
	if got := profileui.PowerLabel(ac); got != "AC" {
		t.Errorf("PowerLabel(AC) = %q", got)
	}
	batt := &api.State{OnAC: false, SourceKnown: true}
	if got := profileui.PowerLabel(batt); got != "Battery" {
		t.Errorf("PowerLabel(battery) = %q", got)
	}
}

func TestTargetRowsShowEmptyProfilesGreyed(t *testing.T) {
	s := stateWith("balanced",
		api.CustomProfile{Name: "gaming", TDP: tdp(60)},
		api.CustomProfile{Name: "hollow"}, // empty: shown, marked, not selectable
	)
	rows := profileui.TargetRows(s)

	want := []profileui.TargetRow{
		{Name: profileui.LeaveAlone, Label: "(don't change)"},
		{Name: "quiet", Label: "Quiet"},
		{Name: "balanced", Label: "Balanced"},
		{Name: "performance", Label: "Performance"},
		// The default profile is always addressable, so it is always listed —
		// greyed here, since this state gives it no settings.
		{Name: "custom", Label: "Custom (empty)", Empty: true},
		// User-typed names display as stored; only the built-ins capitalize.
		{Name: "gaming", Label: "gaming"},
		{Name: "hollow", Label: "hollow (empty)", Empty: true},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("TargetRows = %v, want %v (empty profiles are shown greyed, not hidden)", rows, want)
	}

	// TargetOptions is TargetRows minus the unusable rows; the two must agree
	// or the widget could offer a name the daemon refuses at every transition.
	opts := profileui.TargetOptions(s)
	for _, o := range opts {
		if o == "hollow" {
			t.Errorf("TargetOptions offers the empty profile: %v", opts)
		}
	}
}
