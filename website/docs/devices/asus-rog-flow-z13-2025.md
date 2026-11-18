---
title: ASUS ROG Flow Z13 (2025)
description: What voltaire controls on the 2025 ASUS ROG Flow Z13 (GZ302, Strix Halo), what it needs installed, and its limits.
---

# ASUS ROG Flow Z13 (2025)

voltaire began on the 2025 ROG Flow Z13 (GZ302 series, AMD Strix Halo), and
this is the machine with the fullest support. Every GZ302 variant is detected
by its firmware's model name. The numbers on this page were measured on a
GZ302EA running Linux 7.3; another SKU or kernel may report different ranges,
and voltaire uses whatever the kernel reports.

## Compatibility

| Feature | Status | Notes |
|---|---|---|
| **TDP** | Yes | Sustained, slow and fast limits (PL1/PL2/PL3). 28–80 W sustained on the GZ302EA through asus-armoury ([below](#tdp-and-the-fan-floor)). |
| **Firmware power profiles** | Yes | Quiet, balanced and performance. |
| **Saved custom profiles** | Yes | Each holds its own fan curve, TDP and undervolt. |
| **AC / battery autoswitch** | Yes | A profile for each power source. |
| **CPU boost** | Yes | Put back at every boot. |
| **CPU undervolt** | Yes | Curve Optimizer, 0 to −40. Needs the [`ryzen_smu` module](/installation#optional-ryzen_smu-kernel-module) (amkillam's fork). |
| **Fan speed** | Yes | Both fans. |
| **Fan curves** | Yes | One 8-point curve drives both fans; Quiet, Balanced and Turbo presets as starting points. |
| **RGB lighting** | Yes | Keyboard backlight and edge lightbar, together or separately: color, effect, speed and brightness. |
| **Battery** | Yes | Level, charge and discharge rate, time estimate, pack health, and whether it is charging over USB-C or the adapter. |
| **Charge limit** | Yes | 40–100 %. |
| **Brightness and volume** | Yes | On any desktop and in Steam Gaming Mode. |
| **Refresh rate** | Yes | The panel's rate, and a rate per power source ([KDE Plasma only](/devices/#refresh-rate)). |
| **HDR** | No | The panel is not HDR-capable, and voltaire has no HDR switch on any device ([why](/devices/#hdr)). |
| **Hardware button** | Yes | The Armoury Crate button ([below](#buttons)). |
| **Controller vibration** | — | The Z13 has no built-in controller. |
| **Firmware switches** | Yes | POST boot sound and panel overdrive. |
| **Telemetry** | Yes | Temperatures, package/GPU/NPU/battery power, CPU, GPU and NPU load and clocks, memory and network, with up to an hour of history. |

## What it needs

- **`sudo voltaire setup`**, once, so that voltaire's group can reach the
  lighting, fan, power-limit, battery and firmware files without root.
- **For TDP:** a 7.x kernel, whose asus-armoury driver has the GZ302's
  power-limit table. On older kernels voltaire uses asus-nb-wmi's older power
  attributes instead, which exist only if the distribution builds them
  (`CONFIG_ASUS_WMI_DEPRECATED_ATTRS`).
- **For undervolting:** the `ryzen_smu` module, from amkillam's fork (the
  original does not support Strix Halo). See
  [Installation](/installation#optional-ryzen_smu-kernel-module).

## TDP and the fan floor

The limits come from the kernel: on the GZ302EA, 28–80 W sustained, with the
slow and fast limits raised to their minimums when set lower. Above the safe
sustained limit (75 W) voltaire holds both fans to at least 50 %, rising to
100 % at 80 °C, and refuses the limit if it cannot set the fans first. A fan
curve of your own is kept wherever it is already above that floor.

The firmware resets the power limits whenever the power profile changes or the
fans are handed back to automatic control. The daemon notices and puts a
custom TDP back within about two seconds, which is why a custom TDP needs the
daemon running. [Daemon](/reference/daemon) has the details.

## Buttons

Only the **Armoury Crate** button can open voltaire. The Z13's hotkey device
reports many other keys, but most of them already do something (Fn+F5 changes
the power profile, for example), and voltaire never takes a key away from the
desktop, so a key with a job would do both. More buttons may be offered once
each has been checked on the machine.

The keypress also reaches the desktop. If you would rather voltaire left the
button alone, choose **No button** in Settings or run the daemon with
`--no-button`.

## The detachable keyboard

The keyboard forgets its lighting when it is detached. voltaire notices it come
back and restores your effect within about two seconds.

## Sleep

Before the machine sleeps, voltaire turns the lightbar off. If a custom fan
curve is running, it also hands the fans back to the firmware's automatic
control, which is what lets them stop during sleep, and then holds the suspend
for three seconds: a suspend straight after the hand-back is often woken early
by the embedded controller. Everything is put back on resume.

## Limits

- **No HDR switch** and **no controller vibration**: the panel has no HDR mode
  and there is no built-in controller.
- **One button** opens voltaire ([above](#buttons)).
- **The fan curve is shared**: both fans cool the same chip, and the kernel
  takes one curve for both.
- **Firmware switches** are limited to the two the Z13 has. ASUS firmware on
  other models offers settings such as GPU mode switching, which voltaire
  deliberately never offers: a wrong value there can leave a machine without a
  display.
