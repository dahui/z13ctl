// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package display

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
)

// z13Reply is the real `kscreen-doctor -j` output from the development
// machine's panel, captured rather than hand-written: the fields this package
// reads are a small corner of a large document, and the document has details an
// invented fixture would not — modes named "1600x1200@60" whose refreshRate is
// 59.868, and mode ids that are decimal strings rather than the array index.
func z13Reply(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/kscreen-z13.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestParseKScreenReadsTheZ13Panel(t *testing.T) {
	t.Parallel()

	outs, err := parseKScreen(z13Reply(t))
	if err != nil {
		t.Fatalf("parseKScreen: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("outputs = %d, want 1", len(outs))
	}
	o := outs[0]
	if o.Name != "eDP-1" || !o.Enabled || !o.Connected {
		t.Errorf("output = %+v, want an enabled connected eDP-1", o)
	}
	if o.CurrentModeID != "1" {
		t.Errorf("CurrentModeID = %q, want %q", o.CurrentModeID, "1")
	}
	cur, ok := o.Current()
	if !ok {
		t.Fatal("Current() reported no mode for a current-mode id that is in the list")
	}
	if cur.Width != 2560 || cur.Height != 1600 || cur.RefreshHz != 180 {
		t.Errorf("current mode = %+v, want 2560x1600@180", cur)
	}
}

func TestParseKScreenRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := parseKScreen([]byte("not json")); err == nil {
		t.Fatal("parseKScreen accepted non-JSON")
	}
}

// The panel lists 26 modes across a dozen resolutions. The control offers the
// two that share the resolution it is running, and nothing else: a dropdown
// carrying 1280x720 would be a resolution picker wearing a refresh rate's
// label.
func TestRatesAreTheCurrentResolutionOnly(t *testing.T) {
	t.Parallel()

	outs, err := parseKScreen(z13Reply(t))
	if err != nil {
		t.Fatalf("parseKScreen: %v", err)
	}
	rates := Rates(outs[0])

	var labels []string
	for _, r := range rates {
		labels = append(labels, r.Label)
	}
	if want := []string{"180 Hz", "60 Hz"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels = %v, want %v (highest first)", labels, want)
	}
	if !rates[0].Current {
		t.Error("the running mode is not marked current")
	}
	if rates[1].Current {
		t.Error("a mode that is not running is marked current")
	}
	if rates[1].ModeID == "" {
		t.Error("rate carries no mode id, so Apply would have nothing to send")
	}
}

// Two modes a hundredth of a hertz apart are one menu entry, not two identical
// ones — and the entry has to be the one that is running, or the control cannot
// display the state it is in.
// The panel reports 59.868 for the mode it calls "1600x1200@60". Every other
// piece of software on the machine says 60, so the label does too — while the
// value the mode is selected by stays exact.
func TestRatesRoundTheLabelAndNotTheValue(t *testing.T) {
	t.Parallel()

	outs, err := parseKScreen(z13Reply(t))
	if err != nil {
		t.Fatalf("parseKScreen: %v", err)
	}
	o := outs[0]
	o.CurrentModeID = "5" // 1600x1200@180
	rates := Rates(o)
	if len(rates) != 2 {
		t.Fatalf("rates = %+v, want 180 and 60", rates)
	}
	if rates[1].Label != "60 Hz" {
		t.Errorf("label = %q, want %q", rates[1].Label, "60 Hz")
	}
	if rates[1].Hz == 60 {
		t.Errorf("Hz = %v; the exact rate must survive the rounded label", rates[1].Hz)
	}
}

func TestRatesCollapseDuplicatesAndKeepTheRunningMode(t *testing.T) {
	t.Parallel()

	o := Output{
		Name: "eDP-1", Enabled: true, Connected: true, CurrentModeID: "slow-60",
		Modes: []Mode{
			{ID: "fast-60", Width: 1920, Height: 1080, RefreshHz: 60.00},
			{ID: "slow-60", Width: 1920, Height: 1080, RefreshHz: 59.94},
			{ID: "144", Width: 1920, Height: 1080, RefreshHz: 144},
		},
	}
	rates := Rates(o)
	if len(rates) != 2 {
		t.Fatalf("rates = %d (%+v), want 2", len(rates), rates)
	}
	if rates[0].Label != "144 Hz" || rates[1].Label != "60 Hz" {
		t.Fatalf("labels = %q/%q, want 144 Hz/60 Hz", rates[0].Label, rates[1].Label)
	}
	if rates[1].ModeID != "slow-60" {
		t.Errorf("60 Hz entry is mode %q; the running mode must win the collapse", rates[1].ModeID)
	}
	if !rates[1].Current {
		t.Error("the collapsed entry lost the current flag")
	}
}

// The higher exact rate wins when neither duplicate is running, so a panel that
// can do a true 60 does not advertise it as the 59.94 mode.
func TestRatesKeepTheHigherOfTwoIdenticalLabels(t *testing.T) {
	t.Parallel()

	o := Output{
		Enabled: true, Connected: true, CurrentModeID: "144",
		Modes: []Mode{
			{ID: "slow-60", Width: 1920, Height: 1080, RefreshHz: 59.94},
			{ID: "fast-60", Width: 1920, Height: 1080, RefreshHz: 60.00},
			{ID: "144", Width: 1920, Height: 1080, RefreshHz: 144},
		},
	}
	rates := Rates(o)
	if len(rates) != 2 || rates[1].ModeID != "fast-60" {
		t.Fatalf("rates = %+v, want the 60 Hz slot to hold fast-60", rates)
	}
}

func TestRatesWithoutACurrentModeAreNone(t *testing.T) {
	t.Parallel()

	o := Output{
		Enabled: true, Connected: true, CurrentModeID: "gone",
		Modes: []Mode{{ID: "1", Width: 1920, Height: 1080, RefreshHz: 60}},
	}
	// No resolution to hold fixed means no honest answer about which modes are
	// alternatives, so the control shows nothing rather than every mode.
	if rates := Rates(o); rates != nil {
		t.Fatalf("rates = %+v, want none", rates)
	}
}

func TestFormatHzRounds(t *testing.T) {
	t.Parallel()

	cases := map[float64]string{
		180:   "180 Hz",
		59.96: "60 Hz",
		59.94: "60 Hz",
		144:   "144 Hz",
		29.97: "30 Hz",
	}
	for hz, want := range cases {
		if got := FormatHz(hz); got != want {
			t.Errorf("FormatHz(%v) = %q, want %q", hz, got, want)
		}
	}
}

func TestPrimaryPrefersTheRankedEnabledOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		outs []Output
		want string
		ok   bool
	}{
		{
			name: "priority 1 wins",
			outs: []Output{
				{Name: "HDMI-1", Enabled: true, Connected: true, Priority: 2},
				{Name: "eDP-1", Enabled: true, Connected: true, Priority: 1},
			},
			want: "eDP-1", ok: true,
		},
		{
			name: "a disabled primary is not the primary",
			outs: []Output{
				{Name: "eDP-1", Enabled: false, Connected: true, Priority: 1},
				{Name: "HDMI-1", Enabled: true, Connected: true, Priority: 2},
			},
			want: "HDMI-1", ok: true,
		},
		{
			name: "a disconnected output is not offered",
			outs: []Output{
				{Name: "HDMI-1", Enabled: true, Connected: false, Priority: 1},
				{Name: "eDP-1", Enabled: true, Connected: true, Priority: 2},
			},
			want: "eDP-1", ok: true,
		},
		{
			name: "unranked falls back to the first enabled",
			outs: []Output{
				{Name: "eDP-1", Enabled: true, Connected: true},
				{Name: "HDMI-1", Enabled: true, Connected: true},
			},
			want: "eDP-1", ok: true,
		},
		{
			name: "a ranked output beats an unranked one",
			outs: []Output{
				{Name: "eDP-1", Enabled: true, Connected: true},
				{Name: "HDMI-1", Enabled: true, Connected: true, Priority: 1},
			},
			want: "HDMI-1", ok: true,
		},
		{
			name: "nothing lit is no answer, never a wrong one",
			outs: []Output{{Name: "eDP-1", Connected: true}},
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Primary(tc.outs)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got.Name != tc.want {
				t.Errorf("primary = %q, want %q", got.Name, tc.want)
			}
		})
	}
}

func TestEnabledCount(t *testing.T) {
	t.Parallel()

	outs := []Output{
		{Enabled: true, Connected: true},
		{Enabled: true, Connected: false},
		{Enabled: false, Connected: true},
		{Enabled: true, Connected: true},
	}
	if got := EnabledCount(outs); got != 2 {
		t.Errorf("EnabledCount = %d, want 2", got)
	}
}

// The seam tests below replace package vars, so they cannot run in parallel
// with each other or with anything else that reads them.

func withStubs(t *testing.T, path func(string) (string, error), run func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	oldPath, oldRun := lookPath, runner
	lookPath, runner = path, run
	t.Cleanup(func() { lookPath, runner = oldPath, oldRun })
}

func foundOnPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func TestQueryAndApplyReportNoBackend(t *testing.T) {
	withStubs(t,
		func(string) (string, error) { return "", errors.New("not found") },
		func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("ran the backend after the lookup failed")
			return nil, nil
		})

	if _, err := Query(); !errors.Is(err, ErrNoBackend) {
		t.Errorf("Query err = %v, want ErrNoBackend", err)
	}
	if err := Apply("eDP-1", "1"); !errors.Is(err, ErrNoBackend) {
		t.Errorf("Apply err = %v, want ErrNoBackend", err)
	}
}

// The mode is addressed by id in one atomic argument. A wrong shape here is a
// no-op the user reads as a dead control, so the string is pinned.
func TestApplySendsTheModeID(t *testing.T) {
	var got []string
	withStubs(t, foundOnPath, func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	})

	if err := Apply("eDP-1", "2"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"kscreen-doctor", "output.eDP-1.mode.2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("command = %q, want %q", got, want)
	}
}

func TestApplyRefusesEmptyArguments(t *testing.T) {
	withStubs(t, foundOnPath, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("ran the backend with an incomplete request")
		return nil, nil
	})

	if err := Apply("", "1"); err == nil {
		t.Error("Apply accepted an empty output name")
	}
	if err := Apply("eDP-1", ""); err == nil {
		t.Error("Apply accepted an empty mode id")
	}
}

func TestQueryParsesTheBackendReply(t *testing.T) {
	data := z13Reply(t)
	withStubs(t, foundOnPath, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) != 1 || args[0] != "-j" {
			t.Errorf("query args = %q, want [-j]", args)
		}
		return data, nil
	})

	outs, err := Query()
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(outs) != 1 || outs[0].Name != "eDP-1" {
		t.Fatalf("outputs = %+v, want one eDP-1", outs)
	}
}

func TestQueryReportsABackendFailure(t *testing.T) {
	withStubs(t, foundOnPath, func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("compositor is not listening")
	})

	if _, err := Query(); err == nil {
		t.Fatal("Query swallowed a backend failure")
	}
}
