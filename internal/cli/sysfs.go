package cli

// sysfs.go — sysfs path discovery helpers shared by cmd/ and internal/daemon/.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FindProfilePath returns the writable sysfs path for the ASUS platform-profile attribute.
// It prefers the device whose choices file contains "quiet" (the asus-wmi device), falling
// back to the first device with a profile file, then the ACPI alias.
func FindProfilePath() string {
	const dir = "/sys/class/platform-profile"
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
	return "/sys/firmware/acpi/platform_profile"
}

// SetProfile writes the given ASUS profile (quiet/balanced/performance) to all
// platform_profile sysfs devices, mapping "quiet" to "low-power" for devices that
// do not support that name. Returns an error only if the primary ASUS device write
// fails; secondary device errors are ignored.
func SetProfile(profile string) error {
	const dir = "/sys/class/platform-profile"
	primaryPath := FindProfilePath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return os.WriteFile(primaryPath, []byte(profile+"\n"), 0o644)
	}
	var primaryErr error
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
		}
	}
	if primaryErr == nil {
		setPPD(profile)
	}
	return primaryErr
}

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
	const glob = "/sys/class/power_supply/BAT*/charge_control_end_threshold"
	matches, _ := filepath.Glob(glob)
	if len(matches) > 0 {
		return matches[0]
	}
	return "/sys/class/power_supply/BAT0/charge_control_end_threshold"
}

// FindBootSoundPath returns the sysfs path for the boot sound firmware attribute.
func FindBootSoundPath() string {
	return "/sys/class/firmware-attributes/asus-armoury/attributes/boot_sound/current_value"
}

// FindPanelOverdrivePath returns the sysfs path for the panel overdrive firmware attribute.
func FindPanelOverdrivePath() string {
	return "/sys/class/firmware-attributes/asus-armoury/attributes/panel_overdrive/current_value"
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
	const glob = "/sys/class/power_supply/BAT*/capacity"
	matches, _ := filepath.Glob(glob)
	if len(matches) > 0 {
		return matches[0]
	}
	return "/sys/class/power_supply/BAT0/capacity"
}
