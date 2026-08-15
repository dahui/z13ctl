// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

func wattsOf(v float64) *float64 { return &v }

func TestBatteryStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    *api.State
		want string
	}{
		{
			// The full header: level, rate, and the estimate. The estimate's
			// target says which way the energy is going, so the state word is
			// dropped — "Discharging" plus "to empty" says it twice.
			name: "discharging with the energy pair estimates to empty",
			s: &api.State{
				BatteryState: "discharging", BatteryPowerW: wattsOf(20),
				BatteryLevel: 81, BatteryEnergyWh: 50, BatteryEnergyFullWh: 62,
			},
			want: "81% · 20.0 W · 2 h 30 m to empty",
		},
		{
			// A whole-hour answer drops the minutes.
			name: "a whole-hour estimate is bare",
			s: &api.State{
				BatteryState: "discharging", BatteryPowerW: wattsOf(12),
				BatteryLevel: 60, BatteryEnergyWh: 24, BatteryEnergyFullWh: 62,
			},
			want: "60% · 12.0 W · 2 h to empty",
		},
		{
			// Charging with a limit set: the pack will genuinely stop at the
			// threshold, so the estimate targets it — "to full" would promise
			// something the firmware is going to prevent. The sign convention
			// is positive = discharging, so a charging flow arrives negative;
			// only the magnitude is printed.
			name: "charging with a limit estimates to the limit",
			s: &api.State{
				BatteryState: "charging", BatteryPowerW: wattsOf(-28),
				BatteryLevel: 64, Battery: 80,
				BatteryEnergyWh: 42, BatteryEnergyFullWh: 70,
			},
			want: "64% · 28.0 W · 30 m to limit",
		},
		{
			name: "charging with no limit estimates to full",
			s: &api.State{
				BatteryState: "charging", BatteryPowerW: wattsOf(-14),
				BatteryLevel: 90, BatteryEnergyWh: 63, BatteryEnergyFullWh: 70,
			},
			want: "90% · 14.0 W · 30 m to full",
		},
		{
			// A limit of 100 is not a hold — the pack charges to full.
			name: "a 100 percent limit reads as no limit",
			s: &api.State{
				BatteryState: "charging", BatteryPowerW: wattsOf(-14),
				BatteryLevel: 90, Battery: 100,
				BatteryEnergyWh: 63, BatteryEnergyFullWh: 70,
			},
			want: "90% · 14.0 W · 30 m to full",
		},
		{
			// A resting pack wobbles by tenths of a watt; an estimate built on
			// that claims precision the reading does not have, so the state
			// word returns instead.
			name: "a tiny flow suppresses the estimate",
			s: &api.State{
				BatteryState: "discharging", BatteryPowerW: wattsOf(0.3),
				BatteryLevel: 81, BatteryEnergyWh: 50, BatteryEnergyFullWh: 62,
			},
			want: "81% · Discharging · 0.3 W",
		},
		{
			// Beyond a day the number informs nothing and is usually the
			// low-rate wobble the floor did not catch.
			name: "an estimate beyond a day is suppressed",
			s: &api.State{
				BatteryState: "discharging", BatteryPowerW: wattsOf(2),
				BatteryLevel: 95, BatteryEnergyWh: 60, BatteryEnergyFullWh: 62,
			},
			want: "95% · Discharging · 2.0 W",
		},
		{
			// Already at or above the charge target — nothing to estimate.
			name: "charging above the target falls back to the word",
			s: &api.State{
				BatteryState: "charging", BatteryPowerW: wattsOf(-14),
				BatteryLevel: 85, Battery: 80,
				BatteryEnergyWh: 60, BatteryEnergyFullWh: 70,
			},
			want: "85% · Charging · 14.0 W",
		},
		{
			// A pre-2.0 daemon, or a pack with no energy pair: the level and
			// rate still show, with the word carrying the direction.
			name: "charging without the energy pair shows word and magnitude",
			s:    &api.State{BatteryState: "charging", BatteryPowerW: wattsOf(-24.5)},
			want: "Charging · 24.5 W",
		},
		{
			name: "discharging without the energy pair shows the flow",
			s:    &api.State{BatteryState: "discharging", BatteryPowerW: wattsOf(12.3)},
			want: "Discharging · 12.3 W",
		},
		{
			// The charge-limited machine at rest: the commonest state on this
			// hardware, and the reason the function exists. 0 W is correct
			// here and must not be printed — it reads as a dead sensor.
			name: "not charging at the limit hides the zero",
			s: &api.State{
				BatteryState: "not-charging", BatteryPowerW: wattsOf(0),
				BatteryLevel: 81, SourceKnown: true, OnAC: true,
			},
			want: "81% · AC · not charging",
		},
		{
			// Even a non-zero flow stays hidden behind not-charging: the word
			// is the claim, and a number beside it would contradict it.
			name: "not charging never shows a number",
			s: &api.State{
				BatteryState: "not-charging", BatteryPowerW: wattsOf(1.2),
				SourceKnown: true, OnAC: true,
			},
			want: "AC · not charging",
		},
		{
			name: "not charging with the source unknown",
			s:    &api.State{BatteryState: "not-charging"},
			want: "Not charging",
		},
		{
			name: "full on mains",
			s:    &api.State{BatteryState: "full", BatteryLevel: 100, SourceKnown: true, OnAC: true},
			want: "100% · AC · full",
		},
		{
			name: "full with the source unknown",
			s:    &api.State{BatteryState: "full"},
			want: "Full",
		},
		{
			// A pre-2.0 daemon omits battery_state; the power-source word
			// still gives the flow its context.
			name: "no state word falls back to the source label",
			s:    &api.State{BatteryPowerW: wattsOf(8.1), SourceKnown: true, OnAC: true},
			want: "AC · 8.1 W",
		},
		{
			name: "no state word on battery",
			s:    &api.State{BatteryPowerW: wattsOf(12), SourceKnown: true, OnAC: false},
			want: "Battery · 12.0 W",
		},
		{
			// A number with neither a state word nor a source word is exactly
			// the broken-sensor reading the state exists to prevent — claim
			// nothing and let the chart carry the level.
			name: "a bare number claims nothing",
			s:    &api.State{BatteryPowerW: wattsOf(8.1)},
			want: "",
		},
		{
			// The level alone is still a true claim.
			name: "a level with nothing else stands alone",
			s:    &api.State{BatteryLevel: 81},
			want: "81%",
		},
		{
			name: "a state word without a reading stands alone",
			s:    &api.State{BatteryState: "charging"},
			want: "Charging",
		},
		{
			name: "nothing known",
			s:    &api.State{},
			want: "",
		},
		{
			name: "nil state",
			s:    nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := profileui.BatteryStatus(tc.s); got != tc.want {
				t.Fatalf("BatteryStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChargerLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		st   *api.State
		want string
	}{
		{"no state", nil, ""},
		// A device with one power input says nothing, which is most of them.
		{"device cannot say", &api.State{}, ""},
		// Both observed on hardware, charger physically swapped.
		{"proprietary adapter", &api.State{Charger: "adapter"}, "Adapter"},
		{"usb-c pd", &api.State{Charger: "usb-c"}, "USB-C"},
		// "no charger attached" is a real reading, but PowerLabel already says
		// the machine is on battery and naming the absent charger adds nothing.
		{"nothing attached", &api.State{Charger: "none"}, ""},
		// Firmware may grow a kind before voltaire does.
		{"kind this build does not know", &api.State{Charger: "wireless"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := profileui.ChargerLabel(tc.st); got != tc.want {
				t.Errorf("ChargerLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
