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

// cloneState returns a deep copy of s that shares no mutable memory with it.
//
// api.State holds a map and four pointer fields, so the plain struct copy in
// "s := d.state" still aliases live daemon state. Handlers release d.mu before
// calling saveState, so marshaling an aliased snapshot races with any handler
// that mutates the map or dereferences a pointer under the lock — a concurrent
// map read/write, which the Go runtime turns into an unrecoverable crash rather
// than a catchable panic. Always snapshot through this before unlocking.
func cloneState(s api.State) api.State {
	c := s
	if s.Devices != nil {
		c.Devices = make(map[string]api.LightingState, len(s.Devices))
		for k, v := range s.Devices {
			c.Devices[k] = v
		}
	}
	if s.TDP != nil {
		tdp := *s.TDP
		c.TDP = &tdp
	}
	if s.Undervolt != nil {
		uv := *s.Undervolt
		c.Undervolt = &uv
	}
	if s.FanCurve != nil {
		fc := *s.FanCurve
		if s.FanCurve.Points != nil {
			fc.Points = append([]api.FanCurvePoint(nil), s.FanCurve.Points...)
		}
		c.FanCurve = &fc
	}
	return c
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
