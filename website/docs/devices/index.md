---
title: Supported devices
description: Which machines voltaire supports, and which features work on each one.
---

# Supported devices

voltaire recognizes a supported machine by its firmware's model name, so
there is nothing to select or configure. Each machine offers only what its
hardware and kernel support. A feature it lacks is simply absent from the GUI
and refused by the CLI with a reason, rather than shown as a control that
does nothing.

| Device | Detected as |
|---|---|
| [ASUS ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | every GZ302 variant (GZ302EA and others) |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | the X2 Mini Pro (AMD Strix Halo) |

## Compatibility

"No" links to the explanation on the device's page. Most gaps are in the
kernel, not in voltaire, and close when a kernel with the support ships.

| Feature | ROG Flow Z13 (2025) | OneXPlayer X2 Mini Pro |
|---|---|---|
| **Power** | | |
| TDP (power limits) | Yes: three limits, 28–80 W sustained | Yes: one slider, 10–85 W ([needs ryzen_smu](/devices/onexplayer-x2-mini-pro#tdp-needs-ryzen-smu)) |
| Firmware power profiles | Yes: quiet, balanced, performance | [Only without ryzen_smu](/devices/onexplayer-x2-mini-pro#tdp-needs-ryzen-smu) |
| Saved custom profiles | Yes: fan curve, TDP and undervolt | Yes: TDP and undervolt |
| Switch profile on AC / battery | Yes | Yes |
| CPU boost on/off | Yes | Yes |
| CPU undervolt (Curve Optimizer) | Yes ([needs ryzen_smu](/installation#optional-ryzen_smu-kernel-module)) | Yes: [a setting like the TDP slider](/devices/onexplayer-x2-mini-pro#undervolt) (needs ryzen_smu) |
| **Cooling** | | |
| Fan speed readout | Yes: two fans | Yes |
| Custom fan curves | Yes, with presets | [No](/devices/onexplayer-x2-mini-pro#fan-control) |
| **Lighting** | | |
| RGB lighting | Yes: keyboard and lightbar | [No: joystick rings not supported yet](/devices/onexplayer-x2-mini-pro#joystick-lighting) |
| **Battery** | | |
| Charge level, rate, time left, health | Yes | Yes |
| Charge limit | Yes: 40–100 % | [No](/devices/onexplayer-x2-mini-pro#charge-limit) |
| **Display and sound** | | |
| Screen brightness | Yes | Yes |
| Volume | Yes | Yes |
| Refresh rate | Yes ([KDE Plasma](#refresh-rate)) | Yes ([KDE Plasma](#refresh-rate)) |
| HDR on/off | [No](#hdr) | [No](#hdr) |
| **Buttons and controller** | | |
| Hardware button opens voltaire | Armoury Crate button | Keyboard, OneXPlayer or Home button |
| Controller vibration strength | No built-in controller | Yes: off to 5 |
| **Firmware settings** | | |
| Firmware switches | Yes: POST boot sound, panel overdrive | [None](/devices/onexplayer-x2-mini-pro#firmware-settings) |
| **Telemetry** | | |
| Temperatures, power, load, clocks, memory, network | Yes | Yes |
| NPU power and load | Yes | [No, while suspend needs `amd_iommu=off`](/devices/onexplayer-x2-mini-pro#npu-telemetry) |

Each device's page has the details: what each feature needs installed, the
numbers measured on that machine, and what does not work yet.

## On every device

### Refresh rate

The refresh-rate control, and the option to switch rate with the power source,
work on **KDE Plasma** only: a video mode belongs to the compositor, and KDE's
is the one voltaire can drive (through `kscreen-doctor`). Other desktops get no
refresh-rate control. In Steam Gaming Mode it is hidden, because Steam's Quick
Access menu sets the refresh rate there and would overwrite voltaire's choice.

Brightness and volume work on every desktop and in Gaming Mode.

### HDR

voltaire offers no HDR switch on any device. Only the compositor can turn HDR
on or off, and every compositor does it differently (KDE, GNOME, Hyprland and
gamescope would each need their own code, and some have no runtime switch at
all). Use your desktop's display settings. The Z13's panel is not
HDR-capable in any case; the X2 Mini Pro's is.

## A machine that is not listed

On any other machine voltaire does not start: the daemon and the CLI stop with
`no device file matches this machine`, followed by the machine's firmware
vendor and model strings. Support for a new machine is a device file
describing its hardware ([Device Files](/dev/device-files)), plus a driver for
any interface voltaire does not speak yet ([Writing a Driver](/dev/writing-a-driver)).
To ask for a machine, open an issue on
[GitHub](https://github.com/dahui/voltaire/issues) with those two strings.
