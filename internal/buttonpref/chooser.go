// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package buttonpref

// chooser.go — which hardware button opens voltaire: the "Open voltaire with"
// row's choices and the button a surface should name.
//
// The choices are the device file's buttons plus "no button", the G-Helper
// model: a fixed list a user picks from, never a key learned from a keyboard.
// The daemon stores the choice (api.SendButtonSet) and reports it resolved as
// State.DrawerButton, so nothing here applies the device default itself
// except for a daemon too old to report one.

import "github.com/dahui/voltaire/api/v2"

// Choice is one row of the "Open voltaire with" list.
type Choice struct {
	ID    string // a button id, or api.DrawerButtonNone
	Label string
}

// The chooser row's own text.
const (
	ChooserRowLabel    = "Open voltaire with"
	ChooserDescription = "One of this machine's buttons, or none. voltaire never reads a keyboard, " +
		"so these are the only keys it can listen for; with no button, the desktop entry opens it."
	NoButtonLabel = "No button"
)

// ShowChooser reports whether there is a choice to offer: the daemon listed
// the device's buttons. A nil document (no daemon) or an older daemon that
// predates the list offers none, since a choice could not be sent anyway.
func ShowChooser(info *api.DeviceInfo) bool {
	return info != nil && len(info.HardwareButtons) > 0
}

// Choices is the list to offer, in the device's order, with "no button" last.
func Choices(info *api.DeviceInfo) []Choice {
	if !ShowChooser(info) {
		return nil
	}
	out := make([]Choice, 0, len(info.HardwareButtons)+1)
	for _, b := range info.HardwareButtons {
		out = append(out, Choice{ID: b.ID, Label: Capitalize(b.Label)})
	}
	return append(out, Choice{ID: api.DrawerButtonNone, Label: NoButtonLabel})
}

// Drawer is the button that opens voltaire, by id and mid-sentence label:
// the daemon's resolved answer, else (an older daemon, or no state yet) the
// device's default. ok is false when no button opens it — the user chose
// none, or the device declares none it can name.
func Drawer(info *api.DeviceInfo, st *api.State) (id, label string, ok bool) {
	if st != nil && st.DrawerButton == api.DrawerButtonNone {
		return "", "", false
	}
	if info == nil {
		return "", "", true // no document: assume the button the build always had
	}
	if !info.Buttons {
		return "", "", false
	}
	want := ""
	if st != nil {
		want = st.DrawerButton
	}
	for _, b := range info.HardwareButtons {
		if b.ID == want || (want == "" && b.Default) {
			return b.ID, b.Label, true
		}
	}
	// An older daemon: no list, so the one label it serves is the button.
	return "", info.ButtonLabel, true
}

// ChooserText is the chooser's collapsed text: the selected button's name,
// or NoButtonLabel.
func ChooserText(info *api.DeviceInfo, st *api.State) string {
	if _, label, ok := Drawer(info, st); ok {
		return Capitalize(Name(label))
	}
	return NoButtonLabel
}

// Capitalize upper-cases the first letter of a mid-sentence label for use as
// a heading or list row.
func Capitalize(s string) string {
	if s == "" {
		return s
	}
	return RowLabel(s)
}
