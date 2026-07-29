package daemon

// state.go — State persistence via $XDG_STATE_HOME.
//
// State and LightingState types are defined in the public api package
// (github.com/dahui/z13ctl/api) and used here for persistence.

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/dahui/z13ctl/api"
)

// noHomeWarnOnce keeps the unresolvable-home warning to a single line per
// process. A var so tests can reset it.
var noHomeWarnOnce = new(sync.Once)

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
//
// With neither XDG_STATE_HOME nor a resolvable home directory, os.UserHomeDir
// returns an error and an empty string — which would silently yield the
// absolute path "/.local/state/z13ctl/state.json", unwritable for any non-root
// user. Fall back to a temp dir so state is at least writable for the life of
// the boot rather than every save failing.
func statePath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			// Warn once: statePath is called on every save, so an unconditional
			// warning here would put a line in the journal for every command.
			noHomeWarnOnce.Do(func() {
				slog.Warn("cannot determine home directory; state will not persist across reboots", "err", err)
			})
			return filepath.Join(os.TempDir(), "z13ctl", "state.json")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "z13ctl", "state.json")
}

// loadState reads persisted state. Returns defaultState() if the file is
// missing or cannot be parsed.
//
// A file that exists but does not parse is preserved as state.json.corrupt
// before defaults are returned: the daemon saves state on the next command, so
// otherwise a truncated write (a power cut mid-save on a filesystem that does
// not honour the temp-file rename) would silently discard every saved setting
// with nothing left to inspect or recover from.
func loadState() api.State {
	path := statePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("cannot read state file; starting from defaults", "path", path, "err", err)
		}
		return defaultState()
	}
	var s api.State
	if err := json.Unmarshal(data, &s); err != nil {
		slog.Warn("state file is corrupt; starting from defaults", "path", path, "err", err)
		bad := path + ".corrupt"
		if renameErr := os.Rename(path, bad); renameErr != nil {
			slog.Warn("failed to preserve corrupt state file", "path", bad, "err", renameErr)
		} else {
			slog.Warn("corrupt state file preserved", "path", bad)
		}
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
	if err := os.Rename(tmp, path); err != nil {
		// Don't leave the temp file behind: a rename that fails once tends to
		// keep failing, and every subsequent save would rewrite the same orphan.
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
