// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package buttonpref_test

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/buttonpref"
	"github.com/dahui/voltaire/v2/internal/devicefixture"
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

// A surface that points the user at another one has to name the gesture this
// machine is actually configured for — the whole reason the preference exists is
// that either gesture can reach either surface.
func TestOpenGesture(t *testing.T) {
	t.Parallel()

	cases := []struct {
		single, want buttonpref.Surface
		twice        bool
	}{
		// The default: the window is the double press.
		{buttonpref.Quickbar, buttonpref.Window, true},
		{buttonpref.Quickbar, buttonpref.Quickbar, false},
		// Reversed: now the quickbar is.
		{buttonpref.Window, buttonpref.Quickbar, true},
		{buttonpref.Window, buttonpref.Window, false},
	}
	for _, tc := range cases {
		got := buttonpref.OpenGesture(tc.single, tc.want, "Armoury Crate button")
		if twice := strings.HasSuffix(got, "twice"); twice != tc.twice {
			t.Errorf("OpenGesture(%q, %q) = %q, want twice=%v", tc.single, tc.want, got, tc.twice)
		}
		if !strings.HasPrefix(got, "Press the Armoury Crate button") {
			t.Errorf("OpenGesture(%q, %q) = %q, want a sentence-initial phrase",
				tc.single, tc.want, got)
		}
	}
}

// The name is the device's when it gives one, and a generic one otherwise —
// never another machine's button.
func TestButtonName(t *testing.T) {
	t.Parallel()

	if got := buttonpref.RowLabel("Armoury Crate button"); got != "Armoury Crate button" {
		t.Errorf("RowLabel(declared) = %q", got)
	}
	if got := buttonpref.RowLabel(""); got != "Hardware button" {
		t.Errorf("RowLabel(\"\") = %q, want Hardware button", got)
	}
	if got := buttonpref.OpenGesture(buttonpref.Quickbar, buttonpref.Window, ""); got != "Press the hardware button twice" {
		t.Errorf("OpenGesture with no label = %q", got)
	}
}

// A device without a hardware button has no presses to arrange, and no
// sentence may tell its user to press one.
func TestADeviceWithoutAButton(t *testing.T) {
	syn := devicefixture.Synthetic()
	if buttonpref.ShowPreference(syn) {
		t.Error("ShowPreference = true for a device with no button")
	}
	if !buttonpref.ShowPreference(nil) {
		t.Error("ShowPreference(nil) = false; no answer is not evidence of no button")
	}
	if !buttonpref.ShowPreference(&api.DeviceInfo{Buttons: true}) {
		t.Error("ShowPreference = false for a device with a button")
	}
	got := buttonpref.OpenGestureFor(syn, nil, buttonpref.Quickbar, buttonpref.Window)
	if strings.Contains(got, "Press") || !strings.Contains(got, "expand button") {
		t.Errorf("OpenGestureFor(no button) = %q, want the expand button named", got)
	}
	withButton := &api.DeviceInfo{Buttons: true, ButtonLabel: "Armoury Crate button"}
	if got, want := buttonpref.OpenGestureFor(withButton, nil, buttonpref.Quickbar, buttonpref.Window),
		buttonpref.OpenGesture(buttonpref.Quickbar, buttonpref.Window, "Armoury Crate button"); got != want {
		t.Errorf("OpenGestureFor(button) = %q, want %q", got, want)
	}
}

func TestRowDescriptionNamesTheButton(t *testing.T) {
	if got := buttonpref.RowDescriptionFor(""); got != buttonpref.RowDescription {
		t.Errorf("RowDescriptionFor(\"\") = %q, want %q", got, buttonpref.RowDescription)
	}
	if got := buttonpref.RowDescriptionFor("Armoury Crate button"); !strings.Contains(got, "Armoury Crate button") {
		t.Errorf("RowDescriptionFor = %q, want the button named", got)
	}
}

// A handheld's two buttons, the second the default.
var twoButtons = &api.DeviceInfo{
	Buttons:     true,
	ButtonLabel: "OneXPlayer button",
	HardwareButtons: []api.ButtonInfo{
		{ID: "home", Label: "Home button"},
		{ID: "oxp", Label: "OneXPlayer button", Default: true},
	},
}

func TestChoicesListTheDevicesButtonsThenNone(t *testing.T) {
	got := buttonpref.Choices(twoButtons)
	want := []buttonpref.Choice{
		{ID: "home", Label: "Home button"},
		{ID: "oxp", Label: "OneXPlayer button"},
		{ID: api.DrawerButtonNone, Label: buttonpref.NoButtonLabel},
	}
	if len(got) != len(want) {
		t.Fatalf("Choices = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Choices[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	// No list to choose from: no daemon, or one that predates the list.
	for _, info := range []*api.DeviceInfo{nil, {Buttons: true, ButtonLabel: "Armoury Crate button"}} {
		if buttonpref.ShowChooser(info) || buttonpref.Choices(info) != nil {
			t.Errorf("a chooser offered for %+v", info)
		}
	}
}

func TestDrawerFollowsTheDaemonsAnswer(t *testing.T) {
	for _, c := range []struct {
		desc      string
		info      *api.DeviceInfo
		st        *api.State
		wantID    string
		wantLabel string
		wantOK    bool
		wantText  string
	}{
		{"the daemon's resolved choice", twoButtons, &api.State{DrawerButton: "home"}, "home", "Home button", true, "Home button"},
		{"no state yet: the device default", twoButtons, nil, "oxp", "OneXPlayer button", true, "OneXPlayer button"},
		{"the user chose none", twoButtons, &api.State{DrawerButton: api.DrawerButtonNone}, "", "", false, buttonpref.NoButtonLabel},
		{"an older daemon: its one label", &api.DeviceInfo{Buttons: true, ButtonLabel: "Armoury Crate button"}, &api.State{}, "", "Armoury Crate button", true, "Armoury Crate button"},
		{"a device with no button", &api.DeviceInfo{}, &api.State{}, "", "", false, buttonpref.NoButtonLabel},
		{"no document", nil, nil, "", "", true, "Hardware button"},
	} {
		id, label, ok := buttonpref.Drawer(c.info, c.st)
		if id != c.wantID || label != c.wantLabel || ok != c.wantOK {
			t.Errorf("%s: Drawer = (%q, %q, %v), want (%q, %q, %v)", c.desc, id, label, ok, c.wantID, c.wantLabel, c.wantOK)
		}
		if got := buttonpref.ChooserText(c.info, c.st); got != c.wantText {
			t.Errorf("%s: ChooserText = %q, want %q", c.desc, got, c.wantText)
		}
	}
}

// With no button opening voltaire, the editor note must not tell the user to
// press one.
func TestOpenGestureWithNoButtonChosen(t *testing.T) {
	got := buttonpref.OpenGestureFor(twoButtons, &api.State{DrawerButton: api.DrawerButtonNone},
		buttonpref.Quickbar, buttonpref.Window)
	if strings.Contains(got, "Press") {
		t.Errorf("OpenGestureFor(none chosen) = %q, names a button that does nothing", got)
	}
	got = buttonpref.OpenGestureFor(twoButtons, &api.State{DrawerButton: "home"}, buttonpref.Quickbar, buttonpref.Window)
	if !strings.Contains(got, "Home button twice") {
		t.Errorf("OpenGestureFor(home) = %q, want the chosen button's double press", got)
	}
}
