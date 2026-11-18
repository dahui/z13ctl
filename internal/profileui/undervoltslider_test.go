// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

func TestUndervoltSliderValue(t *testing.T) {
	gaming := map[string]api.CustomProfile{
		"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -25}},
		"plain":  {Name: "plain", TDP: &api.TDPState{PL1SPL: 20}},
	}
	cases := []struct {
		desc   string
		s      *api.State
		want   int
		wantOK bool
	}{
		{"no daemon", nil, 0, false},
		{"unavailable", &api.State{Profile: "balanced"}, 0, false},
		{"nothing set", &api.State{Profile: "balanced", UndervoltAvailable: true}, 0, true},
		{"the setting", &api.State{Profile: "balanced", UndervoltAvailable: true, DeviceUndervolt: &api.UndervoltState{CPUCO: -10}}, -10, true},
		{"a profile's own wins", &api.State{Profile: "gaming", UndervoltAvailable: true, CustomProfiles: gaming,
			DeviceUndervolt: &api.UndervoltState{CPUCO: -10}}, -25, true},
		{"a profile with none runs the setting", &api.State{Profile: "plain", UndervoltAvailable: true, CustomProfiles: gaming,
			DeviceUndervolt: &api.UndervoltState{CPUCO: -10}}, -10, true},
	}
	for _, c := range cases {
		got, ok := UndervoltSliderValue(c.s)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: got %d, %v; want %d, %v", c.desc, got, ok, c.want, c.wantOK)
		}
	}
}

func TestUndervoltSliderNote(t *testing.T) {
	profiles := map[string]api.CustomProfile{
		"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -25}},
		"plain":  {Name: "plain", TDP: &api.TDPState{PL1SPL: 20}},
	}
	cases := []struct {
		desc string
		s    *api.State
		want string // substring; "" means no note
	}{
		{"firmware profile", &api.State{Profile: "balanced", UndervoltAvailable: true}, ""},
		{"unavailable", &api.State{Profile: "balanced"}, "ryzen_smu"},
		{"profile with an offset", &api.State{Profile: "gaming", UndervoltAvailable: true, CustomProfiles: profiles}, "Set by the gaming profile"},
		{"profile without", &api.State{Profile: "plain", UndervoltAvailable: true, CustomProfiles: profiles}, "leaves the plain profile"},
	}
	for _, c := range cases {
		got := UndervoltSliderNote(c.s)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: note %q, want %q", c.desc, got, c.want)
		}
	}
}

func TestUndervoltNoteOn(t *testing.T) {
	if UndervoltNoteOn(false) != UndervoltNote() {
		t.Error("the usual model's note changed")
	}
	if strings.Contains(UndervoltNoteOn(true), "resets them to 0") {
		t.Error("the slider model's note claims a firmware profile resets the offset; there the setting runs")
	}
}
