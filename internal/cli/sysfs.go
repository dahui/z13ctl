package cli

// sysfs.go — sysfs path discovery helpers shared by cmd/ and internal/daemon/.

import (
	"os"
	"path/filepath"
)

// FindProfilePath returns the writable sysfs path for the platform-profile attribute.
// It prefers the class device path (where udev applies group permissions) over the
// ACPI alias, which is a separate kernel object.
func FindProfilePath() string {
	const dir = "/sys/class/platform-profile"
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			p := dir + "/" + e.Name() + "/profile"
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "/sys/firmware/acpi/platform_profile"
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
