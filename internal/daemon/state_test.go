package daemon

// state_test.go — Tests for state persistence: saveState, loadState, defaultState.

import (
	"encoding/json"
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

// TestLoadStateRepairsPartialDeviceEntries: the repair has to happen on load,
// not only when lighting is applied, or get-state keeps handing clients an entry
// with an empty mode and colour and the damaged file is never rewritten.
func TestLoadStateRepairsPartialDeviceEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	// Exactly what "off --device keyboard" then "brightness medium" used to write.
	const broken = `{
	  "lighting": {"enabled":true,"mode":"static","color":"FF0000","color2":"000000","speed":"normal","brightness":3},
	  "devices": {"keyboard": {"enabled":true,"mode":"","color":"","color2":"","speed":"","brightness":2}}
	}`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadState()
	kb, ok := got.Devices["keyboard"]
	if !ok {
		t.Fatal("keyboard entry missing after load")
	}
	if kb.Mode == "" || kb.Color == "" || kb.Speed == "" {
		t.Errorf("keyboard entry still partial after load: %+v", kb)
	}
	if kb.Brightness != 2 || !kb.Enabled {
		t.Errorf("keyboard entry lost its real values: %+v", kb)
	}
	// Gaps are filled from the all-device state, not blindly from defaults.
	if kb.Mode != "static" || kb.Color != "FF0000" {
		t.Errorf("keyboard entry = %+v, want gaps filled from the all-device state", kb)
	}
}

// TestLoadStateMigratesLegacyCustomSettings covers the upgrade path: a state
// file written before named profiles existed carries the custom fan curve, TDP
// and undervolt at the top level. They must become the "custom" profile, or
// every user's saved settings disappear on the first start after upgrading.
func TestLoadStateMigratesLegacyCustomSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	legacy := `{
	  "lighting": {"enabled": true, "mode": "static", "color": "FF0000", "color2": "000000", "speed": "normal", "brightness": 3},
	  "profile": "custom",
	  "fan_curve": {"mode": 1, "points": [{"temp": 40, "pwm": 50}]},
	  "tdp": {"pl1_spl": 35, "pl2_sppt": 40},
	  "undervolt": {"cpu_co": -25, "active": true}
	}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := loadState()

	p, ok := got.ActiveCustomProfile()
	if !ok {
		t.Fatalf("Profile %q is not custom after migration; the saved settings are unreachable", got.Profile)
	}
	if p.TDP == nil || p.TDP.PL1SPL != 35 {
		t.Errorf("migrated TDP = %+v, want 35W", p.TDP)
	}
	if p.FanCurve == nil || len(p.FanCurve.Points) != 1 {
		t.Errorf("migrated fan curve = %+v, want one point", p.FanCurve)
	}
	if p.Undervolt == nil || p.Undervolt.CPUCO != -25 {
		t.Errorf("migrated undervolt = %+v, want CO -25", p.Undervolt)
	}
	// Active describes hardware, which has not been written on the way in.
	if p.Undervolt != nil && p.Undervolt.Active {
		t.Error("migrated undervolt claims to be applied before anything was written to the SMU")
	}
	// The top-level fields are an output projection only; leaving a copy in
	// memory invites a later edit to read the stale one.
	if got.FanCurve != nil || got.TDP != nil || got.Undervolt != nil {
		t.Error("loadState left the legacy projection populated in memory")
	}
}

// TestLoadStateDropsReservedProfileNames is the last of the four layers that
// stop a custom profile shadowing a firmware one. The other three are
// validation, api.State.IsCustomProfile, and applyProfileLocked's stock-first
// switch; this one covers a hand-edited or downgrade-mangled state file, which
// parses fine and so is never seen by any of them.
func TestLoadStateDropsReservedProfileNames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	hacked := `{
	  "profile": "balanced",
	  "custom_profiles": {
	    "balanced": {"name": "balanced", "tdp": {"pl1_spl": 90}},
	    "performance": {"name": "performance"},
	    "gaming": {"name": "gaming", "tdp": {"pl1_spl": 70}}
	  }
	}`
	if err := os.WriteFile(path, []byte(hacked), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := loadState()

	for _, reserved := range api.StockProfiles {
		if _, ok := got.CustomProfiles[reserved]; ok {
			t.Errorf("custom profile %q survived load; it would shadow the firmware profile", reserved)
		}
	}
	if got.IsCustomProfile("balanced") {
		t.Error("IsCustomProfile(\"balanced\") = true after load, want false")
	}
	if _, ok := got.CustomProfiles["gaming"]; !ok {
		t.Error("a legitimate profile was dropped along with the reserved ones")
	}
}

func TestSaveAndLoadStateCustomProfilesRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	s := api.State{
		Lighting: defaultState().Lighting,
		Profile:  "battery-uv",
		CustomProfiles: map[string]api.CustomProfile{
			"battery-uv": {
				Name:      "battery-uv",
				TDP:       &api.TDPState{PL1SPL: 35, PL2SPPT: 40},
				Undervolt: &api.UndervoltState{CPUCO: -25},
			},
		},
		Autoswitch: &api.AutoswitchState{Enabled: true, AC: "balanced", Battery: "battery-uv"},
	}
	if err := saveState(s); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got := loadState()

	if got.Profile != "battery-uv" {
		t.Errorf("Profile = %q, want \"battery-uv\"", got.Profile)
	}
	p, ok := got.CustomProfiles["battery-uv"]
	if !ok {
		t.Fatal("the saved profile did not survive the round trip")
	}
	if p.TDP == nil || p.TDP.PL1SPL != 35 || p.Undervolt == nil || p.Undervolt.CPUCO != -25 {
		t.Errorf("round-tripped profile = %+v", p)
	}
	if got.Autoswitch == nil || !got.Autoswitch.Enabled || got.Autoswitch.Battery != "battery-uv" {
		t.Errorf("round-tripped autoswitch = %+v", got.Autoswitch)
	}
}

// TestSaveStateWritesTheLegacyProjection covers downgrade tolerance: a daemon
// from before named profiles reads only the top-level fields, so the active
// profile's settings are written there too. Without this an upgrade followed by
// a downgrade silently loses the user's custom settings.
func TestSaveStateWritesTheLegacyProjection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	if err := saveState(api.State{
		Profile: "battery-uv",
		CustomProfiles: map[string]api.CustomProfile{
			"battery-uv": {Name: "battery-uv", TDP: &api.TDPState{PL1SPL: 35}},
		},
	}); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "z13ctl", "state.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw struct {
		TDP *api.TDPState `json:"tdp"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw.TDP == nil || raw.TDP.PL1SPL != 35 {
		t.Errorf("top-level tdp in state.json = %+v, want the active profile's 35W", raw.TDP)
	}
}

// TestLoadStateClearsAProfileThatNoLongerExists covers a state file naming a
// custom profile that is gone — deleted by a hand edit, or dropped by a
// downgrade. Carried forward it reads as neither firmware nor custom, so
// nothing restores it, every lookup errors, and Run() would previously have
// written the name straight to platform_profile.
func TestLoadStateClearsAProfileThatNoLongerExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "z13ctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := `{"profile":"deleted-profile","custom_profiles":{"gaming":{"name":"gaming"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := loadState()
	if got.Profile != "" {
		t.Errorf("Profile = %q, want \"\" so effectiveProfile falls back to platform_profile", got.Profile)
	}
	if _, ok := got.CustomProfiles["gaming"]; !ok {
		t.Error("a legitimate profile was dropped along with the dangling name")
	}
}

// TestLoadStateKeepsResolvableProfiles is the premise guard for the test above:
// it must not clear a name that does resolve.
func TestLoadStateKeepsResolvableProfiles(t *testing.T) {
	for _, name := range []string{"balanced", "performance", "custom", "gaming"} {
		dir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", dir)
		path := filepath.Join(dir, "z13ctl", "state.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		body := `{"profile":"` + name + `","custom_profiles":{"gaming":{"name":"gaming"}}}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if got := loadState(); got.Profile != name {
			t.Errorf("Profile = %q, want %q — a resolvable name was cleared", got.Profile, name)
		}
	}
}
