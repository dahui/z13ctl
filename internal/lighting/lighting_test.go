// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package lighting

import (
	"testing"

	"github.com/dahui/z13ctl/api"
)

func TestResolveMode(t *testing.T) {
	tests := []struct {
		name string
		ls   api.LightingState
		want string
	}{
		{
			// The daemon keeps the previous mode recorded while disabled so that
			// re-enabling restores it. The drawer must still show "off" — showing
			// "breathe" as active while the keyboard is dark is a lie, and it is
			// what made a disabled zone come back looking enabled after a reboot.
			name: "disabled keeps its mode but shows off",
			ls:   api.LightingState{Enabled: false, Mode: "breathe"},
			want: ModeOff,
		},
		{
			name: "disabled with no mode",
			ls:   api.LightingState{Enabled: false},
			want: ModeOff,
		},
		{
			name: "enabled uses its mode",
			ls:   api.LightingState{Enabled: true, Mode: "cycle"},
			want: "cycle",
		},
		{
			// A per-zone entry can legitimately be stored with only some fields
			// set, which used to leave no mode button selected at all.
			name: "enabled with no mode falls back to the default",
			ls:   api.LightingState{Enabled: true},
			want: DefaultMode,
		},
		{
			// A newer daemon may know modes this build does not; pass them through
			// rather than substituting a default the user did not choose.
			name: "unknown mode passes through when enabled",
			ls:   api.LightingState{Enabled: true, Mode: "future-effect"},
			want: "future-effect",
		},
		{
			name: "zero state reads as off",
			ls:   api.LightingState{},
			want: ModeOff,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveMode(tt.ls); got != tt.want {
				t.Errorf("ResolveMode(%+v) = %q, want %q", tt.ls, got, tt.want)
			}
		})
	}
}

func TestResolveSpeed(t *testing.T) {
	if got := ResolveSpeed(api.LightingState{Speed: "fast"}); got != "fast" {
		t.Errorf("ResolveSpeed = %q, want fast", got)
	}
	if got := ResolveSpeed(api.LightingState{}); got != DefaultSpeed {
		t.Errorf("ResolveSpeed with no speed = %q, want %q", got, DefaultSpeed)
	}
}

func TestControlsForKnownModes(t *testing.T) {
	tests := []struct {
		mode string
		want Controls
	}{
		{mode: "static", want: Controls{Color1: true, Brightness: true}},
		{mode: "breathe", want: Controls{Color1: true, Color2: true, Speed: true, Brightness: true}},
		{mode: "cycle", want: Controls{Speed: true, Brightness: true}},
		{mode: "rainbow", want: Controls{Speed: true, Brightness: true}},
		{mode: "strobe", want: Controls{Color1: true, Speed: true, Brightness: true}},
		{mode: ModeOff, want: Controls{}},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			if got := ControlsFor(tt.mode); got != tt.want {
				t.Errorf("ControlsFor(%q) = %+v, want %+v", tt.mode, got, tt.want)
			}
		})
	}
}

// Hiding every control for a mode this build does not recognise would make it look
// broken; a newer daemon's mode should still be operable.
func TestControlsForUnknownModeShowsEverything(t *testing.T) {
	got := ControlsFor("future-effect")
	want := Controls{Color1: true, Color2: true, Speed: true, Brightness: true}
	if got != want {
		t.Errorf("ControlsFor(unknown) = %+v, want %+v", got, want)
	}
	if ControlsFor("") != want {
		t.Errorf("ControlsFor(empty) should also show everything")
	}
}

// Off must hide everything: leaving a colour swatch or the brightness slider live
// while lighting is disabled invites edits that cannot take effect.
func TestOffHidesEveryControl(t *testing.T) {
	if got := ControlsFor(ModeOff); got != (Controls{}) {
		t.Errorf("ControlsFor(off) = %+v, want all false", got)
	}
}

// Every mode that animates needs a speed control and vice versa — the table is
// hand-maintained, so tie the two together.
func TestAnimatedModesHaveSpeed(t *testing.T) {
	animated := map[string]bool{"breathe": true, "cycle": true, "rainbow": true, "strobe": true}
	for mode, c := range modeControls {
		if animated[mode] != c.Speed {
			t.Errorf("mode %q: animated=%v but Speed=%v", mode, animated[mode], c.Speed)
		}
	}
}

// Anything other than off should keep brightness available, or the user loses the
// only control that always applies.
func TestEveryVisibleModeKeepsBrightness(t *testing.T) {
	for mode, c := range modeControls {
		if mode == ModeOff {
			continue
		}
		if !c.Brightness {
			t.Errorf("mode %q has no brightness control", mode)
		}
	}
}

func TestKnownMode(t *testing.T) {
	for _, mode := range []string{"static", "breathe", "cycle", "rainbow", "strobe", ModeOff} {
		if !KnownMode(mode) {
			t.Errorf("KnownMode(%q) = false, want true", mode)
		}
	}
	for _, mode := range []string{"", "future-effect", "STATIC"} {
		if KnownMode(mode) {
			t.Errorf("KnownMode(%q) = true, want false", mode)
		}
	}
}

func TestStateForZone(t *testing.T) {
	global := api.LightingState{Enabled: true, Mode: "static", Color: "FF0000"}
	kb := api.LightingState{Enabled: true, Mode: "breathe", Color: "00FF00"}

	s := &api.State{
		Lighting: global,
		Devices:  map[string]api.LightingState{"keyboard": kb},
	}

	if got := StateForZone(s, "keyboard"); got.Mode != "breathe" {
		t.Errorf("keyboard zone = %+v, want the per-device entry", got)
	}
	// A zone with no per-device entry falls back to the global state.
	if got := StateForZone(s, "lightbar"); got.Mode != "static" {
		t.Errorf("lightbar zone = %+v, want the global entry", got)
	}
	// Nil state must not panic, and must read as disabled.
	got := StateForZone(nil, "keyboard")
	if got != (api.LightingState{}) {
		t.Errorf("StateForZone(nil) = %+v, want the zero state", got)
	}
	if ResolveMode(got) != ModeOff {
		t.Error("the zero state should resolve to off")
	}
}

// A per-device entry stored as disabled must win over an enabled global one, or
// turning off one zone would appear to have failed.
func TestStateForZonePrefersADisabledDeviceEntry(t *testing.T) {
	s := &api.State{
		Lighting: api.LightingState{Enabled: true, Mode: "static"},
		Devices:  map[string]api.LightingState{"lightbar": {Enabled: false}},
	}
	if got := ResolveMode(StateForZone(s, "lightbar")); got != ModeOff {
		t.Errorf("disabled lightbar resolved to %q, want off", got)
	}
}

func TestResolveBrightness(t *testing.T) {
	tests := []struct {
		name string
		ls   api.LightingState
		want int
	}{
		{
			// The exact state z13ctl's per-zone off writes: LightingState{Enabled:
			// false}, every other field zeroed. Adopting the 0 sent brightness 0 on
			// the next apply, which is the hardware's off level — so re-enabling a
			// zone left the keyboard dark while reporting success.
			name: "disabled with everything zeroed",
			ls:   api.LightingState{Enabled: false},
			want: DefaultBrightness,
		},
		{
			// Full off preserves the rest of the global entry, so the stored value is
			// real — but it still must not be adopted while disabled, or the same
			// trap applies whenever that value happens to be 0.
			name: "disabled with a stored brightness",
			ls:   api.LightingState{Enabled: false, Mode: "breathe", Brightness: 0},
			want: DefaultBrightness,
		},
		{
			name: "disabled with a non-zero stored brightness",
			ls:   api.LightingState{Enabled: false, Brightness: 2},
			want: DefaultBrightness,
		},
		{
			name: "enabled passes its value through",
			ls:   api.LightingState{Enabled: true, Brightness: 2},
			want: 2,
		},
		{
			name: "enabled at maximum",
			ls:   api.LightingState{Enabled: true, Brightness: 3},
			want: 3,
		},
		{
			// A deliberate slider choice on an enabled zone. Substituting here would
			// misreport the hardware, which is the opposite failure.
			name: "enabled at zero is the user's choice",
			ls:   api.LightingState{Enabled: true, Brightness: 0},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveBrightness(tt.ls); got != tt.want {
				t.Errorf("ResolveBrightness(%+v) = %d, want %d", tt.ls, got, tt.want)
			}
		})
	}
}

// TestResolveBrightnessNeverDarkensAnOffZone is the property that matters: for any
// state the drawer shows as off, the slider must offer a level that actually lights
// the keyboard when the user picks a mode.
func TestResolveBrightnessNeverDarkensAnOffZone(t *testing.T) {
	for stored := -1; stored <= 4; stored++ {
		ls := api.LightingState{Enabled: false, Brightness: stored}
		if got := ResolveBrightness(ls); got <= 0 {
			t.Errorf("stored %d: ResolveBrightness = %d, which leaves the zone dark "+
				"when re-enabled", stored, got)
		}
	}
}

// The zero LightingState is what StateForZone returns when the daemon has told us
// nothing, so it goes down the same path.
func TestResolveBrightnessOfZeroValue(t *testing.T) {
	if got := ResolveBrightness(api.LightingState{}); got != DefaultBrightness {
		t.Errorf("ResolveBrightness(zero) = %d, want %d", got, DefaultBrightness)
	}
}
