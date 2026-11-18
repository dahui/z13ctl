// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// undervoltslider.go — what the plain undervolt control of a slider device
// shows (the daemon's State.DeviceUndervolt): its value and the one line
// beneath it that says what moving it will do. The counterpart of the TDP
// slider's rules in tdpslider.go.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
)

// UndervoltSliderValue is the offset the slider shows: the running custom
// profile's own, when it carries one, else the undervolt setting (0 when none
// is set). False when the daemon has not answered or says the Curve Optimizer
// is unavailable, so the slider is not offered as a live control.
func UndervoltSliderValue(s *api.State) (int, bool) {
	if s == nil || !s.UndervoltAvailable {
		return 0, false
	}
	if p, ok := s.ActiveCustomProfile(); ok && p.Undervolt != nil {
		return p.Undervolt.CPUCO, true
	}
	if s.DeviceUndervolt != nil {
		return s.DeviceUndervolt.CPUCO, true
	}
	return 0, true
}

// UndervoltSliderNote is the line under the slider, or "" when the slider
// says everything itself:
//
//   - the Curve Optimizer is unavailable, which is why the slider is dead;
//   - a custom profile is running, and a move leaves it for the setting, as
//     the TDP slider's does (the profile's own offset, when it has one, is
//     what the slider shows meanwhile).
func UndervoltSliderNote(s *api.State) string {
	if s == nil {
		return ""
	}
	if !s.UndervoltAvailable {
		return "Needs the ryzen_smu kernel module (amkillam's fork for this CPU)"
	}
	if s.InCustomProfile() {
		if p, ok := s.ActiveCustomProfile(); ok && p.Undervolt != nil {
			return fmt.Sprintf("Set by the %s profile. Moving the slider leaves it", Label(s.Profile))
		}
		return fmt.Sprintf("Moving the slider leaves the %s profile", Label(s.Profile))
	}
	return ""
}

// UndervoltNoteOn is UndervoltNote for the Profiles editor of a device that
// may be on the one-slider model, where an offset is not tied to custom
// profiles: the undervolt setting runs whenever the running profile has no
// offset of its own.
func UndervoltNoteOn(slider bool) string {
	if !slider {
		return UndervoltNote()
	}
	return "A profile's offset applies while that profile runs; otherwise the " +
		"undervolt setting on the Dashboard does. Unstable values may cause crashes."
}
