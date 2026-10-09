package asusz13

// sysfs.go — sysfs path discovery helpers shared by cmd/ and internal/daemon/.

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// FindProfilePath returns the owning platform-profile attribute when the
// caller has no handler name: FindProfilePathFor("").
func FindProfilePath() string { return FindProfilePathFor("") }

// FindProfilePathFor returns the profile attribute of the platform-profile
// class device whose `name` is handler (asus-wmi on the Z13). With no handler,
// or none by that name, it falls back to the old heuristic — the device whose
// choices contain "quiet", then the first with a profile file — and finally to
// the ACPI alias. Choosing by name is what makes the answer deterministic on a
// machine with several handlers; the heuristic only keeps a device file without
// a handler working.
func FindProfilePathFor(handler string) string {
	dir := sysProfileDir
	entries, err := os.ReadDir(dir)
	if err == nil {
		if handler != "" {
			for _, e := range entries {
				base := dir + "/" + e.Name()
				if readSysfsTrimmed(base+"/name") == handler {
					p := base + "/profile"
					if _, err := os.Stat(p); err == nil {
						return p
					}
				}
			}
		}
		for _, e := range entries {
			base := dir + "/" + e.Name()
			if profileDeviceSupports(base, "quiet") {
				p := base + "/profile"
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		for _, e := range entries {
			p := dir + "/" + e.Name() + "/profile"
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return sysProfileACPI
}

// SetProfile is SetProfileFor with no handler name.
func SetProfile(profile string) error { return SetProfileFor("", profile) }

// SetProfileFor writes profile to every platform_profile class device, the
// owning handler's write being the one whose error counts. A secondary handler
// that does not offer the name gets its equivalent (profileNameForDevice);
// its errors are ignored. power-profiles-daemon is told afterwards.
func SetProfileFor(handler, profile string) error {
	dir := sysProfileDir
	primaryPath := FindProfilePathFor(handler)
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
		if ppd := PPDProfile(profile); ppd != "" {
			ppdRunner(ppd)
		}
	}
	return primaryErr
}

// ppdRunner applies a power-profiles-daemon profile name. It is a var so tests
// can stub it out — otherwise exercising SetProfile would shell out and change
// the developer's live power profile as a side effect.
var ppdRunner = execPPD

// PPDProfile maps a kernel platform_profile name to the power-profiles-daemon
// profile that represents it, or "" when there is none to sync. It mirrors
// PPD's own platform_profile driver, which selects low-power or quiet for
// power-saver: the low-power group maps down, the performance group up.
// One copy, shared with the dry-run, so the two cannot describe different
// syncs.
func PPDProfile(name string) string {
	switch name {
	case "low-power", "cool", "quiet":
		return "power-saver"
	case "balanced":
		return "balanced"
	case "balanced-performance", "performance", "max-power":
		return "performance"
	}
	return ""
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
	return slices.Contains(readChoices(base+"/profile"), name)
}

// readChoices lists the choices of the class device whose profile attribute
// is profilePath, or nil when there are none to read (the ACPI alias, whose
// choices file is a sibling named platform_profile_choices, is handled too).
func readChoices(profilePath string) []string {
	path := filepath.Dir(profilePath) + "/choices"
	if profilePath == sysProfileACPI {
		path = sysProfileACPI + "_choices"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

// lowPowerGroup is the kernel profiles a handler may spell differently for the
// same intent: asus-wmi says quiet where amd-pmf says low-power. Only this
// group is translated. Mapping performance onto max-power, say, would raise a
// limit the user did not choose.
var lowPowerGroup = []string{"quiet", "low-power", "cool"}

// profileNameForDevice returns the name to write to the handler at base: the
// profile itself when the handler offers it, otherwise the first member of its
// equivalence group the handler does offer, otherwise the profile unchanged
// (the write then fails on that handler, which is ignored for a secondary).
func profileNameForDevice(base, profile string) string {
	if !slices.Contains(lowPowerGroup, profile) || profileDeviceSupports(base, profile) {
		return profile
	}
	for _, alt := range lowPowerGroup {
		if alt != profile && profileDeviceSupports(base, alt) {
			return alt
		}
	}
	return profile
}

// resolveProfileNames is the firmware profile list a device offers: the
// kernel's choices filtered by the device data's names, in the data's order,
// with the kernel's "custom" (handlers disagreeing, not a profile) never
// offered. With no kernel list the data stands alone; with no overlap at all
// the data is wrong for this kernel and the kernel's list is used instead, so
// nothing the handler would refuse is ever offered.
func resolveProfileNames(declared, choices []string) []string {
	var offered []string
	for _, c := range choices {
		if c != "custom" {
			offered = append(offered, c)
		}
	}
	if len(offered) == 0 {
		return append([]string(nil), declared...)
	}
	var out []string
	for _, n := range declared {
		if slices.Contains(offered, n) {
			out = append(out, n)
		} else {
			slog.Warn("device data names a firmware profile the kernel does not offer; not offering it",
				"profile", n, "kernel", offered)
		}
	}
	if len(out) == 0 {
		slog.Warn("no declared firmware profile is offered by the kernel; using the kernel's list",
			"declared", declared, "kernel", offered)
		return offered
	}
	return out
}

// normalizeProfile maps a kernel profile read to one of names. A name the
// device offers passes through; a low-power-group spelling maps to the group
// member the device offers; anything else — notably the kernel's "custom",
// which means its handlers disagree — is an error the caller reads as unknown.
func normalizeProfile(raw string, names []string) (string, error) {
	if slices.Contains(names, raw) {
		return raw, nil
	}
	if slices.Contains(lowPowerGroup, raw) {
		for _, n := range names {
			if slices.Contains(lowPowerGroup, n) {
				return n, nil
			}
		}
	}
	return "", fmt.Errorf("platform profile reads %q, which is not one of this device's profiles %v", raw, names)
}

// readSysfsTrimmed reads a small sysfs file, "" on any error.
func readSysfsTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// FindBatteryThresholdPath returns the writable sysfs path for the system
// battery's charge end threshold (systemBatteryDir chooses the pack).
func FindBatteryThresholdPath() string {
	return findBatteryAttr("charge_control_end_threshold")
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

// findBatteryAttr returns the path to one attribute of the system battery
// (systemBatteryDir). The BAT0 fallback keeps the returned path nameable in an
// error message when no battery exists at all.
func findBatteryAttr(name string) string {
	if dir := systemBatteryDir(); dir != "" {
		return dir + "/" + name
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

// ReadChargerFromSupplies is the charger kind from the kernel's generic
// power_supply view, needing no firmware call: a USB supply of system scope
// that is online (ucsi's port supply, reporting a PD contract) means USB-C;
// otherwise mains power means the dedicated adapter, and no mains means
// none. onAC and acKnown are the mains reading the caller already took, so
// this adds no second AC read (each is an ACPI _PSR evaluation); the ucsi
// supply's `online` is cached connector state.
//
// It reproduces the Z13's charge_mode values as measured (adapter: both
// ucsi supplies offline; USB-C: one online), but the Z13 keeps reading
// charge_mode until the USB-C half has been checked against this rule on
// hardware.
func ReadChargerFromSupplies(onAC, acKnown bool) driver.Charger {
	if entries, err := os.ReadDir(sysPowerSupplyDir); err == nil {
		for _, e := range entries {
			dir := sysPowerSupplyDir + "/" + e.Name()
			if readSysfsTrimmed(dir+"/type") != "USB" || readSysfsTrimmed(dir+"/scope") == "Device" {
				continue
			}
			if readSysfsTrimmed(dir+"/online") == "1" {
				return driver.ChargerUSBC
			}
		}
	}
	switch {
	case !acKnown:
		return driver.ChargerUnknown
	case onAC:
		return driver.ChargerAdapter
	}
	return driver.ChargerNone
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
