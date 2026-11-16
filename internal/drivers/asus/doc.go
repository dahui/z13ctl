// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package asus holds the drivers for ASUS firmware interfaces, shared by every
// ASUS laptop and handheld the kernel's asus-wmi and asus-armoury drivers
// support: the fan pair (the `asus` readings hwmon and the
// `asus_custom_fan_curve` curve device), the PPT power limits (asus-armoury
// first, the deprecated asus-nb-wmi attributes as the fallback), the
// allowlisted asus-armoury firmware toggles and pending_reboot, the
// charge_mode charger reading, and throttle_thermal_policy, the attribute
// asus-wmi notifies when the firmware re-applies a profile's limits.
//
// What differs between ASUS machines is data, not code. Fan channels and curve
// points are enumerated from the kernel; which PPT limits exist, and their
// bounds, come from asus-armoury where it reports them; the device file names
// the rest. The ROG Flow Z13 (GZ302EA) is where all of it has run — comments
// that cite a measurement say "measured on the Z13". The one list that stays in
// code is the toggle allowlist (safeToggles), because attribute names are
// kernel ABI and some ASUS models expose attributes that can black-screen a
// machine.
//
// Sysfs roots live in paths.go as vars purely so the fake tree in
// sysfs_fake_test.go can redirect them; TestMain points every writable root
// nowhere before any test runs. Never let a test reach a real sysfs write.
//
// Like every driver package it is passive: no goroutines, no locks held across
// calls, no policy. The numbers come from device data and the rules live in
// internal/safety; generic Linux interfaces (platform_profile, power_supply)
// are internal/drivers/linux's and AMD silicon is internal/drivers/amd's.
package asus
