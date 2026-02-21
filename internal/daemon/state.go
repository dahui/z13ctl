package daemon

// state.go — State persistence via $XDG_STATE_HOME.
//
// State and LightingState types are defined in the public api package
// (github.com/dahui/z13ctl/api) and used here for persistence.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/dahui/z13ctl/api"
)

func defaultState() api.State {
	return api.State{
		Lighting: api.LightingState{
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
func loadState() api.State {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return defaultState()
	}
	var s api.State
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultState()
	}
	return s
}

// saveState atomically writes state to disk.
func saveState(s api.State) error {
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
