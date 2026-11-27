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

	// Both packages, because the first version of this guard scanned only
	// internal/daemon and missed cmd/tdp.go's no-daemon reset entirely — a real
	// sixth call site, on the path taken whenever the daemon is down. A guard
	// scoped to one package is a guard that certifies the package, not the rule.
	sites := findCalls(t, []string{".", "../../cmd"}, "Undervolt.Reset")
	for _, c := range sites {
		// uvApplied inside the daemon, daemon.UndervoltApplied from cmd/ —
		// the same predicate over the same state file, by necessity: the CLI
		// has no Daemon to ask and must not answer differently.
		if !c.callsBefore("uvApplied", "UndervoltApplied") {
			t.Errorf("%s: Undervolt.Reset() in %s is not gated on an applied-offset check "+
				"(uvApplied / daemon.UndervoltApplied); an offset that was never applied must never be cleared",
				c.pos, c.fnName())
		}
	}
	// Guard the premise: if a refactor renames the call, this test would pass by
	// finding nothing at all. Five in internal/daemon plus two in cmd/.
	if len(sites) < 7 {
		t.Fatalf("found %d Undervolt.Reset() call sites, expected at least 7 — "+
			"this guard needs updating with the rename", len(sites))
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
		"uvProbe": {
			"Daemon.handleUndervolt",      // writes the requested offset
			"Daemon.applyCustomHW",        // writes the profile's offset
			"Daemon.reconcileOnce",        // re-applies after a lost resume
			"Daemon.uvApplied",            // only once state says applied
			"Daemon.applyDeviceUndervolt", // writes the slider device's offset
		},
		"ProbeAvailable": {"Daemon.uvProbe"},
	}
	for call, fns := range allowed {
		sites := findCalls(t, []string{".", "../../cmd"}, call)
		if len(sites) == 0 {
			t.Fatalf("no call to %s found; this guard needs updating with the rename", call)
		}
		for _, c := range sites {
			ok := false
			for _, fn := range fns {
				ok = ok || c.fnName() == fn
			}
			if !ok {
				t.Errorf("%s: %s calls %s, which can write a CO reset; a caller that "+
					"is only asking must use uvAvailable()", c.pos, c.fnName(), call)
			}
		}
	}
}
