// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

func TestSavedRows(t *testing.T) {
	tdp := &api.TDPState{PL1SPL: 20}
	s := &api.State{Profile: "balanced", CustomProfiles: map[string]api.CustomProfile{
		"handheld": {Name: "handheld", TDP: tdp},
	}}
	rows := profileui.SavedRows(s, "balanced")
	if len(rows) < 2 || rows[0].Name != "balanced" || rows[0].Label != profileui.PinnedLabel || !rows[0].Active {
		t.Fatalf("rows = %+v, want No profile first and running", rows)
	}
	if profileui.SavedLabel(s) != profileui.PinnedLabel {
		t.Errorf("label = %q on the TDP setting", profileui.SavedLabel(s))
	}
	s.Profile = "handheld"
	rows = profileui.SavedRows(s, "balanced")
	if rows[0].Active {
		t.Error("No profile marked running while handheld runs")
	}
	found := false
	for _, r := range rows {
		if r.Name == "handheld" {
			found = r.Active && !r.Disabled
		}
	}
	if !found {
		t.Errorf("handheld not offered as the running row: %+v", rows)
	}
	if profileui.SavedLabel(s) != "handheld" {
		t.Errorf("label = %q, want handheld", profileui.SavedLabel(s))
	}
}
