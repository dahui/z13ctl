// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

func TestTDPSlider(t *testing.T) {
	set := func(w int) *api.TDPState { return &api.TDPState{PL1SPL: w, PL2SPPT: w, FPPT: w} }
	tests := []struct {
		name  string
		state *api.State
		value int
		ok    bool
		note  string // a substring; "" means no note
	}{
		{"no daemon", nil, 0, false, ""},
		{"firmware preset", &api.State{Profile: "balanced", TDP: set(80)}, 80, true, "firmware's preset"},
		{"remembered, on AC", &api.State{Profile: "balanced", TDP: set(30), DeviceTDP: set(30), OnAC: true, SourceKnown: true}, 30, true, ""},
		{"remembered above the battery cap", &api.State{Profile: "balanced", TDP: set(55), DeviceTDP: set(70), SourceKnown: true}, 55, true, "Set to 70 W; 55 W"},
		{"source unknown claims no cap", &api.State{Profile: "balanced", TDP: set(55), DeviceTDP: set(70)}, 55, true, ""},
		{"custom profile running", &api.State{Profile: "gaming", TDP: set(20), DeviceTDP: set(30),
			CustomProfiles: map[string]api.CustomProfile{"gaming": {Name: "gaming", TDP: set(20)}}}, 20, true, "Moving the slider leaves it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := profileui.TDPSliderValue(tt.state)
			if v != tt.value || ok != tt.ok {
				t.Errorf("value = %d, %v; want %d, %v", v, ok, tt.value, tt.ok)
			}
			note := profileui.TDPSliderNote(tt.state)
			if (tt.note == "") != (note == "") || !strings.Contains(note, tt.note) {
				t.Errorf("note = %q, want one containing %q", note, tt.note)
			}
		})
	}
}

// A pinned profile is the device's default mode, not a choice among one.
func TestPinnedFirmwareIsOneDefaultRow(t *testing.T) {
	fw := profileui.FirmwareFrom(&api.DeviceInfo{Profiles: &api.ProfileInfo{
		Names: []string{"low-power", "balanced"}, Pin: "balanced",
		Entries: []api.ProfileEntry{{Name: "balanced", Label: "Balanced"}},
	}})
	if len(fw) != 1 || fw[0].Name != "balanced" || fw[0].Label != profileui.PinnedLabel {
		t.Errorf("firmware = %+v, want the pin alone, labelled %q", fw, profileui.PinnedLabel)
	}
	if got := profileui.FirmwareFrom(&api.DeviceInfo{Profiles: &api.ProfileInfo{Names: []string{"quiet", "balanced"}}}); len(got) != 2 {
		t.Errorf("unpinned firmware = %+v, want both", got)
	}
}

// On a slider device "custom" is stored like any profile that is not running:
// a bare send there moves the slider.
func TestPlanEditOnASliderDevice(t *testing.T) {
	s := &api.State{Profile: "balanced"}
	if p := profileui.PlanEditOn(s, "custom", true); p.Live || p.WireProfile() != "custom" {
		t.Errorf("slider plan for custom = %+v, want stored", p)
	}
	if p := profileui.PlanEditOn(s, "custom", false); !p.Live {
		t.Errorf("usual plan for custom = %+v, want live (create and activate)", p)
	}
	s.Profile = "custom"
	s.CustomProfiles = map[string]api.CustomProfile{"custom": {Name: "custom", TDP: &api.TDPState{PL1SPL: 20}}}
	// Live, but named: a bare TDP set would leave the profile for the slider.
	if p := profileui.PlanEditOn(s, "custom", true); !p.Live || p.WireProfile() != "custom" {
		t.Errorf("running custom on a slider device = %+v, want live and named", p)
	}
	if p := profileui.PlanEditOn(s, "custom", false); !p.Live || p.WireProfile() != "" {
		t.Errorf("running custom on the usual model = %+v, want a bare live edit", p)
	}
}

func TestSaveAsBlockOnASliderDevice(t *testing.T) {
	s := &api.State{Profile: "balanced"}
	if got := profileui.SaveAsBlock(s, true); !strings.Contains(got, "preset") {
		t.Errorf("nothing set: %q, want the preset refusal", got)
	}
	s.DeviceTDP = &api.TDPState{PL1SPL: 25}
	if got := profileui.SaveAsBlock(s, true); got != "" {
		t.Errorf("TDP set: blocked by %q", got)
	}
	if got := profileui.SaveAsBlock(s, false); got == "" {
		t.Error("without the slider model the TDP setting is not a source")
	}
}
