// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// lighting_off_test.go — a per-zone off keeps the zone's effect.
//
// handleOff used to store a named zone as LightingState{Enabled: false}, which
// discarded mode, colours, speed and brightness: get-state then served a zone
// with no effect, and re-enabling it with `brightness --device` brought it back
// on the all-device effect instead of its own. The handlers reach hardware only
// through the lighting driver, so a recording fake makes them hermetic.

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// fakeLighting is a driver.Lighting that records nothing and fails nothing.
type fakeLighting struct{}

func (fakeLighting) Zones() []string { return []string{"keyboard", "lightbar"} }
func (fakeLighting) Caps() driver.LightingCaps {
	return driver.LightingCaps{
		Zones:         []driver.LightingZone{{Name: "keyboard"}, {Name: "lightbar"}},
		Modes:         []driver.LightingMode{{Name: "static", Color: true}, {Name: "breathe", Color: true, Color2: true, Speed: true}},
		Speeds:        []string{"slow", "normal", "fast"},
		BrightnessMax: 3,
	}
}
func (fakeLighting) Apply(string, api.LightingState) error { return nil }
func (fakeLighting) Off(string) error                      { return nil }
func (fakeLighting) SetBrightness(string, int) error       { return nil }
func (fakeLighting) PresentZones() []string                { return []string{"keyboard", "lightbar"} }
func (fakeLighting) Reopen() error                         { return nil }

func TestZoneOffKeepsTheZonesEffect(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	global := api.LightingState{Enabled: true, Mode: "static", Color: "FF0000", Color2: "000000", Speed: "normal", Brightness: 3}
	own := api.LightingState{Enabled: true, Mode: "breathe", Color: "00FF88", Color2: "0000FF", Speed: "slow", Brightness: 2}

	newDaemon := func() *Daemon {
		return &Daemon{
			hw: &device.Device{Lighting: fakeLighting{}},
			state: api.State{
				Lighting: global,
				Devices:  map[string]api.LightingState{"keyboard": own},
			},
		}
	}

	t.Run("a zone with its own effect keeps it, disabled", func(t *testing.T) {
		d := newDaemon()
		if resp := d.handleOff(request{Device: "keyboard"}); !resp.OK {
			t.Fatalf("off: %+v", resp)
		}
		want := own
		want.Enabled = false
		if got := d.state.Devices["keyboard"]; got != want {
			t.Errorf("keyboard after off = %+v, want %+v", got, want)
		}
		if d.state.Lighting != global {
			t.Errorf("a zone off changed the all-device state: %+v", d.state.Lighting)
		}
	})

	t.Run("a zone showing the all-device effect keeps that", func(t *testing.T) {
		d := newDaemon()
		if resp := d.handleOff(request{Device: "lightbar"}); !resp.OK {
			t.Fatalf("off: %+v", resp)
		}
		want := global
		want.Enabled = false
		if got := d.state.Devices["lightbar"]; got != want {
			t.Errorf("lightbar after off = %+v, want %+v", got, want)
		}
	})

	t.Run("brightness on the zone brings back its own effect", func(t *testing.T) {
		d := newDaemon()
		d.handleOff(request{Device: "keyboard"})
		if resp := d.handleBrightness(request{Device: "keyboard", Brightness: 3}); !resp.OK {
			t.Fatalf("brightness: %+v", resp)
		}
		want := own
		want.Brightness = 3
		if got := d.state.Devices["keyboard"]; got != want {
			t.Errorf("keyboard after off + brightness = %+v, want its own effect back %+v", got, want)
		}
	})

	t.Run("what is saved is what get-state serves", func(t *testing.T) {
		d := newDaemon()
		d.handleOff(request{Device: "keyboard"})
		got := loadState().Devices["keyboard"]
		want := own
		want.Enabled = false
		if got != want {
			t.Errorf("saved keyboard entry = %+v, want %+v", got, want)
		}
	})
}
