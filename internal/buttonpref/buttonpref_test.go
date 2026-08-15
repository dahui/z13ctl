// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package buttonpref_test

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/buttonpref"
)

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want buttonpref.Surface
		ok   bool
	}{
		{"quickbar", buttonpref.Quickbar, true},
		{"window", buttonpref.Window, true},
		// Hand-editable file: fold case and space rather than refusing.
		{"Window", buttonpref.Window, true},
		{"  WINDOW  ", buttonpref.Window, true},
		// Every failure still yields a usable surface — a bad config costs a
		// warning, never a button that does nothing.
		{"", buttonpref.Default(), false},
		{"drawer", buttonpref.Default(), false},
		{"full", buttonpref.Default(), false},
	}
	for _, c := range cases {
		got, ok := buttonpref.Parse(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Parse(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// The default is the arrangement voltaire shipped with, and changing it would
// silently change what the button does for every existing user.
func TestDefaultIsTheQuickbar(t *testing.T) {
	t.Parallel()
	if buttonpref.Default() != buttonpref.Quickbar {
		t.Fatalf("Default() = %q; want %q", buttonpref.Default(), buttonpref.Quickbar)
	}
}

// Other is an involution over exactly the two surfaces: there is no third
// value, and no value is its own opposite — which is what makes one stored
// field enough to describe both gestures.
func TestOtherIsAnInvolution(t *testing.T) {
	t.Parallel()
	for _, s := range buttonpref.Options() {
		if s.Other() == s {
			t.Errorf("%q.Other() is itself; a double press would open what a single press already did", s)
		}
		if s.Other().Other() != s {
			t.Errorf("%q.Other().Other() = %q; want %q", s, s.Other().Other(), s)
		}
	}
}

// Options must round-trip through Parse: they are written to config.toml and
// read back, so an option the parser rejects would reset itself on restart.
func TestOptionsRoundTripThroughParse(t *testing.T) {
	t.Parallel()
	opts := buttonpref.Options()
	if len(opts) != 2 {
		t.Fatalf("Options() has %d entries; want the two surfaces", len(opts))
	}
	seen := map[buttonpref.Surface]bool{}
	for _, s := range opts {
		if seen[s] {
			t.Errorf("Options() repeats %q", s)
		}
		seen[s] = true
		got, ok := buttonpref.Parse(string(s))
		if !ok || got != s {
			t.Errorf("Parse(%q) = %q,%v; an offered option must parse back to itself", s, got, ok)
		}
		if s.Label() == "" {
			t.Errorf("%q has no label", s)
		}
	}
}

// Summary must name *both* gestures, and name them the right way round. The
// double press is the half nobody discovers on their own, so a summary that
// described only the single press would leave the setting looking like it
// turned the other surface off.
func TestSummaryNamesBothGestures(t *testing.T) {
	t.Parallel()
	for _, s := range buttonpref.Options() {
		got := buttonpref.Summary(s)
		single := strings.ToLower(s.Label())
		double := strings.ToLower(s.Other().Label())

		i, j := strings.Index(got, single), strings.Index(got, double)
		if i < 0 || j < 0 {
			t.Fatalf("Summary(%q) = %q; must name both %q and %q", s, got, single, double)
		}
		if i > j {
			t.Errorf("Summary(%q) = %q; names the double-press surface before the single-press one", s, got)
		}
		if !strings.Contains(got, "double") {
			t.Errorf("Summary(%q) = %q; does not say the second gesture is a double press", s, got)
		}
	}
}
