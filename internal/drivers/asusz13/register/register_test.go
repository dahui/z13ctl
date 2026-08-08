package register

// Assembly here is hermetic: constructors are pure and Assemble performs no
// hardware I/O, so these tests prove the wiring — method names in the Z13
// device file resolve to real factories, and the engine ends up holding the
// file's envelope — without touching sysfs.

import (
	"strings"
	"testing"

	"github.com/dahui/z13ctl/internal/device"
	"github.com/dahui/z13ctl/internal/drivers/asusz13"
)

func z13Config(t *testing.T) device.Config {
	t.Helper()
	configs, err := device.Configs()
	if err != nil {
		t.Fatalf("Configs: %v", err)
	}
	for _, c := range configs {
		if c.Device.ID == "asus-rog-flow-z13-2025" {
			return c
		}
	}
	t.Fatal("Z13 device file not found")
	return device.Config{}
}

// TestZ13AssemblesWithRegisteredDrivers proves every method name the Z13 file
// declares — bar the two known gaps — resolves through init() registration to
// a working factory.
func TestZ13AssemblesWithRegisteredDrivers(t *testing.T) {
	c := z13Config(t)
	// Lighting and buttons are the known gaps (see register.go); drop them so
	// the seven registered classes assemble.
	c.Lighting = nil
	c.Button = nil

	d, err := device.Assemble(c)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if d.Fans == nil || d.Power == nil || d.Profiles == nil || d.Toggles == nil ||
		d.Battery == nil || d.Undervolt == nil || d.Telemetry == nil {
		t.Fatalf("assembled device has nil capabilities: %+v", d)
	}

	// The engine's envelope is the device file's, and the file is pinned to
	// the driver constants by internal/device's drift guard — so this closes
	// the loop: TOML → registry → engine, one envelope throughout.
	env := d.Power.Envelope()
	if env.TDPMaxSafe != asusz13.TDPMaxSafe || len(env.FloorCurve) != len(asusz13.HighTDPFanCurve()) {
		t.Errorf("engine envelope = %+v, want the Z13 file's values", env)
	}
	if shape := d.Fans.Shape(); shape.Points != 8 || shape.TempMin != 35 || shape.TempMax != 105 {
		t.Errorf("fan shape = %+v, want the Z13 file's 8 points over 35–105°C", shape)
	}
	if lo, hi := d.Undervolt.Range(); lo != asusz13.UVMinCPU || hi != asusz13.UVMaxCPU {
		t.Errorf("undervolt range = %d..%d, want %d..%d", lo, hi, asusz13.UVMinCPU, asusz13.UVMaxCPU)
	}
	if got := d.Toggles.List(); len(got) != 2 || got[0].ID != "boot_sound" || got[1].ID != "panel_overdrive" {
		t.Errorf("toggles = %+v, want boot_sound + panel_overdrive", got)
	}
	if names := d.Profiles.Names(); len(names) != 3 || names[0] != "quiet" {
		t.Errorf("profile names = %v, want the three firmware profiles", names)
	}
}

// TestZ13AssemblyGap pins the current known gap: the full Z13 config does not
// assemble because lighting and buttons still live inside the daemon. When
// their drivers register, this test fails — delete it and let the full-config
// assembly above take over.
func TestZ13AssemblyGap(t *testing.T) {
	_, err := device.Assemble(z13Config(t))
	if err == nil {
		t.Fatal("full Z13 config assembled — the lighting/buttons gap has closed; " +
			"update these tests to assemble the full config")
	}
	if !strings.Contains(err.Error(), "lighting") {
		t.Errorf("expected the lighting gap, got: %v", err)
	}
}

// TestTogglesRefuseUnknownID pins that device data naming a toggle this driver
// cannot drive is an assembly-time error, not a dead control.
func TestTogglesRefuseUnknownID(t *testing.T) {
	c := z13Config(t)
	c.Lighting, c.Button = nil, nil
	// Copy before mutating: c.Toggles points into the shared cached config.
	tg := *c.Toggles
	tg.Entries = append(append([]device.ToggleEntry{}, tg.Entries...),
		device.ToggleEntry{ID: "warp-drive", Label: "Warp drive"})
	c.Toggles = &tg
	_, err := device.Assemble(c)
	if err == nil || !strings.Contains(err.Error(), "warp-drive") {
		t.Errorf("unknown toggle id not refused: %v", err)
	}
}
