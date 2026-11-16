// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package linux holds the drivers built only on generic Linux interfaces, so
// they serve any machine whatever its vendor: platform_profile (with the
// power-profiles-daemon sync), the power_supply battery and mains supply,
// cpufreq boost, the powercap RAPL energy counter, procfs CPU and memory
// counters, /proc/net/dev, and the ACPI thermal zones.
//
// Its telemetry driver is a composer. It reads the generic sources itself and
// takes the vendor's — the processor's die temperature, the GPU, the NPU —
// from the registry (TelemetrySources), and fan speed is the daemon's to merge
// from the fan controller. Likewise the battery takes a vendor charger reader
// and the profile controller a vendor policy attribute. No package here
// imports a vendor driver; internal/drivers/register joins them.
//
// Like every driver package it is passive: no goroutines, no locks held
// across calls, no policy. The numbers come from device data and the rules
// live in internal/safety.
package linux
