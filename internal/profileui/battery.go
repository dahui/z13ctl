// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// battery.go — the battery card's headline: the state of charge, what the pack
// is doing, and — while energy is actually moving — the rate and a time
// estimate. The chart below it plots the charge percentage, so the header is
// where the flow figure lives now.

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

// minEstimateW is the smallest flow a time estimate may be built on. Below it
// the arithmetic still divides, but the answer is noise: a pack resting near
// its limit wobbles by tenths of a watt, and "138 h to empty" from a 0.3 W
// blip claims a precision the reading does not have.
const minEstimateW = 0.5

// maxEstimateMin caps the estimate at a day. Beyond that the number stops
// informing any decision the user could take from it, and very long answers
// are usually the low-rate wobble the floor above did not quite catch.
const maxEstimateMin = 24 * 60

// BatteryStatus returns the battery card's header readout: the charge
// percentage, then what the pack is doing. While charging or discharging that
// is the rate with a time estimate ("64% · 24.5 W · 41 m to limit",
// "81% · 12.3 W · 3 h 5 m to empty") — the estimate's target carries the
// direction, so the state word is dropped to keep the line legible; when no
// honest estimate exists the word returns ("81% · Discharging · 12.3 W").
// Returns "" when nothing can honestly be claimed — the caller renders a
// placeholder there.
//
// "not-charging" and "full" deliberately never show a number: a pack held
// below its charge limit on mains reads exactly 0 W, which is correct and
// indistinguishable from a dead sensor — the state word is the entire
// explanation for the zero, and printing "0 W" beside it would reintroduce
// the misreading battery_state exists to prevent. The flow's sign convention
// is positive = discharging; the direction is carried in words here, so only
// the magnitude is shown.
func BatteryStatus(s *api.State) string {
	if s == nil {
		return ""
	}
	// The api documents zero as "no battery", so a bare zero level claims
	// nothing — and a machine genuinely at 0% is not running this code.
	pct := ""
	if s.BatteryLevel > 0 {
		pct = fmt.Sprintf("%d%%", s.BatteryLevel)
	}
	watts := ""
	if s.BatteryPowerW != nil {
		watts = fmt.Sprintf("%.1f W", math.Abs(*s.BatteryPowerW))
	}
	switch s.BatteryState {
	case batteryCharging, batteryDischarging:
		if est := batteryEstimate(s); est != "" {
			return joinStatus(pct, watts, est)
		}
		word := "Charging"
		if s.BatteryState == batteryDischarging {
			word = "Discharging"
		}
		return joinStatus(pct, word, watts)
	case batteryNotCharging:
		if PowerLabel(s) == "AC" {
			return joinStatus(pct, "AC · not charging")
		}
		return joinStatus(pct, "Not charging")
	case batteryFull:
		if PowerLabel(s) == "AC" {
			return joinStatus(pct, "AC · full")
		}
		return joinStatus(pct, "Full")
	}
	// No state word — an older daemon, or a pack the driver cannot read. The
	// power-source word can still give the flow its context; without either
	// word, a bare number is exactly the broken-sensor reading above, so the
	// card claims nothing beyond the level and the chart carries the rest.
	if label := PowerLabel(s); label != "" {
		return joinStatus(pct, label, watts)
	}
	return pct
}

// batteryEstimate turns the flow and the energy pair into "3 h 5 m to empty",
// "41 m to limit" or "41 m to full", or "" when no honest estimate exists: the
// flow is absent or below minEstimateW, the pack reported no energy pair, the
// target is already met, or the answer exceeds maxEstimateMin.
//
// While charging, the target is the charge limit when one is set (the pack
// will genuinely stop there — "to full" would promise something the firmware
// is going to prevent) and the full charge otherwise. The estimate is
// instantaneous — remaining energy over the current rate — so it moves with
// the load exactly as every OS battery indicator does.
func batteryEstimate(s *api.State) string {
	if s.BatteryPowerW == nil {
		return ""
	}
	watts := math.Abs(*s.BatteryPowerW)
	if watts < minEstimateW {
		return ""
	}

	var remainWh float64
	var target string
	switch s.BatteryState {
	case batteryDischarging:
		remainWh, target = s.BatteryEnergyWh, "empty"
	case batteryCharging:
		if s.BatteryEnergyFullWh <= 0 {
			return ""
		}
		targetWh := s.BatteryEnergyFullWh
		target = "full"
		if s.Battery >= 1 && s.Battery <= 99 {
			targetWh = s.BatteryEnergyFullWh * float64(s.Battery) / 100
			target = "limit"
		}
		remainWh = targetWh - s.BatteryEnergyWh
	default:
		return ""
	}
	if remainWh <= 0 {
		return ""
	}

	mins := int(math.Round(remainWh / watts * 60))
	if mins < 1 || mins > maxEstimateMin {
		return ""
	}
	return formatMinutes(mins) + " to " + target
}

// formatMinutes renders a duration the way a battery indicator says it:
// minutes alone under an hour, whole hours bare, "3 h 5 m" otherwise.
func formatMinutes(m int) string {
	switch {
	case m < 60:
		return fmt.Sprintf("%d m", m)
	case m%60 == 0:
		return fmt.Sprintf("%d h", m/60)
	default:
		return fmt.Sprintf("%d h %d m", m/60, m%60)
	}
}

// joinStatus joins the header's pieces with " · ", skipping the ones that have
// nothing to say.
func joinStatus(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " · "
		}
		out += p
	}
	return out
}

// ChargerLabel names which power input is supplying the machine, or "" when
// the device cannot say or nothing is attached.
//
// Separate from BatteryStatus rather than folded into it, because that line is
// already at its limit — it drops the state word when an estimate is present
// specifically to stay legible, so a fourth element would undo the rule its own
// doc comment states. The caller renders this alongside.
//
// Empty on battery: "which charger" has no answer when there is no charger, and
// PowerLabel already says the machine is on battery. Empty for an unrecognised
// kind too — naming it wrongly is worse than saying nothing.
func ChargerLabel(s *api.State) string {
	if s == nil {
		return ""
	}
	switch s.Charger {
	case "adapter":
		return "Adapter"
	case "usb-c":
		return "USB-C"
	}
	return ""
}
