package asusz13

// power.go — mains/battery power source discovery.

import (
	"fmt"
	"os"
	"strings"
)

// FindACOnlinePath returns the sysfs "online" path of the mains adapter, or ""
// when no Mains power supply is present.
//
// Devices must be selected by their type file, never by merely having an
// "online" file. On the Z13 the detachable keyboard registers as
// hid-*-battery-N (type Battery) and the two USB-C ports as ucsi-source-psy-*
// (type USB), and all of them expose "online" — so a */online glob picks up the
// keyboard's battery and reports "on AC" whenever the cover is attached.
func FindACOnlinePath() string {
	paths := mainsOnlinePaths()
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// mainsOnlinePaths returns the "online" path of every Mains power supply, in
// directory order. There is normally one, but a dock can add a second.
func mainsOnlinePaths() []string {
	entries, err := os.ReadDir(sysPowerSupplyDir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		base := sysPowerSupplyDir + "/" + e.Name()
		data, err := os.ReadFile(base + "/type")
		if err != nil || strings.TrimSpace(string(data)) != "Mains" {
			continue
		}
		online := base + "/online"
		if _, err := os.Stat(online); err != nil {
			continue
		}
		paths = append(paths, online)
	}
	return paths
}

// OnACPower reports whether the machine is running on mains power.
//
// It returns an error when no Mains supply exists — a VM, a desktop, or the
// ACPI driver not yet bound. Callers must treat that as *unknown* and take no
// action; reading it as "on battery" would have the daemon apply the battery
// profile to a machine that has no battery.
//
// A machine with a system battery but no Mains supply at all is charged over
// USB-C alone (a handheld, a thin laptop), and its USB supplies are what can
// answer: any System-scope USB supply online means it is plugged in. That
// fallback is gated on the battery so a desktop or VM — no battery, maybe a
// USB-C port — still reads as unknown rather than as "on battery".
func OnACPower() (bool, error) {
	paths := mainsOnlinePaths()
	if len(paths) == 0 {
		if on, ok := usbPowered(); ok {
			return on, nil
		}
		return false, fmt.Errorf("no mains power supply found under %s", sysPowerSupplyDir)
	}
	var lastErr error
	read := false
	for _, p := range paths {
		v, err := readIntFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		read = true
		// Any online adapter means mains power: a dock and the bundled charger
		// both register, and only the one actually plugged in reads 1.
		if v != 0 {
			return true, nil
		}
	}
	if !read {
		return false, fmt.Errorf("reading mains power state: %w", lastErr)
	}
	return false, nil
}

// usbPowered is the USB-C-only answer for OnACPower: ok only when the machine
// has a system battery and at least one System-scope USB supply, on is whether
// any of those is online. ucsi's online is cached connector state, not an
// ACPI call.
func usbPowered() (on, ok bool) {
	if systemBatteryDir() == "" {
		return false, false
	}
	entries, err := os.ReadDir(sysPowerSupplyDir)
	if err != nil {
		return false, false
	}
	for _, e := range entries {
		dir := sysPowerSupplyDir + "/" + e.Name()
		if readSysfsTrimmed(dir+"/type") != "USB" || readSysfsTrimmed(dir+"/scope") == "Device" {
			continue
		}
		ok = true
		if readSysfsTrimmed(dir+"/online") == "1" {
			on = true
		}
	}
	return on, ok
}
