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

func TestCycleTarget(t *testing.T) {
	opts := []string{"", "quiet", "balanced"}
	cases := []struct {
		cur  string
		dir  int
		want string
	}{
		{"", +1, "quiet"},
		{"quiet", +1, "balanced"},
		{"balanced", +1, ""}, // wraps
		{"", -1, "balanced"}, // wraps backward
		{"quiet", -1, ""},
		// A stale target (profile emptied, hand-edited state) lands on the
		// safe first option instead of being unreachable.
		{"ghost", +1, ""},
	}
	for _, tt := range cases {
		if got := profileui.CycleTarget(opts, tt.cur, tt.dir); got != tt.want {
			t.Errorf("CycleTarget(%q, %+d) = %q, want %q", tt.cur, tt.dir, got, tt.want)
		}
	}
	if got := profileui.CycleTarget(nil, "x", 1); got != "x" {
		t.Errorf("CycleTarget with no options moved to %q", got)
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
