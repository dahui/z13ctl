// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package amd holds the drivers and readers for AMD silicon, whichever vendor
// built the machine around it: the ryzen_smu mailbox and the Curve Optimizer
// undervolt that goes through it, the SMU power-table parser, k10temp's die
// temperature, and the amdgpu and amdxdna (NPU) telemetry. The ROG Flow Z13
// (Strix Halo) is where all of it has run; the per-CPU tables say which rows
// are measured and which are transcribed.
//
// The SMU rules are the ones that matter most here. A Curve Optimizer probe is
// itself a write, a mailbox command can collide with the kernel's own PMFW
// traffic, and the driver shares one argument buffer across mailboxes — so
// every SMU message goes through smuMu, a CPU this package has no command for
// is refused before anything is written, and nothing probes unless an offset
// is about to be written (see SMUProbeUndervolt).
//
// Like every driver package it is passive: no goroutines, no locks held
// across calls but its own serialization of the mailbox, no policy. The
// numbers come from device data and the rules live in internal/safety.
package amd
