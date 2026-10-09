// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// undervolt_gate_test.go — the one rule protecting the SMU mailbox: never send
// a Curve Optimizer reset for an offset that was never applied.
//
// uvAvailable() answers "does this machine support CO at all", which is true on
// every Z13 with ryzen_smu loaded. Gating a reset on it means every route to a
// stock profile writes the MP1 mailbox to clear something that was never set.
// One of those hard-locked this SoC on 2026-08-14 with no offset saved or
// active anywhere. uvApplied() is the gate; these tests are what keep a sixth
// call site from quietly going back to the wrong one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
)

func TestUndervoltActiveMirrorsSetUndervoltActive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		s    api.State
		want bool
	}{
		{"no profiles at all", api.State{}, false},
		{
			"a profile with no undervolt",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming"},
			}},
			false,
		},
		{
			// The case that froze the machine: an offset is saved but has never
			// been written to hardware, so there is nothing to clear.
			"saved but not active",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20}},
			}},
			false,
		},
		{
			"applied",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20, Active: true}},
			}},
			true,
		},
		{
			// CO is global hardware, so at most one profile's offset can be
			// applied — but the answer is "any", not "the active profile's".
			"one of several applied",
			api.State{CustomProfiles: map[string]api.CustomProfile{
				"quietish": {Name: "quietish", Undervolt: &api.UndervoltState{CPUCO: -5}},
				"gaming":   {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20, Active: true}},
			}},
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := undervoltActive(tc.s); got != tc.want {
				t.Errorf("undervoltActive = %v, want %v", got, tc.want)
			}
		})
	}
}

// setUndervoltActive and undervoltActive are the write and the read of one
// fact. Round-tripping is what stops them drifting apart — a getter that
// consulted a different field would keep passing its own table forever.
func TestUndervoltActiveRoundTrips(t *testing.T) {
	t.Parallel()

	s := api.State{CustomProfiles: map[string]api.CustomProfile{
		"a": {Name: "a", Undervolt: &api.UndervoltState{CPUCO: -10}},
		"b": {Name: "b", Undervolt: &api.UndervoltState{CPUCO: -20}},
	}}
	if undervoltActive(s) {
		t.Fatal("fresh profiles must not report an applied offset")
	}
	setUndervoltActive(s, true)
	if !undervoltActive(s) {
		t.Error("after setUndervoltActive(true) the state must read as applied")
	}
	setUndervoltActive(s, false)
	if undervoltActive(s) {
		t.Error("after setUndervoltActive(false) the state must not read as applied")
	}
}

// TestEveryUndervoltResetIsGatedOnApplied is a source check, for the same
// reason TestEveryToggleWritePathNotifies is: the alternative is a live SMU
// write, and the failure it guards is a machine that stops responding rather
// than a test that goes red.
//
// It looks backwards from each Undervolt.Reset() for its guard. funcBody's
// comment stripping is load-bearing — every one of these call sites *mentions*
// uvAvailable in prose, so a check that read comments would pass on the
// documentation alone. The negative control for this test is to change any one
// site back to uvAvailable() and confirm it fails.
func TestEveryUndervoltResetIsGatedOnApplied(t *testing.T) {
	t.Parallel()

	const window = 8 // lines to look back for the enclosing guard

	// Both packages, because the first version of this guard scanned only
	// internal/daemon and missed cmd/tdp.go's no-daemon reset entirely — a real
	// sixth call site, on the path taken whenever the daemon is down. A guard
	// scoped to one package is a guard that certifies the package, not the rule.
	dirs := []string{".", "../../cmd"}

	var files []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			files = append(files, filepath.Join(dir, name))
		}
	}

	found := 0
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		// Strip comments the same way funcBody does, so prose about the rule is
		// never mistaken for the rule.
		var code []string
		for _, line := range strings.Split(string(raw), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			code = append(code, line)
		}

		for i, line := range code {
			if !strings.Contains(line, "Undervolt.Reset()") {
				continue
			}
			found++
			lo := max(0, i-window)
			guard := strings.Join(code[lo:i], "\n")
			// uvApplied inside the daemon, daemon.UndervoltApplied from cmd/ —
			// the same predicate over the same state file, by necessity: the CLI
			// has no Daemon to ask and must not answer differently.
			gated := strings.Contains(guard, "uvApplied") ||
				strings.Contains(guard, "UndervoltApplied")
			if !gated {
				t.Errorf("%s:%d — Undervolt.Reset() is not gated on an applied-offset "+
					"check; an offset that was never applied must never be cleared. Guard was:\n%s",
					name, i+1, guard)
			}
			if strings.Contains(guard, "uvAvailable()") {
				t.Errorf("%s:%d — Undervolt.Reset() is gated on uvAvailable(), which is "+
					"true on every machine with the module loaded. Use uvApplied().",
					name, i+1)
			}
			if strings.Contains(guard, "Undervolt.Present()") && !gated {
				t.Errorf("%s:%d — Undervolt.Reset() is gated on Present(), which only "+
					"stats the module. Use daemon.UndervoltApplied().", name, i+1)
			}
		}
	}

	// Guard the premise: if a refactor renames the call, this test would pass by
	// finding nothing at all. Five in internal/daemon plus two in cmd/.
	if found < 7 {
		t.Fatalf("found %d Undervolt.Reset() call sites, expected at least 7 — "+
			"this guard needs updating with the rename", found)
	}
}

// probeSpy is an Undervolter that records whether its destructive probe ran.
type probeSpy struct{ probed *bool }

func (probeSpy) Present() bool { return true }
func (u probeSpy) ProbeAvailable() bool {
	*u.probed = true
	return true
}
func (probeSpy) Available() bool     { return true }
func (probeSpy) Range() (lo, hi int) { return -40, 0 }
func (probeSpy) Apply(int) error     { return nil }
func (probeSpy) Reset() error        { return nil }

// TestUvAppliedSkipsTheProbeWhenNothingIsApplied pins the order inside
// uvApplied: state first. The driver's probe sends the same payload as a reset
// the first time it runs, so asking it on a machine with nothing applied would
// be the speculative write by another route.
func TestUvAppliedSkipsTheProbeWhenNothingIsApplied(t *testing.T) {
	t.Parallel()
	probed := false
	d := &Daemon{
		hw: &device.Device{Undervolt: probeSpy{&probed}},
		state: api.State{CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -20}},
		}},
	}
	if d.uvApplied() || probed {
		t.Errorf("nothing applied: uvApplied probed=%v, want false without probing", probed)
	}

	setUndervoltActive(d.state, true)
	if !d.uvApplied() || !probed {
		t.Errorf("offset applied: uvApplied probed=%v, want true after probing", probed)
	}
}

// TestOnlyWritersProbeTheSMU: the driver's probe writes a CO reset on its first
// run, so uvProbe() may be called only where an offset is about to be written
// anyway, and ProbeAvailable() only from uvProbe itself. Anything that merely
// asks — get-state's undervolt_available, undervolt-get, an undervolt-reset with
// nothing applied — uses uvAvailable(), which never writes. get-state probing
// was how a GUI poll sent the speculative MP1 write after every daemon start.
func TestOnlyWritersProbeTheSMU(t *testing.T) {
	t.Parallel()

	allowed := map[string][]string{
		"uvProbe()": {
			"func (d *Daemon) handleUndervolt(", // writes the requested offset
			"func (d *Daemon) applyCustomHW(",   // writes the profile's offset
			"func (d *Daemon) reconcileOnce(",   // re-applies after a lost resume
			"func (d *Daemon) uvApplied(",       // only once state says applied
		},
		"ProbeAvailable()": {"func (d *Daemon) uvProbe("},
	}

	for _, dir := range []string{".", "../../cmd"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			raw, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			fn := ""
			for i, line := range strings.Split(string(raw), "\n") {
				if j := strings.Index(line, "//"); j >= 0 {
					line = line[:j]
				}
				if strings.HasPrefix(line, "func ") {
					fn = line
				}
				for call, fns := range allowed {
					if !strings.Contains(line, call) || strings.HasPrefix(line, "func ") {
						continue
					}
					ok := false
					for _, prefix := range fns {
						if strings.HasPrefix(fn, prefix) {
							ok = true
						}
					}
					if !ok {
						t.Errorf("%s:%d — %q calls %s, which can write a CO reset; a caller that "+
							"is only asking must use uvAvailable()", path, i+1, fn, call)
					}
				}
			}
		}
	}
}
