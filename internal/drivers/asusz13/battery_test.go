// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package asusz13

// battery_test.go — state of health against the fake sysfs tree.
//
// The unit question is the whole reason this is not a one-liner: power_supply
// publishes either energy_full/energy_full_design in µWh or
// charge_full/charge_full_design in µAh depending on the battery driver, and
// this machine's ACPI battery reports energy where the driver interface's
// first draft assumed charge. Only the ratio is portable, so the ratio is what
// crosses the driver boundary.

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

func TestReadBatteryHealthPercent(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  int
		fails bool
	}{
		{
			// The measured values from the development machine.
			name:  "energy pair (µWh) — what the Z13 publishes",
			files: map[string]string{"energy_full": "64092000", "energy_full_design": "70003000"},
			want:  92, // 91.56 rounds up
		},
		{
			name:  "charge pair (µAh) — what many other supplies publish",
			files: map[string]string{"charge_full": "5000000", "charge_full_design": "6000000"},
			want:  83,
		},
		{
			// A pack fresh off calibration genuinely reads over its design
			// capacity. Reporting 103 beats a number quietly adjusted to look
			// plausible, so nothing clamps it.
			name:  "above design capacity is not clamped",
			files: map[string]string{"energy_full": "72000000", "energy_full_design": "70000000"},
			want:  103,
		},
		{
			// A supply that publishes the attribute but zeroes it must not
			// produce a division by zero or a nonsense percentage — fall
			// through to the other pair.
			name: "zero design capacity falls through to the other pair",
			files: map[string]string{
				"energy_full": "64092000", "energy_full_design": "0",
				"charge_full": "5000000", "charge_full_design": "6000000",
			},
			want: 83,
		},
		{
			name:  "half a pair is not a pair",
			files: map[string]string{"energy_full": "64092000"},
			fails: true,
		},
		{
			name:  "no full-charge attributes at all",
			files: map[string]string{},
			fails: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSysfs(t)
			for name, v := range tc.files {
				f.writeFile(t, f.battery+"/"+name, v)
			}

			got, err := ReadBatteryHealthPercent()
			switch {
			case tc.fails && err == nil:
				t.Fatalf("ReadBatteryHealthPercent() = %d, want an error", got)
			case tc.fails:
				return
			case err != nil:
				t.Fatalf("ReadBatteryHealthPercent() = %v", err)
			case got != tc.want:
				t.Errorf("ReadBatteryHealthPercent() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBatteryStatusReportsHealthOnlyWhenDeclared(t *testing.T) {
	// Health is a declared capability, not a probe: a device whose data does
	// not claim it must report zero even on a machine where the attributes
	// happen to be readable, or the document and the reading disagree.
	f := newFakeSysfs(t)
	f.writeFile(t, f.battery+"/capacity", "80")
	f.writeFile(t, f.battery+"/energy_full", "64092000")
	f.writeFile(t, f.battery+"/energy_full_design", "70003000")

	for _, tc := range []struct {
		caps driver.BatteryCaps
		want int
	}{
		{driver.BatteryCaps{ChargeLimit: true, Health: true}, 92},
		{driver.BatteryCaps{ChargeLimit: true}, 0},
	} {
		b := NewBattery(tc.caps)
		if got := b.Caps(); got != tc.caps {
			t.Errorf("Caps() = %+v, want %+v", got, tc.caps)
		}
		st, err := b.Status()
		if err != nil {
			t.Fatalf("Status() = %v", err)
		}
		if st.Capacity != 80 {
			t.Errorf("Capacity = %d, want 80", st.Capacity)
		}
		if st.HealthPercent != tc.want {
			t.Errorf("health with caps %+v = %d, want %d", tc.caps, st.HealthPercent, tc.want)
		}
	}
}

func TestBatteryStatusSurvivesAnUnreadableHealthPair(t *testing.T) {
	// Best-effort, for the same reason RPM is in Sample: a pack whose
	// full-charge attributes are missing must not make the charge level
	// unreadable. Zero is the documented "not reported".
	f := newFakeSysfs(t)
	f.writeFile(t, f.battery+"/capacity", "55")

	st, err := NewBattery(driver.BatteryCaps{Health: true}).Status()
	if err != nil {
		t.Fatalf("Status() = %v", err)
	}
	if st.Capacity != 55 || st.HealthPercent != 0 {
		t.Errorf("Status() = %+v, want capacity 55 and no health", st)
	}
}
