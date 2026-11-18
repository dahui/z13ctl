---
title: Overview
description: TDP, fan curves, RGB lighting, and system control for the ASUS ROG Flow Z13 (2025) and OneXPlayer X2 Mini Pro on Linux.
---

# Voltaire

TDP control, performance profiles, fan curves, RGB lighting, battery limit,
CPU undervolting, and display and controller settings on Linux, for the ASUS
ROG Flow Z13 (2025) and the OneXPlayer X2 Mini Pro. Formerly z13ctl and
z13gui.

New here? Start with [Installation](/installation). Coming from z13ctl or
z13gui? See [Migrating from z13ctl](/migrating-from-z13ctl).

## What Voltaire does

Not every machine has every feature: [Supported devices](/devices/) lists
what works on each one, and why something does not. The examples below are
the Z13's.

- **RGB lighting** — Color, mode, speed, and brightness on the keyboard backlight and edge lightbar — independently or together.
- **Performance profiles** — Switch between your firmware's profiles (`quiet`, `balanced` and `performance` on the Z13), or build named custom profiles with their own fan curve, TDP, and undervolt.
- **Fan curves** — Custom fan curves via the asus-wmi hwmon interface, defended against the kernel silently dropping them.
- **TDP control** — CPU/GPU power limits (28–80 W sustained on the GZ302EA) through the kernel's asus-armoury attributes, kept in force by the daemon, with an automatic fan floor above the safe sustained limit (on the GZ302EA, 50% PWM above 75 W, rising to 100% at 80 °C).
- **CPU undervolting** — Lower voltage via AMD Curve Optimizer for better temperatures and power draw at the same performance (requires the `ryzen_smu` kernel module).
- **Touch-first GUI** — `voltaire-gui` is a GTK4 overlay drawer covering every feature — on the desktop and in Steam Gaming Mode, with full gamepad navigation.

Also: battery charge limit, POST boot sound, panel overdrive, and any
other firmware toggle the device data describes.

All features work without root after a one-time `setup` step, and all persist
across reboots when the daemon is running. The daemon re-applies keyboard
lighting when the detachable keyboard is reattached, and puts your fan curve
and power limits back when the kernel or firmware discards them — which happens
on every system power profile change (see [Daemon](/reference/daemon#custom-fan-curve-reconciliation)).

::: tip New to Linux? Use the GUI
Most users — especially those newer to Linux — should install
**voltaire-gui**, the touch-friendly GTK4 overlay that controls every feature
(lighting, fan curves, TDP, undervolt, profiles, battery limit) through the
daemon — no command line required. The raw `voltaire` CLI is great for
scripting and advanced tuning, but if you're not a Linux veteran you'll have a
far smoother experience with the GUI. Both install together — see
[Installation](/installation).
:::

## Background

Linux support for RGB control on the 2025 ASUS ROG Flow Z13 is limited.
OpenRGB does not support this model. asusctl does not work with it.
rogauracore only controls the keyboard backlight. HHD works on Bazzite kernels
but requires out-of-tree patches that reduce performance on CachyOS.

Voltaire implements the Aura HID protocol directly against the Linux `hidraw`
interface — no kernel patches, no external daemons. The protocol was
reverse-engineered from [g-helper](https://github.com/seerge/g-helper) (MIT).
A full protocol specification is available in the
[Protocol Reference](/reference/protocol).

Voltaire is the continuation of **z13ctl** and **z13gui**, merged into one
project at 2.0. Existing installs upgrade in place — state, config, and the
daemon socket all carry over. See
[Migrating from z13ctl](/migrating-from-z13ctl).

## Requirements

- Linux kernel with `hidraw` support
- A [supported device](/devices/): the ASUS ROG Flow Z13 (2025) or the
  OneXPlayer X2 Mini Pro
- Read/write access to the relevant `hidraw` and sysfs files — either run as
  root, or use [`voltaire setup`](/reference/commands#setup) to
  install udev rules granting access to a group

## Next steps

Head to the [Installation](/installation) guide to get Voltaire on
your system, then follow the [Quick Start](/quick-start) to configure
lighting, fan curves, and performance profiles. For the full CLI reference, see
[Commands](/reference/commands).
