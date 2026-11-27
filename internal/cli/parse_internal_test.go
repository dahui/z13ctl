package cli

// parse_internal_test.go — in-package tests for TDP flag resolution, and the
// profile-name invariants the safety engine relies on (the rules are
// api.ValidateProfileName's; fan-curve parsing moved to internal/safety).

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

// TestNoStockProfileNameIsAcceptedAsCustom is the invariant the safety engine's
// ReadEffective silently relies on: it treats any name absent from the
// envelope's stock table as custom and disables its stale-5W fallback, so a
// custom profile named "balanced" would make the machine misreport its power
// limits. (The companion data test in internal/device checks the envelope's
// own table keys against the same reservation.)
func TestNoStockProfileNameIsAcceptedAsCustom(t *testing.T) {
	for _, name := range api.KernelProfileNames {
		if !api.IsReservedProfileName(name) {
			t.Errorf("api.IsReservedProfileName(%q) = false, want true", name)
		}
		if err := api.ValidateProfileName(name); err == nil {
			t.Errorf("api.ValidateProfileName(%q) = nil, want an error — a custom profile may not shadow a firmware profile", name)
		}
		var s api.State
		if s.IsCustomProfile(name) {
			t.Errorf("State.IsCustomProfile(%q) = true, want false", name)
		}
	}
}

// TestIsCustomProfileIgnoresAShadowingMapEntry covers the hand-edited state
// file: even with a "balanced" entry in the profile map, the firmware profile
// must win.
func TestIsCustomProfileIgnoresAShadowingMapEntry(t *testing.T) {
	s := api.State{CustomProfiles: map[string]api.CustomProfile{
		"balanced": {Name: "balanced"},
		"gaming":   {Name: "gaming"},
	}}
	if s.IsCustomProfile("balanced") {
		t.Error("IsCustomProfile(\"balanced\") = true, want false even with a map entry of that name")
	}
	if !s.IsCustomProfile("gaming") {
		t.Error("IsCustomProfile(\"gaming\") = false, want true")
	}
	if !s.IsCustomProfile(api.DefaultCustomProfile) {
		t.Error("IsCustomProfile(\"custom\") = false, want true even with no map entry")
	}
	if s.IsCustomProfile("never-saved") {
		t.Error("IsCustomProfile(\"never-saved\") = true, want false")
	}
}
