// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package asusz13

// rapl.go — package power from the kernel's powercap RAPL interface, and
// battery flow from power_supply.
//
// Neither is instantaneous in the form the hardware offers it. RAPL publishes a
// cumulative *energy* counter in microjoules, so power is the difference
// between two readings over the time between them — arithmetic this package
// deliberately does not do, because it needs the previous reading and drivers
// hold no state (see driver.Sample). What is here is the read and the
// discovery.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// raplPackageName is the powercap domain that measures the whole CPU package.
// The interface also exposes sub-domains (core, uncore, dram) whose energy is a
// *part* of this one, so summing them would double-count; a dashboard asking
// for "package power" wants exactly this domain.
const raplPackageName = "package-0"

// FindRAPLPackagePath returns the powercap directory measuring package energy.
//
// Discovery is by the `name` attribute rather than by the `intel-rapl:0` path,
// for the same reason fan hwmon discovery is: the numbering is assignment
// order, not identity. The name is `intel-rapl` on AMD too — the driver binds
// to AMD's compatible MSRs and keeps the interface name — so matching on the
// vendor string would find nothing on the machine this package is for.
func FindRAPLPackagePath() (string, error) {
	entries, err := os.ReadDir(sysPowercapDir)
	if err != nil {
		return "", fmt.Errorf("powercap not available: %w", err)
	}
	for _, e := range entries {
		dir := sysPowercapDir + "/" + e.Name()
		name, err := os.ReadFile(dir + "/name")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(name)) == raplPackageName {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no powercap domain named %s", raplPackageName)
}

// ReadPackageEnergy returns the cumulative package energy counter in
// microjoules and the value it wraps at.
//
// The permission note matters more than the read. energy_uj is 0400 root:root
// out of the box — the Platypus side-channel mitigation, which turned a
// world-readable counter into a privileged one because it leaks enough about
// power draw to infer what the CPU is doing. voltaire setup grants the voltaire
// group read access through a udev rule and the perms unit, exactly as it does
// for the PPT attributes; without that grant this returns a permission error
// and the caller reports no package power rather than failing the whole sample.
func ReadPackageEnergy() (energyUJ, maxUJ uint64, err error) {
	dir, err := FindRAPLPackagePath()
	if err != nil {
		return 0, 0, err
	}
	energyUJ, err = readUint(dir + "/energy_uj")
	if err != nil {
		return 0, 0, err
	}
	// A missing range is not fatal: it is only needed to tell a wrap from a
	// counter reset, and zero is the documented "unknown".
	maxUJ, _ = readUint(dir + "/max_energy_range_uj")
	return energyUJ, maxUJ, nil
}

// ReadBatteryPowerW returns battery flow in watts: positive while discharging,
// negative while charging, zero while neither.
//
// Two hardware shapes, the same split as battery health. A pack reporting
// *energy* publishes power_now in microwatts directly; one reporting *charge*
// publishes current_now in microamps, which has to be multiplied by voltage_now
// to become power. The Z13's ACPI battery is the energy kind and has no
// current_now at all, so a reader written for only the charge form would find
// nothing on the machine it was written for — which is exactly how
// ReadBatteryHealthPercent's first draft went wrong.
//
// The sysfs value is unsigned magnitude on both paths; direction comes from
// `status`, and anything other than Charging or Discharging (Full, Not
// charging, Unknown) means nothing is flowing.
func ReadBatteryPowerW() (float64, error) {
	dir, err := findBatteryDir()
	if err != nil {
		return 0, err
	}

	microwatts, err := readUint(dir + "/power_now")
	if err != nil {
		currentUA, cErr := readUint(dir + "/current_now")
		voltageUV, vErr := readUint(dir + "/voltage_now")
		if cErr != nil || vErr != nil {
			return 0, fmt.Errorf("no battery power reading: %w", err)
		}
		// µA × µV = picowatts.
		return signedByStatus(dir, float64(currentUA)*float64(voltageUV)/1e12), nil
	}
	return signedByStatus(dir, float64(microwatts)/1e6), nil
}

// ReadBatteryState reports what the pack is doing, or BatteryStateUnknown when
// it cannot be read. It is the one place that knows power_supply's `status`
// vocabulary, so the sign convention below and the state on the wire cannot
// drift apart.
func ReadBatteryState() driver.BatteryState {
	dir, err := findBatteryDir()
	if err != nil {
		return driver.BatteryStateUnknown
	}
	state, _ := batteryStateIn(dir)
	return state
}

// batteryStateIn also reports whether `status` could be read at all, which is
// **not** the same as reading it and finding "Unknown". Both map to
// BatteryStateUnknown on the wire — a client can do nothing different with them
// — but signedByStatus treats them differently, so collapsing them here silently
// changes what a flow reading means. That happened: the two were merged during a
// refactor and TestReadBatteryPowerW caught it.
func batteryStateIn(dir string) (state driver.BatteryState, readable bool) {
	raw, err := os.ReadFile(dir + "/status")
	if err != nil {
		return driver.BatteryStateUnknown, false
	}
	return mapBatteryState(strings.TrimSpace(string(raw))), true
}

func mapBatteryState(raw string) driver.BatteryState {
	switch raw {
	case "Charging":
		return driver.BatteryStateCharging
	case "Discharging":
		return driver.BatteryStateDischarging
	case "Full":
		return driver.BatteryStateFull
	case "Not charging":
		// The pack could charge and is being held back — on this machine, by
		// the charge-end threshold. Deliberately distinct from Full: it is the
		// state that makes a 0 W flow reading make sense.
		return driver.BatteryStateNotCharging
	default:
		return driver.BatteryStateUnknown
	}
}

// signedByStatus applies the sign the magnitude in sysfs does not carry.
func signedByStatus(dir string, watts float64) float64 {
	state, readable := batteryStateIn(dir)
	if !readable {
		// No direction to be had. Reporting the magnitude as a draw is the
		// safer of the two guesses: a machine on battery is the case where the
		// number matters, and calling a discharge a charge would draw the graph
		// upside down.
		return watts
	}
	switch state {
	case driver.BatteryStateDischarging:
		return watts
	case driver.BatteryStateCharging:
		return -watts
	default:
		// Full, Not charging, Unknown — the firmware was asked and reported no
		// flow, which is a stronger statement than an unreadable file.
		return 0
	}
}

// findBatteryDir returns the first power_supply device of type Battery that
// carries a capacity — the machine's own pack.
//
// The type check is the same one FindACOnlinePath needs and for the same
// reason: on this machine the detachable keyboard registers as
// hid-*-battery-N, which is also type Battery and also has a capacity. It is
// ruled out by requiring a name beginning "BAT", which is what the ACPI battery
// driver names the system pack on every machine this runs on.
func findBatteryDir() (string, error) {
	entries, err := os.ReadDir(sysPowerSupplyDir)
	if err != nil {
		return "", fmt.Errorf("power_supply not available: %w", err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "BAT") {
			continue
		}
		dir := sysPowerSupplyDir + "/" + e.Name()
		if typ, err := os.ReadFile(dir + "/type"); err == nil &&
			strings.TrimSpace(string(typ)) != "Battery" {
			continue
		}
		return dir, nil
	}
	return "", fmt.Errorf("no system battery found under %s", sysPowerSupplyDir)
}

// readUint reads a sysfs file holding a single unsigned integer.
func readUint(path string) (uint64, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}
