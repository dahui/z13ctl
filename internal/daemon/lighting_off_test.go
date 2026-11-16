// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// lighting_off_test.go — a per-zone off keeps the zone's effect.
//
// handleOff used to store a named zone as LightingState{Enabled: false},
// discarding mode, colours, speed and brightness: get-state then served a zone
// at brightness 0, and `brightness --device` brought it back on the all-device
// effect instead of its own. handleOff itself writes HID, so the state it
// stores is decided by zoneOffState, which is pure.

import (
	"testing"

	"github.com/dahui/z13ctl/api"
)

func TestZoneOffState(t *testing.T) {
	global := api.LightingState{Enabled: true, Mode: "static", Color: "FF0000", Color2: "000000", Speed: "normal", Brightness: 3}
	own := api.LightingState{Enabled: true, Mode: "breathe", Color: "00FF88", Color2: "0000FF", Speed: "slow", Brightness: 2}
	disabled := func(ls api.LightingState) api.LightingState {
		ls.Enabled = false
		return ls
	}

	tests := []struct {
		name  string
		state api.State
		zone  string
		want  api.LightingState
	}{
		{
			name:  "a zone with its own effect keeps it, brightness included",
			state: api.State{Lighting: global, Devices: map[string]api.LightingState{"keyboard": own}},
			zone:  "keyboard",
			want:  disabled(own),
		},
		{
			name:  "a zone showing the all-device effect keeps that",
			state: api.State{Lighting: global, Devices: map[string]api.LightingState{"keyboard": own}},
			zone:  "lightbar",
			want:  disabled(global),
		},
		{
			name:  "no per-zone entries at all",
			state: api.State{Lighting: global},
			zone:  "keyboard",
			want:  disabled(global),
		},
		{
			// An entry an older daemon wrote: the strings are repaired from
			// the all-device state, as on load; its brightness is gone and
			// stays 0 rather than being invented.
			name:  "an entry zeroed by an older off is repaired, not trusted",
			state: api.State{Lighting: global, Devices: map[string]api.LightingState{"keyboard": {Enabled: false}}},
			zone:  "keyboard",
			want:  api.LightingState{Mode: "static", Color: "FF0000", Color2: "000000", Speed: "normal"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := cloneState(tt.state)
			if got := zoneOffState(tt.state, tt.zone); got != tt.want {
				t.Errorf("zoneOffState(%q) = %+v, want %+v", tt.zone, got, tt.want)
			}
			if tt.state.Lighting != before.Lighting || len(tt.state.Devices) != len(before.Devices) {
				t.Errorf("zoneOffState modified the state it was given")
			}
		})
	}
}
