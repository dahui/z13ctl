package asusz13

// toggles_test.go — which firmware toggles are offered is the allowlist ∩ what
// the kernel exposes as a writable 0/1 attribute; wording is device data,
// falling back to the kernel's display_name.

import (
	"os"
	"slices"
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// attr writes one armoury attribute's metadata into the fake.
func (f *fakeSysfs) attr(t *testing.T, id, typ, possible, display string, mode os.FileMode) {
	t.Helper()
	dir := f.firmware + "/" + id
	f.writeFile(t, dir+"/current_value", "0")
	if err := os.Chmod(dir+"/current_value", mode); err != nil {
		t.Fatal(err)
	}
	f.writeFile(t, dir+"/type", typ)
	f.writeFile(t, dir+"/possible_values", possible)
	f.writeFile(t, dir+"/display_name", display)
}

func ids(specs []driver.ToggleSpec) []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = s.ID
	}
	return out
}

func TestTogglesComeFromTheKernelThroughTheAllowlist(t *testing.T) {
	f := newFakeSysfs(t)
	f.attr(t, "boot_sound", "enumeration", "0;1", "Set the boot POST sound", 0o664)
	f.attr(t, "panel_overdrive", "enumeration", "0;1", "Set the panel refresh overdrive", 0o664)
	// Present in the kernel, never offered: not on the allowlist.
	f.attr(t, "gpu_mux_mode", "enumeration", "0;1", "Set the GPU display MUX mode", 0o664)

	declared := []driver.ToggleSpec{{ID: "panel_overdrive", Label: "Panel overdrive", Description: "may ghost", Kind: driver.ToggleBool}}
	tg, err := NewToggles(declared, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := tg.List()
	// Declared first, then allowlisted extras the kernel exposes.
	if !slices.Equal(ids(got), []string{"panel_overdrive", "boot_sound"}) {
		t.Fatalf("List = %v, want panel_overdrive then boot_sound, and never gpu_mux_mode", ids(got))
	}
	if got[0].Label != "Panel overdrive" || got[0].Description != "may ghost" {
		t.Errorf("declared wording lost: %+v", got[0])
	}
	if got[1].Label != "Set the boot POST sound" {
		t.Errorf("undeclared label = %q, want the kernel's display_name", got[1].Label)
	}
	if !slices.Equal(got[0].Values, []int{0, 1}) || !got[0].Accepts(1) || got[0].Accepts(2) {
		t.Errorf("values = %v, want the kernel's 0;1", got[0].Values)
	}
}

func TestTogglesSkipWhatCannotBeASwitch(t *testing.T) {
	f := newFakeSysfs(t)
	f.attr(t, "boot_sound", "enumeration", "0;1;2", "x", 0o664)    // not 0/1
	f.attr(t, "panel_overdrive", "enumeration", "0;1", "y", 0o444) // read-only
	tg, _ := NewToggles(nil, nil)
	if got := tg.List(); len(got) != 0 {
		t.Errorf("List = %v, want nothing: one is not a switch, one is read-only", ids(got))
	}
}

func TestTogglesHiddenAndUnknown(t *testing.T) {
	f := newFakeSysfs(t)
	f.attr(t, "boot_sound", "enumeration", "0;1", "x", 0o664)
	f.attr(t, "panel_overdrive", "enumeration", "0;1", "y", 0o664)
	tg, err := NewToggles(nil, []string{"boot_sound"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(tg.List()); !slices.Equal(got, []string{"panel_overdrive"}) {
		t.Errorf("List = %v, want boot_sound hidden", got)
	}
	if _, err := NewToggles([]driver.ToggleSpec{{ID: "gpu_mux_mode"}}, nil); err == nil {
		t.Error("NewToggles accepted an id outside the allowlist")
	}
	if _, err := NewToggles(nil, []string{"dgpu_disable"}); err == nil {
		t.Error("NewToggles accepted a hidden id outside the allowlist")
	}
}

// asus-armoury not loaded: the declared list stands, so the rows show as
// unknown (a failed read) rather than vanishing.
func TestTogglesFallBackToDeclaredWithoutTheModule(t *testing.T) {
	newFakeSysfs(t)
	swap(t, &sysFirmwareAttrDir, t.TempDir()+"/absent")
	declared := []driver.ToggleSpec{{ID: "boot_sound", Label: "POST boot sound", Kind: driver.ToggleBool}}
	tg, _ := NewToggles(declared, nil)
	if got := ids(tg.List()); !slices.Equal(got, []string{"boot_sound"}) {
		t.Errorf("List = %v, want the declared entry", got)
	}
}
