// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package driver defines the per-hardware-class interfaces the daemon and CLI
// program against, so that device support is a set of implementations selected
// by device data rather than calls into one device's sysfs layout.
//
// The developer guide (website/src/content/docs/dev/writing-a-driver.md) walks
// through writing one; this comment is the contract it relies on.
//
// # Classes, not devices
//
// Interfaces are per hardware class — FanController, PowerLimiter,
// ProfileController, Lighting, Toggles, Battery, Undervolter, CPUBoost,
// Telemetry, Buttons — never per device. A device is an assembly of
// implementations: internal/device matches the machine's DMI identity against
// the embedded device files, and each capability block's `method` names the
// factory that builds that class's driver (registered in
// internal/drivers/register). One implementation serves every machine whose
// kernel interface it speaks, with what differs between machines carried as
// data — a fan count, a limit range, a zone table — not as a branch.
//
// # Absence is the answer
//
// A nil interface on the assembled device means the capability does not exist
// there, and that is exactly what the socket API reports to clients: a missing
// section in device-get, a control that is not drawn. Capability discovery is
// by absence, never by error. Within a class, ErrUnsupported is how a method
// says "this part is missing" (RPM readable, mode not) — the caller skips it,
// it does not report a failure.
//
// # Drivers are passive
//
// A driver must not start goroutines, must not hold locks across calls, and
// must not contain policy. Serialization is the daemon's (hwMu, then d.mu),
// and every safety rule — the high-TDP fan floor above all — lives in
// internal/safety, which sits between handlers and these interfaces: the
// assembled device carries a safety.Engine, never the raw PowerLimiter, so no
// handler can reach a power write without the floor. A driver that needs to
// "decide" something has the wrong shape: it reports facts (its envelope, its
// shape, its readings) and performs the writes it is handed.
//
// Passivity has a consequence for anything computed across calls. A rate
// needs the previous reading, which a driver cannot keep — Telemetry.Sample is
// called by the daemon's 1 Hz sampler and by every get-state — so drivers
// return counters (Sample.PackageEnergyUJ, CPU jiffies, network bytes) and the
// daemon's sampler, the one sequential caller, does the arithmetic.
//
// # Writes verify
//
// A write that the kernel accepted is not a write that took effect. Fan
// curves are dropped by any platform_profile write; amd-pstate refuses a boost
// change and reports success; an ignored release is indistinguishable from a
// successful one. Every write whose effect can be read back is read back, and
// a mismatch is an error (FanController.ApplyCurve, CPUBoost.Set). A failed
// readback is not an error — the write probably landed — but a readback that
// contradicts the write is.
//
// # Reading is not free
//
// Some reads are hardware transactions: an asus-armoury PPT read is a live
// ACPI call, a pm_table read is an SMU message, opening the NPU's accel node
// wakes it. Where a class has a cheap question and an expensive one, the
// interface splits them, and callers that only ask take the cheap one:
//
//   - Undervolter.Present is a stat; Available is the probe's cached answer
//     (module presence until something has probed); ProbeAvailable is the
//     probe itself — a CO-0 write, byte-for-byte a reset — and is called only
//     where an offset is about to be written.
//   - Lighting.PresentZones checks sysfs and opens nothing; Reopen opens.
//   - Telemetry.Info answers by stat and static attribute and is computed
//     once; Sample reads.
//
// # Declared means read
//
// A capability the device data declares must be one the driver actually
// reads. Discovery is by absence, so a declared-but-unread source does not
// degrade to "nothing shown" — it degrades to a chart flat at zero, which
// reads as a measurement. internal/daemon's declaration guards fail in both
// directions: a source declared with no reader, and a reading with no
// declaration.
//
// # Precedence
//
// Every limit, range, count or vocabulary a driver serves comes from, in
// order: what the kernel or firmware reports, then the device file, then a
// built-in default. The kernel's fan-curve point count beats the file's; the
// file's floor curve beats nothing; a limit's label comes from the file, else
// the driver's own wording. Where the kernel and the file disagree on a safety
// bound the conservative answer wins and a warning is logged.
//
// # Optional interfaces
//
// A behaviour only some implementations of a class have is an optional
// interface the caller type-asserts, never a method that some implementations
// answer with a no-op:
//
//   - PolicyWriteNotifier (ProfileController): the sysfs attributes the kernel
//     notifies on every policy write, which the daemon polls for POLLPRI.
//   - RebootPending (Toggles): whether a firmware setting waits on a restart.
//   - UndervoltDescriber (Undervolter): how to name the offset.
//   - UndervoltCommander (Undervolter): the command a write would send, for
//     the dry run.
//   - DeviceEnvelope (PowerLimiter): the device data's envelope alone, served
//     while reading the kernel's would reach a stalled embedded controller.
//
// # Testing
//
// A driver package keeps every sysfs root it touches in package variables
// (paths.go) and a TestMain that points each writable one at a path that does
// not exist before any test runs, so a test that forgets its fake tree fails
// with "no such file" instead of writing the developer's machine. Fakes build
// a temp-dir tree and swap the variables in; exec and mailbox I/O go through
// function variables the fakes replace. Never let a test reach a real sysfs
// write.
package driver
