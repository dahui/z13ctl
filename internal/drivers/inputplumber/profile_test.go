package inputplumber

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// oxpDefault is GetProfileYaml's answer on a OneXPlayer X2 Mini Pro running
// InputPlumber 0.84.0's default profile, captured 2026-11-17.
func oxpDefault(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/oxp-x2-mini-pro-default.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// generic decodes a profile for comparison, ignoring formatting.
func generic(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatalf("profile does not parse: %v\n%s", err, b)
	}
	return v
}

// mappings returns the profile's mapping list, decoded.
func mappings(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var p struct {
		Mapping []map[string]any `yaml:"mapping"`
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p.Mapping
}

// targetsFor returns the target events of every mapping on a gamepad button.
func targetsFor(t *testing.T, b []byte, button string) [][]any {
	t.Helper()
	var out [][]any
	for _, m := range mappings(t, b) {
		src, _ := m["source_event"].(map[string]any)
		gp, _ := src["gamepad"].(map[string]any)
		if gp["button"] == button {
			te, _ := m["target_events"].([]any)
			out = append(out, te)
		}
	}
	return out
}

func TestSetMappingTakesTheCapabilityAndGivesItBack(t *testing.T) {
	orig := oxpDefault(t)

	// The Keyboard button (QuickAccess2), which the default maps to Screenshot.
	out, changed, err := SetMapping(orig, "QuickAccess2", Action("keyboard"))
	if err != nil || !changed {
		t.Fatalf("SetMapping = changed %v, err %v", changed, err)
	}
	got := targetsFor(t, out, "QuickAccess2")
	want := [][]any{{map[string]any{"dbus": "voltaire_keyboard"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("QuickAccess2 targets = %v, want only voltaire's action %v", got, want)
	}
	if n, m := len(mappings(t, out)), len(mappings(t, orig)); n != m {
		t.Errorf("mapping count %d, want %d: one replaced, none added or lost", n, m)
	}
	if !reflect.DeepEqual(targetsFor(t, out, "Keyboard"), targetsFor(t, orig, "Keyboard")) {
		t.Error("an unrelated mapping changed")
	}

	// Loading it again changes nothing, so a re-check never reloads.
	if again, rechanged, reErr := SetMapping(out, "QuickAccess2", Action("keyboard")); reErr != nil || rechanged || !bytesEqual(again, out) {
		t.Errorf("second SetMapping: changed %v, err %v", rechanged, reErr)
	}

	// Removing it puts Screenshot back where it was.
	back, changed, err := SetMapping(out, "", "")
	if err != nil || !changed {
		t.Fatalf("removal: changed %v, err %v", changed, err)
	}
	if !reflect.DeepEqual(generic(t, back), generic(t, orig)) {
		t.Errorf("removal did not restore the profile:\n%s\nwant\n%s", back, orig)
	}
}

func TestSetMappingMovesBetweenButtons(t *testing.T) {
	orig := oxpDefault(t)
	kb, _, err := SetMapping(orig, "QuickAccess2", Action("keyboard"))
	if err != nil {
		t.Fatal(err)
	}
	// The user picks the OneXPlayer button (QuickAccess), which the default
	// profile does not map at all.
	oxp, changed, err := SetMapping(kb, "QuickAccess", Action("oxp"))
	if err != nil || !changed {
		t.Fatalf("SetMapping = changed %v, err %v", changed, err)
	}
	if !reflect.DeepEqual(targetsFor(t, oxp, "QuickAccess2"), targetsFor(t, orig, "QuickAccess2")) {
		t.Error("moving off the Keyboard button did not give Screenshot back")
	}
	if got := targetsFor(t, oxp, "QuickAccess"); !reflect.DeepEqual(got, [][]any{{map[string]any{"dbus": "voltaire_oxp"}}}) {
		t.Errorf("QuickAccess targets = %v", got)
	}
	if n := len(mappings(t, oxp)); n != len(mappings(t, orig))+1 {
		t.Errorf("mapping count %d, want one added", n)
	}
	back, _, err := SetMapping(oxp, "", "")
	if err != nil || !reflect.DeepEqual(generic(t, back), generic(t, orig)) {
		t.Errorf("removal did not restore the profile (err %v)", err)
	}
}

func TestSetMappingRemovingNothingChangesNothing(t *testing.T) {
	orig := oxpDefault(t)
	out, changed, err := SetMapping(orig, "", "")
	if err != nil || changed || !bytesEqual(out, orig) {
		t.Errorf("SetMapping(remove) on a profile without ours: changed %v, err %v", changed, err)
	}
}

// A profile carries fields voltaire knows nothing about; they survive.
func TestSetMappingKeepsUnknownFields(t *testing.T) {
	p := []byte(`version: 1
kind: DeviceProfile
name: Custom
future_option: {nested: [1, 2]}
mapping:
- name: A
  source_event:
    gamepad:
      button: South
  target_events:
  - gamepad:
      button: East
  some_new_field: true
`)
	out, _, err := SetMapping(p, "QuickAccess", Action("oxp"))
	if err != nil {
		t.Fatal(err)
	}
	g := generic(t, out)
	if !reflect.DeepEqual(g["future_option"], map[string]any{"nested": []any{1, 2}}) {
		t.Errorf("unknown top-level field lost: %v", g["future_option"])
	}
	if mappings(t, out)[0]["some_new_field"] != true {
		t.Error("unknown mapping field lost")
	}
}

func TestSetMappingAddsAMissingMappingList(t *testing.T) {
	out, changed, err := SetMapping([]byte("version: 1\nkind: DeviceProfile\nname: Empty\n"), "Keyboard", Action("home"))
	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if got := targetsFor(t, out, "Keyboard"); len(got) != 1 {
		t.Errorf("Keyboard targets = %v", got)
	}
}

func TestSetMappingRefusesForeignActionsAndBadProfiles(t *testing.T) {
	if _, _, err := SetMapping(oxpDefault(t), "Keyboard", "screenshot"); err == nil {
		t.Error("an action without voltaire's prefix must be refused: removal could not find it again")
	}
	for _, bad := range []string{"- a list\n", "mapping: notalist\n", ":\n  :"} {
		if _, _, err := SetMapping([]byte(bad), "Keyboard", Action("home")); err == nil {
			t.Errorf("profile %q accepted", bad)
		}
	}
}

func TestActionRoundTrip(t *testing.T) {
	if id, ok := ButtonID(Action("keyboard")); !ok || id != "keyboard" {
		t.Errorf("ButtonID(Action) = %q, %v", id, ok)
	}
	for _, a := range []string{"marker_a", "voltaire_", "screenshot"} {
		if _, ok := ButtonID(a); ok {
			t.Errorf("ButtonID(%q) claimed a button", a)
		}
	}
	if !strings.HasPrefix(Action("x"), ActionPrefix) {
		t.Error("Action must carry the prefix")
	}
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }
