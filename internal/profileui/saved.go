// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// saved.go — the Saved profile control, which replaces the firmware profile
// buttons on a device with a pinned profile (api.ProfileInfo.Pin; the OXP).
//
// There the firmware profile is not a choice, so a row of performance-profile
// buttons was a control that did not mean anything (Jeff, 2026-11-17). What a
// user does choose is whether a saved profile is loaded: one dropdown whose
// first row, "No profile", goes back to the TDP setting, and whose other rows
// load what each profile saved.

import "github.com/dahui/voltaire/api/v2"

// SavedRows returns the Saved profile control's rows: "No profile" (the
// pinned firmware profile) first, then the custom profiles as the drawer's
// picker lists them.
func SavedRows(s *api.State, pin string) []PickerRow {
	custom := s != nil && s.InCustomProfile()
	rows := []PickerRow{{Name: pin, Label: PinnedLabel, Active: !custom}}
	return append(rows, PickerRows(s)...)
}

// SavedLabel is the control's trigger text: the running profile, or "No
// profile".
func SavedLabel(s *api.State) string {
	if s != nil && s.InCustomProfile() {
		return Label(s.Profile)
	}
	return PinnedLabel
}

// NoSavedProfilesNote replaces the control when no profile has anything to
// load. It says where profiles come from, since the drawer has no editor:
// gesture is buttonpref's name for the press that opens the full window.
func NoSavedProfilesNote(gesture string) string {
	return "No saved profiles yet. " + gesture + " to save the TDP as one."
}
