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
			// The sign convention is positive = discharging, so a charging
			// flow arrives negative; the word carries the direction and only
			// the magnitude is printed.
			name: "charging shows the magnitude",
			s:    &api.State{BatteryState: "charging", BatteryPowerW: wattsOf(-24.5)},
			want: "Charging · 24.5 W",
		},
		{
			name: "discharging shows the flow",
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
				SourceKnown: true, OnAC: true,
			},
			want: "AC · not charging",
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
			s:    &api.State{BatteryState: "full", SourceKnown: true, OnAC: true},
			want: "AC · full",
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
			// nothing and let the chart carry the flow.
			name: "a bare number claims nothing",
			s:    &api.State{BatteryPowerW: wattsOf(8.1)},
			want: "",
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
