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
	// Repair partially-populated lighting entries on the way in, so the fix
	// reaches everything reading daemon state — including get-state, which would
	// otherwise hand a GUI an entry with an empty mode and colour — and so the
	// repair is persisted by the next save rather than reapplied on every read.
	s.Lighting = normalizeLightingState(s.Lighting, defaultState().Lighting)
	for name, ls := range s.Devices {
		s.Devices[name] = normalizeLightingState(ls, s.Lighting)
	}
	return migrateCustomProfiles(s)
}

// migrateCustomProfiles brings a loaded state up to the current schema.
//
// State files written before named profiles existed carry the custom fan curve,
// TDP and undervolt in the top-level FanCurve/TDP/Undervolt fields; those become
// the "custom" profile. Newer files carry CustomProfiles and the top-level
// fields are only a projection of the active profile, so they are discarded and
// rebuilt rather than trusted.
//
// It also drops any profile whose name is reserved. Validation rejects those at
// the door, but state.json is a plain file a user can edit, and a custom profile
// named "balanced" would otherwise shadow the firmware profile of that name.
func migrateCustomProfiles(s api.State) api.State {
	if s.CustomProfiles == nil && (s.FanCurve != nil || s.TDP != nil || s.Undervolt != nil) {
		p := api.CustomProfile{
			Name:      api.DefaultCustomProfile,
			FanCurve:  s.FanCurve,
			TDP:       s.TDP,
			Undervolt: s.Undervolt,
		}
		if p.Undervolt != nil {
			// Active describes hardware, which has not been written yet.
			p.Undervolt.Active = false
		}
		s.CustomProfiles = map[string]api.CustomProfile{api.DefaultCustomProfile: p}
		slog.Info("migrated saved custom settings into the \"custom\" profile")
	}
	for name := range s.CustomProfiles {
		if api.IsStockProfileName(name) || name == "" {
			slog.Warn("dropping custom profile with a reserved name", "profile", name)
			delete(s.CustomProfiles, name)
			continue
		}
		// The name field is what profile --list reports; a hand-edited file may
		// disagree with the key, and the key is the addressable one.
		p := s.CustomProfiles[name]
		p.Name = name
		s.CustomProfiles[name] = p
	}
	// An active profile that resolves to nothing — deleted by a hand edit, or
	// dropped by a downgrade — must not be carried forward. Left in place it
	// reads as neither stock nor custom, so nothing restores it and every later
	// lookup falls through to an error. Clearing it makes effectiveProfile fall
	// back to platform_profile, which is the honest answer.
	if s.Profile != "" && !api.IsStockProfileName(s.Profile) && !s.IsCustomProfile(s.Profile) {
		slog.Warn("saved profile no longer exists; falling back to the firmware profile", "profile", s.Profile)
		s.Profile = ""
	}

	// The legacy fields are an output projection, never in-memory storage, so
	// clear them here: leaving a copy behind invites a future edit to read the
	// stale one instead of the profile map.
	s.FanCurve, s.TDP, s.Undervolt = nil, nil, nil
	return s
}

// withLegacyProjection returns s with the top-level FanCurve/TDP/Undervolt
// fields filled in from the custom profile those fields used to mean.
//
// They are no longer storage — CustomProfiles is — but they remain part of the
// get-state response and of the state file, so clients and daemons written
// before named profiles existed keep working. Everything inside the daemon
// reads the profile map; this projection exists purely for the wire, and is
// applied at the two serialization boundaries (saveState and get-state).
//
// The source is the active custom profile, or "custom" when a firmware profile
// is active — i.e. whatever a bare "undervolt --set" would edit and
// "profile --set custom" would recall. Projecting nothing on a firmware profile
// would be a regression on both sides: a GUI showing "saved undervolt, not
// active" would lose the value it displays, and a downgrade taken while on a
// firmware profile would write those settings away entirely. Active is false
// throughout in that case, since a firmware profile clears the offset in
// hardware, so nothing here can read as applied when it is not.
func withLegacyProjection(s api.State) api.State {
	p, ok := s.ActiveCustomProfile()
	if !ok {
		p = s.CustomProfiles[api.DefaultCustomProfile]
	}
	s.FanCurve = p.FanCurve
	s.TDP = p.TDP
	s.Undervolt = p.Undervolt
	return s
}

// cloneState returns a deep copy of s that shares no mutable memory with it.
//
// api.State holds two maps and five pointer fields, so the plain struct copy in
// "s := d.state" still aliases live daemon state. Handlers release d.mu before
// calling saveState, so marshaling an aliased snapshot races with any handler
// that mutates a map or dereferences a pointer under the lock — a concurrent
// map read/write, which the Go runtime turns into an unrecoverable crash rather
// than a catchable panic. Always snapshot through this before unlocking.
//
// CustomProfiles needs a copy per entry, not just a new map header: each entry
// carries three pointers and a slice, and a shallow copy would share every one
// of them.
func cloneState(s api.State) api.State {
	c := s
	if s.Devices != nil {
		c.Devices = make(map[string]api.LightingState, len(s.Devices))
		for k, v := range s.Devices {
			c.Devices[k] = v
		}
	}
	if s.CustomProfiles != nil {
		c.CustomProfiles = make(map[string]api.CustomProfile, len(s.CustomProfiles))
		for k, v := range s.CustomProfiles {
			c.CustomProfiles[k] = cloneCustomProfile(v)
		}
	}
	if s.Autoswitch != nil {
		as := *s.Autoswitch
		c.Autoswitch = &as
	}
	c.TDP = cloneTDP(s.TDP)
	c.Undervolt = cloneUndervolt(s.Undervolt)
	c.FanCurve = cloneFanCurve(s.FanCurve)
	return c
}

// cloneCustomProfile deep-copies one profile, including the fan curve's points.
func cloneCustomProfile(p api.CustomProfile) api.CustomProfile {
	p.FanCurve = cloneFanCurve(p.FanCurve)
	p.TDP = cloneTDP(p.TDP)
	p.Undervolt = cloneUndervolt(p.Undervolt)
	return p
}

func cloneFanCurve(fc *api.FanCurveState) *api.FanCurveState {
	if fc == nil {
		return nil
	}
	c := *fc
	if fc.Points != nil {
		c.Points = append([]api.FanCurvePoint(nil), fc.Points...)
	}
	return &c
}

func cloneTDP(t *api.TDPState) *api.TDPState {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func cloneUndervolt(uv *api.UndervoltState) *api.UndervoltState {
	if uv == nil {
		return nil
	}
	c := *uv
	return &c
}

// saveState atomically writes state to disk. The legacy FanCurve/TDP/Undervolt
// fields are projected from the active profile on the way out so that a daemon
// from before named profiles existed still finds the active profile's settings.
func saveState(s api.State) error {
	s = withLegacyProjection(s)
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
