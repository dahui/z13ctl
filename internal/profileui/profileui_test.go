// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

// tdp returns a minimal non-empty TDP so a profile counts as having settings.
func tdp(pl1 int) *api.TDPState {
	return &api.TDPState{PL1SPL: pl1, PL2SPPT: pl1, FPPT: pl1}
}

// stateWith builds a state with the given active profile and saved profiles.
func stateWith(active string, profiles ...api.CustomProfile) *api.State {
	s := &api.State{Profile: active}
	if len(profiles) > 0 {
		s.CustomProfiles = make(map[string]api.CustomProfile, len(profiles))
		for _, p := range profiles {
			s.CustomProfiles[p.Name] = p
		}
	}
	return s
}

func rowByName(t *testing.T, rows []profileui.Row, name string) profileui.Row {
	t.Helper()
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no row named %q in %v", name, rows)
	return profileui.Row{}
}

func TestStockRows(t *testing.T) {
	rows := profileui.StockRows(nil)
	want := []string{"quiet", "balanced", "performance"}
	if len(rows) != len(want) {
		t.Fatalf("StockRows(nil) = %d rows, want %d", len(rows), len(want))
	}
	for i, name := range want {
		if rows[i].Name != name {
			t.Errorf("row %d = %q, want %q", i, rows[i].Name, name)
		}
		if rows[i].Active {
			t.Errorf("row %q active with no state", name)
		}
		if rows[i].ActivateBlock != "" {
			t.Errorf("firmware profile %q blocked: %q", name, rows[i].ActivateBlock)
		}
		if rows[i].DeleteBlock == "" {
			t.Errorf("firmware profile %q reported deletable", name)
		}
	}
	if !rowByName(t, profileui.StockRows(stateWith("balanced")), "balanced").Active {
		t.Error("active firmware profile not marked Active")
	}
}

func TestCustomRowsOrderAndKinds(t *testing.T) {
	// Nil state still offers "custom": it is addressable before it has ever
	// been populated.
	if rows := profileui.CustomRows(nil); len(rows) != 1 || rows[0].Name != "custom" {
		t.Fatalf("CustomRows(nil) = %v, want just custom", rows)
	}

	s := stateWith("balanced",
		api.CustomProfile{Name: "gaming", TDP: tdp(60)},
		api.CustomProfile{Name: "cool", TDP: tdp(30)},
	)
	rows := profileui.CustomRows(s)
	wantOrder := []string{"custom", "cool", "gaming"}
	if len(rows) != len(wantOrder) {
		t.Fatalf("got %d rows, want %d", len(rows), len(wantOrder))
	}
	for i, name := range wantOrder {
		if rows[i].Name != name {
			t.Errorf("row %d = %q, want %q (custom first, then sorted)", i, rows[i].Name, name)
		}
	}
	if got := rowByName(t, rows, "custom").Kind; got != profileui.DefaultCustom {
		t.Errorf("custom Kind = %v, want DefaultCustom", got)
	}
	if got := rowByName(t, rows, "gaming").Kind; got != profileui.Named {
		t.Errorf("gaming Kind = %v, want Named", got)
	}
	// The firmware profiles never appear among the custom rows — that
	// separation is what keeps the main view to four controls.
	for _, r := range rows {
		if r.Kind == profileui.Stock {
			t.Errorf("firmware profile %q leaked into CustomRows", r.Name)
		}
	}
}

func TestActivateBlock(t *testing.T) {
	s := stateWith("gaming",
		api.CustomProfile{Name: "gaming", TDP: tdp(60)},
		api.CustomProfile{Name: "empty"}, // created, never populated
		api.CustomProfile{Name: "cool", TDP: tdp(30)},
	)
	cases := []struct {
		name    string
		blocked bool
	}{
		{"balanced", false}, // firmware profiles are always activatable
		{"cool", false},
		{"gaming", true}, // already running
		{"empty", true},  // nothing to apply
		{"custom", true}, // never populated
	}
	for _, tt := range cases {
		got := profileui.ActivateBlock(s, tt.name)
		if tt.blocked && got == "" {
			t.Errorf("%s: activatable, want a block reason", tt.name)
		}
		if !tt.blocked && got != "" {
			t.Errorf("%s: blocked with %q, want activatable", tt.name, got)
		}
	}
	// A firmware profile stays activatable even when it is the active one:
	// re-selecting it is how a user leaves a custom profile.
	if got := profileui.ActivateBlock(stateWith("balanced"), "balanced"); got != "" {
		t.Errorf("active firmware profile blocked: %q", got)
	}
}

func TestCustomSummary(t *testing.T) {
	if got := profileui.Custom(nil); got.Active || got.Label != "Custom" {
		t.Errorf("Custom(nil) = %+v, want inactive \"Custom\"", got)
	}
	if got := profileui.Custom(stateWith("balanced")); got.Active || got.Label != "Custom" {
		t.Errorf("on a firmware profile: %+v, want inactive \"Custom\"", got)
	}
	// The main view has no profile list any more, so the button is the only
	// thing that can say which custom profile is running.
	s := stateWith("gaming", api.CustomProfile{Name: "gaming", TDP: tdp(60)})
	if got := profileui.Custom(s); !got.Active || got.Label != "gaming" {
		t.Errorf("with gaming active: %+v, want active \"gaming\"", got)
	}
}

func TestDefaultEditTarget(t *testing.T) {
	if got := profileui.DefaultEditTarget(nil); got != "custom" {
		t.Errorf("DefaultEditTarget(nil) = %q, want \"custom\"", got)
	}
	if got := profileui.DefaultEditTarget(stateWith("balanced")); got != "custom" {
		t.Errorf("on a firmware profile = %q, want \"custom\"", got)
	}
	s := stateWith("gaming", api.CustomProfile{Name: "gaming", TDP: tdp(60)})
	if got := profileui.DefaultEditTarget(s); got != "gaming" {
		t.Errorf("with gaming active = %q, want \"gaming\"", got)
	}
}

func TestDeleteBlocks(t *testing.T) {
	s := stateWith("gaming",
		api.CustomProfile{Name: "gaming", TDP: tdp(60)},
		api.CustomProfile{Name: "ac-prof", TDP: tdp(40)},
		api.CustomProfile{Name: "batt-prof", TDP: tdp(20)},
		api.CustomProfile{Name: "free", TDP: tdp(30)},
	)
	s.Autoswitch = &api.AutoswitchState{Enabled: true, AC: "ac-prof", Battery: "batt-prof"}

	cases := []struct {
		name      string
		deletable bool
	}{
		{"gaming", false},    // active
		{"ac-prof", false},   // autoswitch AC target
		{"batt-prof", false}, // autoswitch battery target
		{"custom", false},    // nothing saved under it
		{"free", true},
	}
	for _, tt := range cases {
		got := profileui.DeleteBlockFor(s, tt.name)
		if tt.deletable && got != "" {
			t.Errorf("%s: DeleteBlock = %q, want deletable", tt.name, got)
		}
		if !tt.deletable && got == "" {
			t.Errorf("%s: deletable, want a block reason", tt.name)
		}
	}
}

func TestSavedCustomIsDeletable(t *testing.T) {
	// "custom" is only special while unpopulated: once saved and not active or
	// referenced, it deletes like any named profile.
	s := stateWith("balanced", api.CustomProfile{Name: "custom", TDP: tdp(50)})
	if got := profileui.DeleteBlockFor(s, "custom"); got != "" {
		t.Errorf("saved, inactive custom: DeleteBlock = %q, want deletable", got)
	}
}

func TestLabel(t *testing.T) {
	cases := map[string]string{
		"quiet":       "Quiet",
		"balanced":    "Balanced",
		"performance": "Performance",
		"custom":      "Custom",
		"gaming":      "gaming",     // named: verbatim
		"my-profile":  "my-profile", // no title-casing an identifier
	}
	for in, want := range cases {
		if got := profileui.Label(in); got != want {
			t.Errorf("Label(%q) = %q, want %q", in, got, want)
		}
	}
}
