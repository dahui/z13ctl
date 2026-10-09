// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// firmware.go — which firmware profiles the selector offers, and what each is
// called. The device says (device-get's profiles section); the Z13's three are
// only the fallback for a client the daemon has not answered.

import "github.com/dahui/voltaire/api/v2"

// Firmware is the device's firmware profiles in display order, each with its
// label.
type Firmware []api.ProfileEntry

// FirmwareFrom reads the firmware profiles from the device document. A nil
// document — no daemon, or one too old to answer device-get — yields the
// built-in list, the same posture limits.FromDevice and controls.Resolve take:
// no answer is not evidence the machine lacks profiles. A daemon that sends
// names without labelled entries (older than the field) gets fallback labels.
func FirmwareFrom(info *api.DeviceInfo) Firmware {
	if info == nil || info.Profiles == nil || len(info.Profiles.Names) == 0 {
		return defaultFirmware()
	}
	p := info.Profiles
	labels := make(map[string]string, len(p.Entries))
	for _, e := range p.Entries {
		labels[e.Name] = e.Label
	}
	out := make(Firmware, 0, len(p.Names))
	for _, n := range p.Names {
		l := labels[n]
		if l == "" {
			l = api.ProfileLabel(n)
		}
		out = append(out, api.ProfileEntry{Name: n, Label: l})
	}
	return out
}

// defaultFirmware is the 2025 ROG Flow Z13's list, used only when there is no
// device document to read.
func defaultFirmware() Firmware {
	return Firmware{
		{Name: "quiet", Label: "Quiet"},
		{Name: "balanced", Label: "Balanced"},
		{Name: "performance", Label: "Performance"},
	}
}

// Names lists the profile names in display order.
func (f Firmware) Names() []string {
	out := make([]string, len(f))
	for i, e := range f {
		out[i] = e.Name
	}
	return out
}

// Label is the device's label for a firmware profile name, falling back to
// the package-wide Label for anything else (a custom profile, or a name the
// device does not list).
func (f Firmware) Label(name string) string {
	for _, e := range f {
		if e.Name == name {
			return e.Label
		}
	}
	return Label(name)
}
