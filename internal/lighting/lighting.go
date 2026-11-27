// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package lighting holds the drawer's RGB lighting rules: which mode a daemon
// state represents, which controls that mode needs, and what to fall back to when
// state is missing.
//
// Separate from internal/gui because that package needs CGO and GTK4 headers and
// cannot be unit tested. These are decisions about daemon state, not widgets.
package lighting

import (
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
)

// Defaults used when daemon state is unavailable — before the first sync, or when
// the daemon is not running.
const (
	DefaultColor1     = "FF0000"
	DefaultColor2     = "000000"
	DefaultMode       = "static"
	DefaultSpeed      = "normal"
	DefaultBrightness = 3 // clamped to the device's scale by the caller

	// ModeOff is the drawer's pseudo-mode for "lighting disabled". The daemon
	// represents this as Enabled=false, and the drawer needs a selectable button
	// for it.
	//
	// A per-zone off, the only kind the drawer issues, keeps the zone's effect
	// and flips Enabled on a 2.0 daemon (and z13ctl 1.4.3). Older z13ctl
	// daemons instead store LightingState{Enabled: false} with mode, colours,
	// speed and brightness all zeroed, and a state file written by one still
	// carries such entries until the zone is next set. So every field read out of a disabled state still
	// needs a fallback — see ResolveBrightness for what happens when one does
	// not have it.
	ModeOff = "off"
)

// Controls says which of the lighting sub-controls apply to a mode. A mode that
// does not animate has no speed; one that ignores colour has no swatches.
type Controls struct {
	Color1     bool
	Color2     bool
	Speed      bool
	Brightness bool
}

// Zone is one lighting zone: the name the daemon takes and the label to show.
type Zone struct {
	Name, Label string
}

// Caps is the device's lighting as the drawer needs it: zones, effects with
// the inputs each takes, speeds, and the brightness scale. CapsFrom builds it
// from the device document; nothing in the GUI restates a mode list.
type Caps struct {
	Zones         []Zone
	Modes         []api.LightingMode
	Speeds        []string
	BrightnessMax int
	// BrightnessNames names each level from 0 to BrightnessMax, lower case as
	// the device serves them; empty when the device names none.
	BrightnessNames []string
}

// fallback is what the drawer offered before the daemon described its
// lighting — the Z13's Aura set. It is what CapsFrom answers with no document
// (no daemon), the keep-everything posture limits.FromDevice takes, and what
// fills the fields a daemon older than them leaves out.
var fallback = Caps{
	Zones: []Zone{{"keyboard", "Keyboard"}, {"lightbar", "Lightbar"}},
	Modes: []api.LightingMode{
		{Name: "static", Label: "Static", Color: true},
		{Name: "breathe", Label: "Breathe", Color: true, Color2: true, Speed: true},
		{Name: "cycle", Label: "Cycle", Speed: true},
		{Name: "rainbow", Label: "Rainbow", Speed: true},
		{Name: "strobe", Label: "Strobe", Color: true, Speed: true},
	},
	Speeds:          []string{"slow", "normal", "fast"},
	BrightnessMax:   3,
	BrightnessNames: []string{"off", "low", "medium", "high"},
}

// CapsFrom reads the device document's lighting section. A nil document means
// the daemon did not answer, so the fallback stands; a document with no
// lighting section means the device has none, so the answer is empty. Fields
// an older daemon does not send are filled from the fallback one by one.
func CapsFrom(doc *api.DeviceInfo) Caps {
	if doc == nil {
		return fallback
	}
	li := doc.Lighting
	if li == nil {
		return Caps{}
	}
	c := Caps{Modes: li.Modes, Speeds: li.Speeds, BrightnessMax: li.BrightnessMax}
	for i, name := range li.Zones {
		label := ""
		if i < len(li.Labels) {
			label = li.Labels[i]
		}
		if label == "" {
			label = titleCase(name)
		}
		c.Zones = append(c.Zones, Zone{Name: name, Label: label})
	}
	if len(c.Modes) == 0 {
		c.Modes = fallback.Modes
	}
	if len(c.Speeds) == 0 {
		c.Speeds = fallback.Speeds
	}
	if c.BrightnessMax <= 0 {
		c.BrightnessMax = fallback.BrightnessMax
	}
	// Names only when there is one per level; a daemon older than the field
	// sends none, and on the scale the fallback describes its names are what
	// that daemon's levels always meant.
	switch {
	case len(li.BrightnessNames) == c.BrightnessMax+1:
		c.BrightnessNames = li.BrightnessNames
	case len(li.BrightnessNames) == 0 && c.BrightnessMax == fallback.BrightnessMax:
		c.BrightnessNames = fallback.BrightnessNames
	}
	return c
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ModeNames is the effect buttons to build, in order: the device's modes,
// then the drawer's ModeOff.
func (c Caps) ModeNames() []string {
	out := make([]string, 0, len(c.Modes)+1)
	for _, m := range c.Modes {
		out = append(out, m.Name)
	}
	return append(out, ModeOff)
}

// Label is the button text for a mode name.
func (c Caps) Label(mode string) string {
	for _, m := range c.Modes {
		if m.Name == mode && m.Label != "" {
			return m.Label
		}
	}
	return titleCase(mode)
}

// ControlsFor returns the controls a mode needs, from what the device says
// that mode takes.
//
// An unrecognised mode shows everything. A newer daemon may know modes this build
// does not, and revealing all the controls lets the user still operate them;
// hiding them would make the mode look broken.
func (c Caps) ControlsFor(mode string) Controls {
	if mode == ModeOff {
		return Controls{}
	}
	for _, m := range c.Modes {
		if m.Name == mode {
			return Controls{Color1: m.Color, Color2: m.Color2, Speed: m.Speed, Brightness: true}
		}
	}
	return Controls{Color1: true, Color2: true, Speed: true, Brightness: true}
}

// knownMode reports whether mode is one the device offers (or the drawer's off).
func (c Caps) knownMode(mode string) bool {
	if mode == ModeOff {
		return true
	}
	for _, m := range c.Modes {
		if m.Name == mode {
			return true
		}
	}
	return false
}

// BrightnessName is the window's label for a level: the device's own name for
// it where it names its levels (Off/Low/Medium/High on the Z13's Aura scale),
// and the number otherwise — a name the device did not give would be invented.
// Level 0 is always the hardware's off.
func (c Caps) BrightnessName(level int) string {
	if len(c.BrightnessNames) == c.BrightnessMax+1 && c.BrightnessMax > 0 {
		return titleCase(c.BrightnessNames[min(max(level, 0), c.BrightnessMax)])
	}
	if level == 0 {
		return "Off"
	}
	return strconv.Itoa(level)
}

// ResolveMode returns the mode button the drawer should select for a lighting
// state.
//
// Disabled lighting selects ModeOff regardless of any mode the daemon still has
// recorded: showing "breathe" as active while the keyboard is dark would be a lie.
//
// An enabled state with no mode falls back to the default rather than selecting
// nothing: the daemon can legitimately store a partial per-zone entry, which is
// what made zone lighting come back blank after a reboot.
func ResolveMode(ls api.LightingState) string {
	if !ls.Enabled {
		return ModeOff
	}
	if ls.Mode == "" {
		return DefaultMode
	}
	return ls.Mode
}

// ResolveSpeed returns the speed to select, falling back when unset.
func ResolveSpeed(ls api.LightingState) string {
	if ls.Speed == "" {
		return DefaultSpeed
	}
	return ls.Speed
}

// ResolveBrightness returns the brightness the slider should show.
//
// A disabled state shows the level it was turned off at, so picking an effect
// brings the zone back as bright as it was: an off keeps the zone's brightness
// (the per-zone off since z13ctl 1.4.3 and voltaire 2.0, the all-device off
// always). A stored 0 is never shown for a disabled state, though — it falls
// back to the default. A z13ctl 1.4.2-or-older per-zone off replaced the whole
// entry with LightingState{Enabled: false}, every other field zeroed, and
// `brightness 0` stores a disabled 0 on every version; 0 is the hardware's
// "off" level, not merely a dim one, so it is never a level to come back at.
// The caller clamps to the device's maximum.
//
// Without the fallback, turning a zone off and then back on left the keyboard
// dark: the slider adopted the zero, the next apply sent brightness 0, and the
// daemon dutifully set the backlight to off while reporting success. The mode
// button lit up and nothing else happened.
//
// A zero brightness on an *enabled* state is passed through, since that is a
// setting the user can deliberately choose with the slider, and second-guessing it
// would misreport the hardware. This is the same partial-state problem ResolveMode
// and ResolveSpeed already guard against; brightness was simply missed.
func ResolveBrightness(ls api.LightingState) int {
	if !ls.Enabled {
		if ls.Brightness > 0 {
			return ls.Brightness
		}
		return DefaultBrightness
	}
	return ls.Brightness
}

// StateForZone picks the lighting state to display for a zone, preferring the
// per-device entry and falling back to the global one.
//
// Returns the zero state when nothing is available, which ResolveMode reads as
// disabled — the correct thing to show when the daemon has told us nothing.
func StateForZone(s *api.State, zone string) api.LightingState {
	if s == nil {
		return api.LightingState{}
	}
	if dev, ok := s.Devices[zone]; ok {
		return dev
	}
	return s.Lighting
}
