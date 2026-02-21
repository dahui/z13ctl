package daemon

// state_test.go — Tests for state persistence: saveState, loadState, defaultState.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadState_RoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	s := State{
		Lighting: LightingState{
			Enabled:    true,
			Mode:       "cycle",
			Color:      "FF0000",
			Color2:     "000000",
			Speed:      "normal",
			Brightness: 3,
		},
		Profile: "performance",
		Battery: 80,
	}
	if err := saveState(s); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got := loadState()
	if got.Lighting != s.Lighting {
		t.Errorf("Lighting mismatch: got %+v, want %+v", got.Lighting, s.Lighting)
	}
	if got.Profile != s.Profile {
		t.Errorf("Profile: got %q, want %q", got.Profile, s.Profile)
	}
	if got.Battery != s.Battery {
		t.Errorf("Battery: got %d, want %d", got.Battery, s.Battery)
	}
}

func TestSaveAndLoadState_WithDevices(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	s := State{
		Lighting: LightingState{
			Enabled: true, Mode: "cycle", Color: "FF0000",
			Color2: "000000", Speed: "normal", Brightness: 3,
		},
		Devices: map[string]LightingState{
			"keyboard": {
				Enabled: true, Mode: "static", Color: "00FFFF",
				Color2: "000000", Speed: "normal", Brightness: 2,
			},
			"lightbar": {
				Enabled: true, Mode: "cycle", Color: "FF0000",
				Color2: "000000", Speed: "fast", Brightness: 3,
			},
		},
	}
	if err := saveState(s); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got := loadState()

	if got.Lighting != s.Lighting {
		t.Errorf("Lighting mismatch: got %+v, want %+v", got.Lighting, s.Lighting)
	}
	if len(got.Devices) != 2 {
		t.Fatalf("Devices len: got %d, want 2", len(got.Devices))
	}
	if got.Devices["keyboard"] != s.Devices["keyboard"] {
		t.Errorf("keyboard mismatch: got %+v, want %+v", got.Devices["keyboard"], s.Devices["keyboard"])
	}
	if got.Devices["lightbar"] != s.Devices["lightbar"] {
		t.Errorf("lightbar mismatch: got %+v, want %+v", got.Devices["lightbar"], s.Devices["lightbar"])
	}
}

func TestLoadState_MissingFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "nonexistent"))

	got := loadState()
	def := defaultState()
	if got.Lighting != def.Lighting {
		t.Errorf("missing file: got Lighting %+v, want default %+v", got.Lighting, def.Lighting)
	}
}

func TestLoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadState()
	def := defaultState()
	if got.Lighting != def.Lighting {
		t.Errorf("invalid JSON: got Lighting %+v, want default %+v", got.Lighting, def.Lighting)
	}
}

func TestLoadState_DevicesNilOnAllDeviceState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Saving a state with no Devices should load back with nil Devices.
	s := State{
		Lighting: LightingState{
			Enabled: true, Mode: "static", Color: "FF00FF",
			Color2: "000000", Speed: "normal", Brightness: 2,
		},
	}
	if err := saveState(s); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got := loadState()
	if got.Devices != nil {
		t.Errorf("Devices should be nil for all-device state, got %v", got.Devices)
	}
}
