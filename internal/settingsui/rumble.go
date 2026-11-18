// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package settingsui

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
)

// RumbleRange is the CONTROLLER card's vibration-strength scale, and whether
// the card is built at all: only when the document offers the control, since
// its range is the kernel's and nothing else can bound the slider. A nil
// document builds nothing, unlike the FIRMWARE card — there is no empty state
// worth explaining for a control most machines do not have.
func RumbleRange(doc *api.DeviceInfo) (lo, hi int, ok bool) {
	if doc == nil || doc.Rumble == nil || doc.Rumble.Max <= doc.Rumble.Min {
		return 0, 0, false
	}
	return doc.Rumble.Min, doc.Rumble.Max, true
}

// RumbleText is the slider's readout: "Off" at 0, else the step on its scale.
func RumbleText(v, hi int) string {
	if v <= 0 {
		return "Off"
	}
	return fmt.Sprintf("%d of %d", v, hi)
}

// RumbleValue is the strength to show from get-state, and whether it is known.
// Absent means it could not be read, which the slider shows as insensitive,
// never as a value.
func RumbleValue(st *api.State) (int, bool) {
	if st == nil || st.Rumble == nil {
		return 0, false
	}
	return *st.Rumble, true
}
