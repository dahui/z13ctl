// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package asusz13

import (
	"os"
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// TestFindRAPLPackagePathPicksThePackageDomain is the double-counting guard.
// The powercap tree exposes sub-domains (core, uncore, dram) whose energy is
// part of the package's, and they sit next to it under names that differ only
// by suffix. Matching by directory name — intel-rapl:0 — would work here and
// break the moment the numbering changed, which is the same reason fan hwmon
// discovery reads `name`.
func TestFindRAPLPackagePathPicksThePackageDomain(t *testing.T) {
	f := newFakeSysfs(t)

	got, err := FindRAPLPackagePath()
	if err != nil {
		t.Fatalf("FindRAPLPackagePath() = %v", err)
	}
	if got != f.powercap {
		t.Errorf("FindRAPLPackagePath() = %q, want the package-0 domain %q", got, f.powercap)
	}
}

func TestReadPackageEnergy(t *testing.T) {
	f := newFakeSysfs(t)

	uj, maxUJ, err := ReadPackageEnergy()
	if err != nil {
		t.Fatalf("ReadPackageEnergy() = %v", err)
	}
	if uj != 1_000_000 {
		t.Errorf("energy = %d, want 1000000", uj)
	}
	if maxUJ != 262_143_328_850 {
		t.Errorf("max = %d, want 262143328850", maxUJ)
	}

	// A missing range is not fatal: it only distinguishes a wrap from a reset,
	// and zero is the documented "unknown".
	if rmErr := os.Remove(f.powercap + "/max_energy_range_uj"); rmErr != nil {
		t.Fatal(rmErr)
	}
	if _, maxUJ, err = ReadPackageEnergy(); err != nil {
		t.Fatalf("ReadPackageEnergy() without a range = %v, want success", err)
	}
	if maxUJ != 0 {
		t.Errorf("max = %d with no range file, want 0", maxUJ)
	}
}

// TestReadPackageEnergyWithoutPowercap is the un-granted case: energy_uj is
// 0400 root:root until voltaire setup runs, and an unreadable counter must be
// an error the caller can skip, never a zero it would plot as 0 W.
func TestReadPackageEnergyWithoutPowercap(t *testing.T) {
	f := newFakeSysfs(t)
	if err := os.Remove(f.powercap + "/energy_uj"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadPackageEnergy(); err == nil {
		t.Error("ReadPackageEnergy() = nil error with no readable counter")
	}
}

// TestReadBatteryPowerW covers the shape this machine actually has — power_now
// in microwatts — and the sign, which sysfs does not carry.
func TestReadBatteryPowerW(t *testing.T) {
	f := newFakeSysfs(t)

	cases := []struct {
		status string
		want   float64
	}{
		{"Discharging", 12.5}, // drawing from the pack
		{"Charging", -12.5},   // going in
		{"Full", 0},           // no flow, whatever the magnitude says
		{"Not charging", 0},   // the Z13's own state on mains at 100%
		{"Unknown", 0},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			f.writeFile(t, f.battery+"/status", tc.status)
			got, err := ReadBatteryPowerW()
			if err != nil {
				t.Fatalf("ReadBatteryPowerW() = %v", err)
			}
			if got != tc.want {
				t.Errorf("with status %q: got %v W, want %v W", tc.status, got, tc.want)
			}
		})
	}
}

// TestReadBatteryPowerFallsBackToCurrentTimesVoltage covers the *other*
// hardware shape. A charge-reporting pack has current_now and no power_now;
// the Z13 is the reverse, so a reader written against only one of them finds
// nothing on half the machines — the mistake ReadBatteryHealthPercent's first
// draft made with charge_full versus energy_full.
func TestReadBatteryPowerFallsBackToCurrentTimesVoltage(t *testing.T) {
	f := newFakeSysfs(t)
	if err := os.Remove(f.battery + "/power_now"); err != nil {
		t.Fatal(err)
	}
	f.writeFile(t, f.battery+"/current_now", "1000000")  // 1 A
	f.writeFile(t, f.battery+"/voltage_now", "16000000") // 16 V
	f.writeFile(t, f.battery+"/status", "Discharging")

	got, err := ReadBatteryPowerW()
	if err != nil {
		t.Fatalf("ReadBatteryPowerW() = %v", err)
	}
	if got != 16 {
		t.Errorf("got %v W, want 16 W from 1 A × 16 V", got)
	}
}

// TestReadBatteryEnergyWh covers both hardware shapes of the estimate's
// divisor: the Z13's energy pair in µWh, and the charge-form fallback that
// becomes watt-hours through voltage_now — the same portability split as the
// flow and health readers, for the same reason.
func TestReadBatteryEnergyWh(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.battery+"/energy_now", "45500000")  // 45.5 Wh
	f.writeFile(t, f.battery+"/energy_full", "64092000") // 64.092 Wh

	now, full, err := ReadBatteryEnergyWh()
	if err != nil {
		t.Fatalf("ReadBatteryEnergyWh() = %v", err)
	}
	if now != 45.5 || full != 64.092 {
		t.Errorf("= %v, %v Wh; want 45.5, 64.092", now, full)
	}
}

func TestReadBatteryEnergyFallsBackToChargeTimesVoltage(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.battery+"/charge_now", "2500000")   // 2.5 Ah
	f.writeFile(t, f.battery+"/charge_full", "5000000")  // 5 Ah
	f.writeFile(t, f.battery+"/voltage_now", "16000000") // 16 V

	now, full, err := ReadBatteryEnergyWh()
	if err != nil {
		t.Fatalf("ReadBatteryEnergyWh() = %v", err)
	}
	if now != 40 || full != 80 {
		t.Errorf("= %v, %v Wh; want 40, 80 from 2.5/5 Ah × 16 V", now, full)
	}
}

// TestReadBatteryEnergyRequiresThePair: a remaining figure without the pack
// size behind it answers only half the estimate, so half a pair is an error
// on both paths — never a zero standing in for the missing half.
func TestReadBatteryEnergyRequiresThePair(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.battery+"/energy_now", "45500000")
	if _, _, err := ReadBatteryEnergyWh(); err == nil {
		t.Error("ReadBatteryEnergyWh() = nil error with energy_now but no energy_full")
	}
}

// TestReadBatteryPowerWithNoReading: a pack reporting neither form is an
// error, not a zero. Zero is a real reading here (a full pack on mains moves
// no energy), so it cannot double as "no source".
func TestReadBatteryPowerWithNoReading(t *testing.T) {
	f := newFakeSysfs(t)
	for _, name := range []string{"power_now", "current_now", "voltage_now"} {
		_ = os.Remove(f.battery + "/" + name)
	}
	if _, err := ReadBatteryPowerW(); err == nil {
		t.Error("ReadBatteryPowerW() = nil error with nothing to read")
	}
}

// TestFindBatteryDirIgnoresTheDetachableKeyboard is the same decoy problem
// FindACOnlinePath has: the cover's HID pack is also type Battery and also
// reports a capacity, so a reader that took the first Battery-typed supply
// would report the keyboard's charge as the machine's.
func TestFindBatteryDirIgnoresTheDetachableKeyboard(t *testing.T) {
	f := newFakeSysfs(t)

	got, err := findBatteryDir()
	if err != nil {
		t.Fatalf("findBatteryDir() = %v", err)
	}
	if got != f.battery {
		t.Errorf("findBatteryDir() = %q, want the system pack %q", got, f.battery)
	}
}

// TestSampleReadsEveryQuantity is the integration check: one Sample call must
// fill temperature, both fans, the energy counter and battery flow from the
// same fake tree.
func TestSampleReadsEveryQuantity(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.hwmonTemp+"/temp1_input", "62500")
	f.writeFile(t, f.hwmonRead+"/fan1_input", "2400")
	f.writeFile(t, f.hwmonRead+"/fan2_input", "2600")

	s, err := telemetry{}.Sample()
	if err != nil {
		t.Fatalf("Sample() = %v", err)
	}
	if s.TempC == 0 {
		t.Error("Sample() reported no temperature")
	}
	if len(s.RPM) == 0 {
		t.Error("Sample() reported no fan speeds")
	}
	if s.PackageEnergyUJ == 0 {
		t.Error("Sample() reported no package energy counter")
	}
	if s.BatteryPowerW == 0 {
		t.Error("Sample() reported no battery flow")
	}
	if !s.BatteryLevelKnown || s.BatteryLevelPct != 81 {
		t.Errorf("BatteryLevelPct = %d (known %v), want 81 — the battery chart's series",
			s.BatteryLevelPct, s.BatteryLevelKnown)
	}
	// The counter is a counter: converting it needs the previous reading, so
	// the driver must not have guessed at a power figure.
	if s.PackagePowerW != 0 {
		t.Errorf("PackagePowerW = %v from a single sample; the conversion belongs to the caller",
			s.PackagePowerW)
	}
}

// TestSampleSurvivesAnUngrantedCounter: the powercap grant is the one this
// feature added, and a user who has not re-run voltaire setup has an
// unreadable energy_uj. That must cost the power series and nothing else — a
// failed sample is a gap in *every* series, including the temperature that was
// readable all along.
func TestSampleSurvivesAnUngrantedCounter(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.hwmonTemp+"/temp1_input", "62500")
	if err := os.Remove(f.powercap + "/energy_uj"); err != nil {
		t.Fatal(err)
	}

	s, err := telemetry{}.Sample()
	if err != nil {
		t.Fatalf("Sample() = %v, want success without the powercap grant", err)
	}
	if s.TempC == 0 {
		t.Error("an unreadable energy counter cost the temperature reading")
	}
	if s.PackageEnergyUJ != 0 {
		t.Errorf("PackageEnergyUJ = %d, want 0 when the counter is unreadable", s.PackageEnergyUJ)
	}
}

// TestBatteryStateMapping pins power_supply's `status` vocabulary, which is the
// one place that knows it — the sign convention in signedByStatus reads the same
// function, so the flow's direction and the state on the wire cannot drift.
func TestBatteryStateMapping(t *testing.T) {
	f := newFakeSysfs(t)

	cases := []struct {
		raw  string
		want driver.BatteryState
	}{
		{"Charging", driver.BatteryStateCharging},
		{"Discharging", driver.BatteryStateDischarging},
		{"Full", driver.BatteryStateFull},
		{"Not charging", driver.BatteryStateNotCharging},
		{"Unknown", driver.BatteryStateUnknown},
		{"something new", driver.BatteryStateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			f.writeFile(t, f.battery+"/status", tc.raw)
			if got := ReadBatteryState(); got != tc.want {
				t.Errorf("ReadBatteryState() with status %q = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNotChargingIsDistinctFromFull is the case that produced the bug report,
// and the reason the state is on the wire at all.
//
// A charge-end threshold below the current level leaves the pack resting: it
// cannot charge (it is over the limit) and it is not discharging (mains is
// attached), so power_now is 0 and that is the truth. Reported as a bare "0 W"
// it reads as a dead sensor. Collapsing "Not charging" into "Full" — the
// tempting simplification, since both mean no flow — would throw away exactly
// the distinction that explains it, because the pack is *not* full: it is at
// 81% against a 74% limit.
func TestNotChargingIsDistinctFromFull(t *testing.T) {
	f := newFakeSysfs(t)

	f.writeFile(t, f.battery+"/status", "Not charging")
	f.writeFile(t, f.battery+"/capacity", "81")
	f.writeFile(t, f.battery+"/charge_control_end_threshold", "74")

	if got := ReadBatteryState(); got != driver.BatteryStateNotCharging {
		t.Fatalf("ReadBatteryState() = %q, want %q", got, driver.BatteryStateNotCharging)
	}
	if driver.BatteryStateNotCharging == driver.BatteryStateFull {
		t.Fatal("held-below-threshold and full must stay distinguishable")
	}
	// And the flow really is zero, which is what needed explaining.
	watts, err := ReadBatteryPowerW()
	if err != nil {
		t.Fatalf("ReadBatteryPowerW() = %v", err)
	}
	if watts != 0 {
		t.Errorf("= %v W, want 0 — nothing flows into or out of a pack held above its limit", watts)
	}
}
