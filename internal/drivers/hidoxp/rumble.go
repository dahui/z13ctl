// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package hidoxp drives what the kernel's hid-oxp driver (the OneXPlayer
// controller, Linux 7.x) exposes in sysfs beyond input: today the vibration
// strength, rumble_intensity, on the controller's vendor-interface HID device.
//
// Passive like every driver here: it reads and writes one attribute and keeps
// nothing. The attribute's scale is the kernel's (rumble_intensity_range,
// "0-5" on the X2 Mini Pro); a write outside it is refused here rather than
// sent. Checked on the X2 Mini Pro (Linux 7.3.4, 2026-11-17): 5, 1, 5, 1
// written between four identical rumble effects were felt as strong, weak,
// strong, weak, so the controller obeys it (unlike hid-oxp's RGB attributes
// on the same device, which the X2 ignores).
package hidoxp

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DriverDir is the hid-oxp driver's sysfs directory. Each bound HID device is
// a link in it named "bus:VVVV:PPPP.nnnn". Exported for voltaire setup, which
// grants the attribute below on every device bound here.
const DriverDir = "/sys/bus/hid/drivers/hid-oxp"

// RumbleAttr is the vibration-strength attribute, and rumbleRangeAttr the
// kernel's "lo-hi" range for it.
const (
	RumbleAttr      = "rumble_intensity"
	rumbleRangeAttr = "rumble_intensity_range"
)

// sysDriverDir is DriverDir as a var, so tests point it at a fake tree.
var sysDriverDir = DriverDir

// Rumble is driver.Rumble over hid-oxp's rumble_intensity.
type Rumble struct {
	vendor, product uint16
}

// NewRumble returns the control for the controller with this USB identity.
// Pure: nothing is read until it is used.
func NewRumble(vendor, product uint16) *Rumble {
	return &Rumble{vendor: vendor, product: product}
}

// errNoController is returned while no matching hid-oxp device carries the
// attribute: the driver is not loaded, or the controller has not enumerated.
var errNoController = errors.New("no hid-oxp controller with " + RumbleAttr + " (is the hid-oxp driver loaded?)")

// dir finds the controller's HID device directory: a link in the driver's
// directory whose name carries this vendor and product and which has the
// attribute. Several HID devices of one controller can bind to hid-oxp; only
// its vendor interface has the attribute.
func (r *Rumble) dir() (string, error) {
	entries, err := os.ReadDir(sysDriverDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errNoController
		}
		return "", err
	}
	want := fmt.Sprintf(":%04X:%04X.", r.vendor, r.product)
	for _, e := range entries {
		if !strings.Contains(strings.ToUpper(e.Name()), want) {
			continue
		}
		d := sysDriverDir + "/" + e.Name()
		if _, err := os.Stat(d + "/" + RumbleAttr); err == nil {
			return d, nil
		}
	}
	return "", errNoController
}

func readInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", path, strings.TrimSpace(string(b)))
	}
	return v, nil
}

// Range is the kernel's scale for the strength.
func (r *Rumble) Range() (lo, hi int, err error) {
	d, err := r.dir()
	if err != nil {
		return 0, 0, err
	}
	b, err := os.ReadFile(d + "/" + rumbleRangeAttr)
	if err != nil {
		return 0, 0, err
	}
	return parseRange(strings.TrimSpace(string(b)))
}

// parseRange reads "lo-hi", refusing anything else (including lo > hi).
func parseRange(s string) (lo, hi int, err error) {
	a, b, ok := strings.Cut(s, "-")
	lo, err1 := strconv.Atoi(strings.TrimSpace(a))
	hi, err2 := strconv.Atoi(strings.TrimSpace(b))
	if !ok || err1 != nil || err2 != nil || lo > hi {
		return 0, 0, fmt.Errorf("%s: %q is not a lo-hi range", rumbleRangeAttr, s)
	}
	return lo, hi, nil
}

// Get reads the strength. hid-oxp answers from the value it last sent, which
// is what the controller was told.
func (r *Rumble) Get() (int, error) {
	d, err := r.dir()
	if err != nil {
		return 0, err
	}
	return readInt(d + "/" + RumbleAttr)
}

// Set writes the strength, refusing a value outside the kernel's range before
// anything is written.
func (r *Rumble) Set(v int) error {
	lo, hi, err := r.Range()
	if err != nil {
		return err
	}
	if v < lo || v > hi {
		return fmt.Errorf("rumble strength %d is outside %d–%d", v, lo, hi)
	}
	d, err := r.dir()
	if err != nil {
		return err
	}
	return os.WriteFile(d+"/"+RumbleAttr, []byte(strconv.Itoa(v)), 0o644)
}
