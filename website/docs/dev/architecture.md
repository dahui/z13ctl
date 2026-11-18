---
title: Architecture
description: How voltaire is put together — device files, the driver registry, the safety engine, and how capabilities reach the clients.
---

# Architecture

This page is for people adding support for a machine or changing how voltaire
talks to hardware. It is a map: what each part does, and which other page
covers it in depth.

## The shape in one paragraph

A **device file** (TOML, embedded in the binary) describes one family of
machines: how to recognise it, and which kernel interface drives each
capability. The daemon matches the running machine against the device files by
DMI, then builds each capability from its block's `method` through a
**registry** of compiled-in driver factories. The result is a `device.Device`:
one field per hardware class, `nil` where the machine has no such capability.
Handlers in the daemon program against those fields, with a **safety engine**
between them and the power limits. The daemon serves the result to clients as
the **`device-get` capability document**, and the CLI and GUI draw only what
that document says exists.

```
devices/*.toml ──DMI match──▶ Config ──method──▶ registry ──▶ driver.* implementations
                                                                 │
                       device.Device { Fans, Power(safety.Engine), Profiles, … }
                                                                 │
                     daemon handlers ──▶ get-state / device-get ──▶ CLI, GUI, plugins
```

## Packages

| Package | Holds |
|---|---|
| `internal/driver` | The per-class interfaces (`FanController`, `PowerLimiter`, `ProfileController`, `Lighting`, `Toggles`, `Battery`, `Undervolter`, `CPUBoost`, `Telemetry`, `Buttons`) and the values they carry. Its package comment is the driver contract. |
| `internal/device` | Device files (`devices/*.toml`), their schema and validation (`config.go`), DMI matching, the method registries, and `Assemble`. |
| `internal/drivers/asus` | ASUS firmware interfaces: the asus-wmi fan pair, PPT power limits (asus-armoury, else asus-nb-wmi), the allowlisted asus-armoury toggles, `charge_mode`, `throttle_thermal_policy`. |
| `internal/drivers/amd` | AMD silicon: the ryzen_smu Curve Optimizer, the pm_table parser, amdgpu, the amdxdna NPU, k10temp. |
| `internal/drivers/linux` | Generic Linux: `platform_profile`, `power_supply`, RAPL, cpufreq boost, procfs CPU/memory/network, and the telemetry composer. |
| `internal/drivers/aurahid`, `evdevkey` | The Aura HID lighting protocol; one key on an evdev device. |
| `internal/drivers/register` | Wires method names to factories. The one place allowed to join drivers from different packages. |
| `internal/safety` | The thermal-safety rules (the high-TDP fan floor) and `Engine`, the only path to a power-limit write. |
| `internal/daemon` | The socket server, state, and the watchers (reconcile, power source, sleep, telemetry, hotplug, button). |
| `api/` | The public client module: wire types, `Send*` functions, `DeviceInfo`. |

## Method names

The device file names a driver by its kernel interface, not by vendor.
Several methods are served by a vendor package, but nothing in the name ties
them to one machine:

| Block | Method | Package |
|---|---|---|
| `[fans]` | `asus-nb-wmi` | `asus` |
| `[power]` | `asus-nb-wmi-ppt` | `asus` (asus-armoury chosen at runtime where writable) |
| `[profiles]` | `platform-profile` | `linux` |
| `[toggles]` | `asus-armoury` | `asus` |
| `[battery]` | `power-supply` | `linux` (`charger = "asus-armoury"` adds `asus`'s firmware reading) |
| `[undervolt]` | `ryzen-smu-co` | `amd` |
| `[cpu] boost` | `cpufreq` | `linux` |
| `[telemetry]` | `hwmon-rapl` | `linux`, with `amd`'s temperature, GPU and NPU sources |
| `[lighting]` | `aura-hid` | `aurahid` |
| `[button]` | `evdev-key` | `evdevkey` |

Every key of every block is documented in [Device files](/dev/device-files).

## How drivers from different packages meet

No driver package imports another. Where one capability needs something
another package owns, the factory in `internal/drivers/register` passes it in:

- **Charger kind.** `linux.NewBattery(caps, charger)` takes an optional
  function. With `charger = "asus-armoury"` the registry passes
  `asus.ChargerFromArmoury`; otherwise the battery reads the kind from
  `power_supply`.
- **Policy-write notifications.** `linux.NewProfileController(…, extra...)`
  takes extra sysfs paths to watch. With `handler = "asus-wmi"` the registry
  adds asus-wmi's `throttle_thermal_policy`, which the kernel notifies on every
  profile write and every fan release.
- **Telemetry sources.** `linux.NewTelemetry(info, sources)` composes
  vendor-neutral readings (RAPL, procfs, power_supply) with the sources it is
  handed: AMD's APU temperature, GPU and NPU readers on the Z13.
- **Fan speed.** Telemetry drivers leave RPM empty; the daemon merges the fan
  driver's `ReadRPM` into each sample (`liveSample`), so fan speed has one
  reader.

## The safety engine

`device.Device` carries a `safety.Engine`, never the raw `PowerLimiter`. Every
power-limit write goes through `Engine.ApplyTDPSafely`, which writes the
device's fan floor before any sustained limit above `tdp_max_safe` and refuses
the limit if the floor does not take. Releases run the other way round: lower
the power first, then hand the fans back. The rules are pure functions of the
device's envelope (`driver.PowerEnvelope`, from `[power]`), so a new device
gets them by declaring a floor curve and a safe maximum, with no code.

Drivers have no part in this. A driver reports facts and performs the writes
it is handed; it never decides whether a write is safe.

## Discovery by absence

A capability the machine lacks is a `nil` field on the device, a missing
section in `device-get`, and a control nobody draws. Absence is never an error:

- **CLI.** A command for a missing capability says the device does not have
  it.
- **GUI.** The editor builds only the cards the device has; the Profiles tab
  needs something for a custom profile to hold; a device without a hardware
  button gets no button preference.
- **No daemon.** When the daemon did not answer, clients fall back to the
  built-in Z13 defaults rather than blanking. No answer is not evidence the
  machine lacks anything.

## Where values come from

Every limit, range, count and name follows one precedence, in validation, UI,
help text and docs alike:

1. what the kernel or firmware reports (asus-armoury's PPT bounds, the hwmon
   curve-point count, which power limits exist);
2. the device file;
3. a built-in default.

The wording a client shows comes through the same path. Power-limit labels,
the undervolt name and the brightness level names are all in `device-get`, so
the GUI says "PL2 (sPPT)" on an ASUS machine and whatever a device file
declares on another, without knowing which it is.

## Further reading

- [Device files](/dev/device-files): every key, with a complete example.
- [Writing a driver](/dev/writing-a-driver): adding a method.
- [Plugins](/dev/plugins): the planned external driver tier.
- [Daemon & socket protocol](/reference/daemon#device-capabilities):
  the `device-get` document clients read.
