// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// tdpslider.go — what the plain TDP control of a slider device shows
// (limits.Limits.Slider, the OneXConsole model): its value and the one line
// beneath it that says what moving it will do.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
)

// TDPSliderValue is the sustained limit the slider shows: get-state's tdp. With
// no custom profile running that is the limiter's own account of the hardware
// — the remembered value as this power source holds it, or the firmware's
// preset before one was set — and with one running it is that profile's.
// False before the daemon has reported any.
func TDPSliderValue(s *api.State) (int, bool) {
	if s == nil || s.TDP == nil || s.TDP.PL1SPL <= 0 {
		return 0, false
	}
	return s.TDP.PL1SPL, true
}

// TDPSliderNote is the line under the slider, or "" when the slider says
// everything itself. Each case is something the position alone would leave
// the user to guess:
//
//   - a custom profile is running, and a move leaves it for the TDP setting
//     (Jeff, 2026-11-17: the slider never edits a profile; changing one is
//     something the user does on purpose, on the Profiles page);
//   - nothing has been set, so the number is the firmware's preset, which
//     voltaire is not holding and the firmware changes on a charger edge;
//   - the value set is above this source's cap, so the slider reads lower
//     than what was set and will read it again on AC.
func TDPSliderNote(s *api.State) string {
	if s == nil {
		return ""
	}
	if s.InCustomProfile() {
		return fmt.Sprintf("Set by the %s profile. Moving the slider leaves it", Label(s.Profile))
	}
	if s.DeviceTDP == nil {
		return "The firmware's preset. Moving the slider sets your own"
	}
	shown, ok := TDPSliderValue(s)
	if ok && s.SourceKnown && !s.OnAC && s.DeviceTDP.PL1SPL > shown {
		return fmt.Sprintf("Set to %d W; %d W is the most on battery", s.DeviceTDP.PL1SPL, shown)
	}
	return ""
}
