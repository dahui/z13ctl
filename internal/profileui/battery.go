// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// battery.go — the battery card's headline: what the pack is doing, with the
// live flow beside it only where the number would not mislead.

import (
	"fmt"
	"math"

	"github.com/dahui/voltaire/api/v2"
)

// The battery_state vocabulary get-state serves, matched as wire strings the
// way any other client would. internal/driver holds the same values, but the
// GUI shares only the api contract with the daemon, and importing the driver
// tier for four strings would couple the drawer to daemon internals.
const (
	batteryCharging    = "charging"
	batteryDischarging = "discharging"
	batteryFull        = "full"
	batteryNotCharging = "not-charging"
)

// BatteryStatus returns the battery card's header readout: the pack's state
// word with the live flow beside it ("Charging · 24.5 W", "Discharging ·
// 12.3 W"), or "" when nothing can honestly be claimed — the caller renders a
// placeholder there.
//
// "not-charging" and "full" deliberately never show the number: a pack held
// below its charge limit on mains reads exactly 0 W, which is correct and
// indistinguishable from a dead sensor — the state word is the entire
// explanation for the zero, and printing "0 W" beside it would reintroduce
// the misreading battery_state exists to prevent. The flow's sign convention
// is positive = discharging; the word carries the direction here, so only the
// magnitude is shown.
func BatteryStatus(s *api.State) string {
	if s == nil {
		return ""
	}
	watts := ""
	if s.BatteryPowerW != nil {
		watts = fmt.Sprintf("%.1f W", math.Abs(*s.BatteryPowerW))
	}
	switch s.BatteryState {
	case batteryCharging:
		return joinStatus("Charging", watts)
	case batteryDischarging:
		return joinStatus("Discharging", watts)
	case batteryNotCharging:
		if PowerLabel(s) == "AC" {
			return "AC · not charging"
		}
		return "Not charging"
	case batteryFull:
		if PowerLabel(s) == "AC" {
			return "AC · full"
		}
		return "Full"
	}
	// No state word — an older daemon, or a pack the driver cannot read. The
	// power-source word can still give the flow its context; without either
	// word, a bare number is exactly the broken-sensor reading above, so the
	// card claims nothing and the chart alone carries the flow.
	if label := PowerLabel(s); label != "" {
		return joinStatus(label, watts)
	}
	return ""
}

// joinStatus joins the state word and the flow readout, dropping the
// separator when there is no number to show.
func joinStatus(word, watts string) string {
	if watts == "" {
		return word
	}
	return word + " · " + watts
}
