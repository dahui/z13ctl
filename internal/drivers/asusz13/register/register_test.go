package register

// Assembly here is hermetic: constructors are pure and Assemble performs no
// hardware I/O, so these tests prove the wiring — method names in the Z13
// device file resolve to real factories, and the engine ends up holding the
// file's envelope — without touching sysfs.

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
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
// declares resolves through init() registration to a working factory.
func TestZ13AssemblesWithRegisteredDrivers(t *testing.T) {
	c := z13Config(t)
	d, err := device.Assemble(c)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if d.Fans == nil || d.Power == nil || d.Profiles == nil || d.Toggles == nil ||
		d.Battery == nil || d.Undervolt == nil || d.Telemetry == nil ||
		d.Lighting == nil || d.Buttons == nil {
		t.Fatalf("assembled device has nil capabilities: %+v", d)
	}
	if zones := d.Lighting.Zones(); len(zones) != 2 || zones[0] != "keyboard" || zones[1] != "lightbar" {
		t.Errorf("lighting zones = %v, want keyboard + lightbar", zones)
	}

	// The engine's envelope is the device file's — the file is the authoritative
	// source of the Z13's numbers — so this closes the loop: TOML → registry →
	// engine, one envelope throughout.
	fileEnv := c.Power.Envelope()
	env := d.Power.Envelope()
	if env.TDPMaxSafe != fileEnv.TDPMaxSafe || env.TDPMaxSafe == 0 ||
		len(env.FloorCurve) != len(fileEnv.FloorCurve) || len(env.FloorCurve) == 0 {
		t.Errorf("engine envelope = %+v, want the Z13 file's values", env)
	}
	if shape := d.Fans.Shape(); shape.Points != 8 || shape.TempMin != 35 || shape.TempMax != 105 {
		t.Errorf("fan shape = %+v, want the Z13 file's 8 points over 35–105°C", shape)
	}
	if lo, hi := d.Undervolt.Range(); lo != c.Undervolt.Min || hi != c.Undervolt.Max || lo == hi {
		t.Errorf("undervolt range = %d..%d, want the file's %d..%d", lo, hi, c.Undervolt.Min, c.Undervolt.Max)
	}
	if got := d.Toggles.List(); len(got) != 2 || got[0].ID != "boot_sound" || got[1].ID != "panel_overdrive" {
		t.Errorf("toggles = %+v, want boot_sound + panel_overdrive", got)
	}
	if names := d.Profiles.Names(); len(names) != 3 || names[0] != "quiet" {
		t.Errorf("profile names = %v, want the three firmware profiles", names)
	}
}

// TestTogglesRefuseUnknownID pins that device data naming a toggle this driver
// cannot drive is an assembly-time error, not a dead control.
func TestTogglesRefuseUnknownID(t *testing.T) {
	c := z13Config(t)
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
