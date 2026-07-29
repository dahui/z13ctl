package daemon

// state_test.go — Tests for state persistence: saveState, loadState, defaultState.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
)

func TestSaveAndLoadState_RoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	s := api.State{
		Lighting: api.LightingState{
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

	s := api.State{
		Lighting: api.LightingState{
			Enabled: true, Mode: "cycle", Color: "FF0000",
			Color2: "000000", Speed: "normal", Brightness: 3,
		},
		Devices: map[string]api.LightingState{
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
	s := api.State{
		Lighting: api.LightingState{
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

// TestStatePath_WithoutHome covers the fallback for an environment with neither
// XDG_STATE_HOME nor a resolvable home directory. os.UserHomeDir returns an
// error and an empty string there, which silently yielded the absolute path
// "/.local/state/z13ctl/state.json" — unwritable for any non-root user, so
// every save failed.
func TestStatePath_WithoutHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	got := statePath()
	if strings.HasPrefix(got, "/.local/") {
		t.Fatalf("statePath() = %q, want a writable fallback rather than a root-relative path", got)
	}
	if filepath.Dir(filepath.Dir(got)) != os.TempDir() {
		t.Errorf("statePath() = %q, want it under %q", got, os.TempDir())
	}
}

// TestLoadState_PreservesCorruptFile is the regression guard for a silent data
// loss: an unparseable state file was discarded with no log line, and the next
// save overwrote it — taking every saved setting with it and leaving nothing to
// diagnose.
func TestLoadState_PreservesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	// A truncated write — the realistic corruption for an atomic-rename scheme.
	const truncated = `{"lighting":{"enabled":true,"mode":"stat`
	if err := os.WriteFile(path, []byte(truncated), 0o600); err != nil {
		t.Fatal(err)
	}

	if got, def := loadState(), defaultState(); got.Lighting != def.Lighting {
		t.Errorf("corrupt file: got Lighting %+v, want default %+v", got.Lighting, def.Lighting)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupt state.json is still in place (err = %v); it should have been renamed aside", err)
	}
	preserved, err := os.ReadFile(path + ".corrupt")
	if err != nil {
		t.Fatalf("reading the preserved copy = %v, want the original content kept for diagnosis", err)
	}
	if string(preserved) != truncated {
		t.Errorf("preserved copy = %q, want the original %q", preserved, truncated)
	}
}

// TestSaveState_CleansUpTempOnRenameFailure: a rename that fails tends to keep
// failing, so leaving the temp file behind means every later save rewrites the
// same orphan beside the state file.
func TestSaveState_CleansUpTempOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	// A directory at the destination makes os.Rename fail while the temp write
	// itself still succeeds.
	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := saveState(defaultState()); err == nil {
		t.Fatal("saveState() = nil, want an error when the destination is a directory")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("state.json.tmp was left behind (err = %v)", err)
	}
}

func TestSaveState_RestrictivePermissions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := saveState(defaultState()); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	info, err := os.Stat(statePath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file mode = %#o, want 0600", perm)
	}
}
