package daemon

// state.go — State type and JSON persistence via $XDG_STATE_HOME.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State holds the last-applied settings for all controllable subsystems.
// It is written to disk on every successful command and restored on startup.
type State struct {
	Lighting LightingState            `json:"lighting"`
	Devices  map[string]LightingState `json:"devices,omitempty"` // per-device overrides keyed by name
	Profile  string                   `json:"profile,omitempty"`
	Battery  int                      `json:"battery_limit,omitempty"`
}

// LightingState captures all parameters needed to reproduce the last Apply call.
type LightingState struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	Color      string `json:"color"`  // "RRGGBB" hex
	Color2     string `json:"color2"` // "RRGGBB" hex
	Speed      string `json:"speed"`
	Brightness int    `json:"brightness"` // 0–3
}

func defaultState() State {
	return State{
		Lighting: LightingState{
			Enabled:    true,
			Mode:       "static",
			Color:      "FF0000",
			Color2:     "000000",
			Speed:      "normal",
			Brightness: 3,
		},
	}
}

// statePath returns the XDG-compliant path for the state file.
func statePath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "z13ctl", "state.json")
}

// loadState reads persisted state. Returns defaultState() if the file is
// missing or cannot be parsed.
func loadState() State {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return defaultState()
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultState()
	}
	return s
}

// saveState atomically writes state to disk.
func saveState(s State) error {
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
