package asusz13

// sysfs.go — sysfs path discovery helpers shared by cmd/ and internal/daemon/.

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// FindProfilePath returns the writable sysfs path for the ASUS platform-profile attribute.
// It prefers the device whose choices file contains "quiet" (the asus-wmi device), falling
// back to the first device with a profile file, then the ACPI alias.
func FindProfilePath() string {
	dir := sysProfileDir
	entries, err := os.ReadDir(dir)
	if err == nil {
		// First pass: prefer the ASUS device (choices includes "quiet").
		for _, e := range entries {
			base := dir + "/" + e.Name()
			if profileDeviceSupports(base, "quiet") {
				p := base + "/profile"
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		// Fallback: first device with a profile file.
		for _, e := range entries {
			p := dir + "/" + e.Name() + "/profile"
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return sysProfileACPI
}

// SetProfile writes the given ASUS profile (quiet/balanced/performance) to all
// platform_profile sysfs devices, mapping "quiet" to "low-power" for devices that
// do not support that name. Returns an error only if the primary ASUS device write
// fails; secondary device errors are ignored.
func SetProfile(profile string) error {
	dir := sysProfileDir
	primaryPath := FindProfilePath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return os.WriteFile(primaryPath, []byte(profile+"\n"), 0o644)
	}
	var primaryErr error
	primaryWritten := false
	for _, e := range entries {
		base := dir + "/" + e.Name()
		p := base + "/profile"
		if _, err := os.Stat(p); err != nil {
			continue
		}
		name := profileNameForDevice(base, profile)
		werr := os.WriteFile(p, []byte(name+"\n"), 0o644)
		if p == primaryPath {
			primaryErr = werr
			primaryWritten = true
		}
	}
	// The loop may not have covered the primary at all: when no device under
	// sysProfileDir exposes a profile file, FindProfilePath falls back to the
	// ACPI alias, which lives outside that directory. Without this the function
	// returned nil having written nothing — a silent no-op profile switch that
	// still told power-profiles-daemon the change had happened.
	if !primaryWritten {
		primaryErr = os.WriteFile(primaryPath, []byte(profile+"\n"), 0o644)
	}
	if primaryErr == nil {
		setPPD(profile)
	}
	return primaryErr
}

// ppdRunner applies a power-profiles-daemon profile name. It is a var so tests
// can stub it out — otherwise exercising SetProfile would shell out and change
// the developer's live power profile as a side effect.
var ppdRunner = execPPD

// setPPD updates the power-profiles-daemon active profile to match the given
// ASUS profile. No-op if powerprofilesctl is not installed. Silently ignores
// errors if PPD is installed but not currently running.
func setPPD(asusProfile string) {
	ppd := map[string]string{
		"quiet":       "power-saver",
		"balanced":    "balanced",
		"performance": "performance",
	}[asusProfile]
	if ppd == "" {
		return
	}
	ppdRunner(ppd)
}

// execPPD invokes powerprofilesctl, if it is installed.
func execPPD(ppd string) {
	bin, err := exec.LookPath("powerprofilesctl")
	if err != nil {
		return
	}
	_ = exec.Command(bin, "set", ppd).Run()
}

// profileDeviceSupports reports whether the platform_profile device at base
// lists name in its choices file.
func profileDeviceSupports(base, name string) bool {
	data, err := os.ReadFile(base + "/choices")
	if err != nil {
		return false
	}
	for _, p := range strings.Fields(string(data)) {
		if p == name {
			return true
		}
	}
	return false
}

// profileNameForDevice returns the appropriate profile name for a given device,
// mapping the ASUS-specific "quiet" to "low-power" for devices that don't support it.
func profileNameForDevice(base, asusProfile string) string {
	if asusProfile != "quiet" {
		return asusProfile // "balanced" and "performance" are universal
	}
	if profileDeviceSupports(base, "quiet") {
		return "quiet"
	}
	if profileDeviceSupports(base, "low-power") {
		return "low-power"
	}
	return asusProfile
}

// FindBatteryThresholdPath returns the writable sysfs path for the battery charge
// end threshold. It globs BAT* to avoid hardcoding BAT0 vs BAT1.
func FindBatteryThresholdPath() string {
	matches, _ := filepath.Glob(sysPowerSupplyDir + "/BAT*/charge_control_end_threshold")
	if len(matches) > 0 {
		return matches[0]
	}
	return sysPowerSupplyDir + "/BAT0/charge_control_end_threshold"
}

// FindBootSoundPath returns the sysfs path for the boot sound firmware attribute.
func FindBootSoundPath() string {
	return sysFirmwareAttrDir + "/boot_sound/current_value"
}

// FindPanelOverdrivePath returns the sysfs path for the panel overdrive firmware attribute.
func FindPanelOverdrivePath() string {
	return sysFirmwareAttrDir + "/panel_overdrive/current_value"
}

// SetBootSound writes the given boot sound value (0 or 1) to the firmware attribute.
func SetBootSound(value int) error {
	return os.WriteFile(FindBootSoundPath(), []byte(strconv.Itoa(value)+"\n"), 0o644)
}

// SetPanelOverdrive writes the given panel overdrive value (0 or 1) to the firmware attribute.
func SetPanelOverdrive(value int) error {
	return os.WriteFile(FindPanelOverdrivePath(), []byte(strconv.Itoa(value)+"\n"), 0o644)
}

// FindAPUTemperaturePath returns the sysfs path for the APU temperature sensor.
// Uses the k10temp hwmon device (AMD Ryzen thermal sensor, label "Tctl").
func FindAPUTemperaturePath() string {
	dir := FindFanHwmonPath("k10temp")
	if dir == "" {
		return ""
	}
	return dir + "/temp1_input"
}

// ReadAPUTemperature reads the current APU die temperature in degrees Celsius.
// The k10temp driver reports millidegrees; this function converts to whole degrees.
func ReadAPUTemperature() (int, error) {
	path := FindAPUTemperaturePath()
	if path == "" {
		return 0, fmt.Errorf("k10temp hwmon device not found")
	}
	milli, err := readIntFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading APU temperature: %w", err)
	}
	return milli / 1000, nil
}

// FindBatteryCapacityPath returns the sysfs path for the current battery charge level.
func FindBatteryCapacityPath() string {
	return findBatteryAttr("capacity")
}

// findBatteryAttr returns the path to one attribute of the system battery,
// globbing BAT* to avoid hardcoding BAT0 vs BAT1 as the other finders do. The
// BAT0 fallback keeps the returned path nameable in an error message when no
// battery exists at all.
func findBatteryAttr(name string) string {
	matches, _ := filepath.Glob(sysPowerSupplyDir + "/BAT*/" + name)
	if len(matches) > 0 {
		return matches[0]
	}
	return sysPowerSupplyDir + "/BAT0/" + name
}

// ReadBatteryHealthPercent returns the battery's full-charge capacity as a
// percentage of its design capacity.
//
// It reads whichever pair the supply publishes: this machine's ACPI battery
// reports energy_full/energy_full_design in µWh, while many power_supply
// drivers report charge_full/charge_full_design in µAh. The units cancel in
// the ratio, which is exactly why driver.BatteryStatus carries the ratio and
// not the pair.
//
// The result is not clamped to 100. A freshly calibrated pack genuinely reads
// slightly above its design capacity, and reporting 103% is more useful than a
// number that has been quietly adjusted to look plausible.
func ReadBatteryHealthPercent() (int, error) {
	for _, pair := range [][2]string{
		{"energy_full", "energy_full_design"},
		{"charge_full", "charge_full_design"},
	} {
		full, err := readIntFile(findBatteryAttr(pair[0]))
		if err != nil {
			continue
		}
		design, err := readIntFile(findBatteryAttr(pair[1]))
		if err != nil || design <= 0 {
			continue
		}
		return int(math.Round(float64(full) * 100 / float64(design))), nil
	}
	return 0, fmt.Errorf("battery state of health: neither energy_full nor charge_full is readable")
}

// ReadCharger reports which power input is supplying the machine, or
// driver.ChargerUnknown when the firmware does not say.
//
// The Z13 takes power two ways — the proprietary high-wattage DC adapter shared
// with the Zephyrus line, and USB-C Power Delivery — and the *Mains* supply
// reads online for both. That is correct (mains power is attached either way)
// and is exactly why OnACPower cannot answer this question: asus-armoury's
// charge_mode is the only thing on the machine that distinguishes them.
//
// The vocabulary was established by observation rather than inference, with the
// charger physically swapped: on the DC adapter the attribute reads 1 with both
// ucsi-source-psy ports offline; on USB-C it reads 2 with a ucsi port online and
// its usb_type showing PD active. 0 is documented by possible_values and is the
// only remaining case, but has not been observed — a machine with no charger
// attached is on battery, where nobody was watching this attribute.
func ReadCharger() driver.Charger {
	v, err := readIntFile(sysFirmwareAttrDir + "/charge_mode/current_value")
	if err != nil {
		return driver.ChargerUnknown
	}
	switch v {
	case 0:
		return driver.ChargerNone
	case 1:
		return driver.ChargerAdapter
	case 2:
		return driver.ChargerUSBC
	}
	// A value this build does not know is unknown, not a guess. Firmware may
	// grow a kind before voltaire does, and naming it wrongly is worse than
	// saying nothing.
	return driver.ChargerUnknown
}
