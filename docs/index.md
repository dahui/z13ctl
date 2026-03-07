# z13ctl

RGB lighting, performance profiles, battery limit, fan curves, TDP control,
CPU undervolting, and display settings for the **2025 ASUS ROG Flow Z13**
on Linux.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](https://github.com/dahui/z13ctl/blob/main/LICENSE)

---

## What z13ctl does

- **RGB lighting** — set color, mode, speed, and brightness on the keyboard backlight
  and edge lightbar, independently or together
- **Performance profiles** — switch between `quiet`, `balanced`, and `performance`
  via the asus-wmi sysfs interface
- **Battery charge limit** — cap charging at any percentage (40–100) to extend
  long-term battery health
- **Fan curves** — set custom 8-point fan curves via the asus-wmi hwmon interface
  (both fans cool the same APU and share one curve)
- **TDP control** — set CPU/GPU power limits (5–93W) via asus-nb-wmi PPT attributes,
  with automatic fan safety above 75W
- **CPU undervolting** — reduce voltage via AMD Curve Optimizer for lower
  temperatures and power draw without reducing performance (requires `ryzen_smu`
  kernel module)
- **Boot sound** — enable or disable the POST beep via the asus-armoury firmware
  attributes interface
- **Panel overdrive** — toggle display overdrive for reduced motion blur

All features work without root after a one-time `setup` step, and all persist across
reboots when the daemon is running.

---

## Background

Linux support for RGB control on the 2025 ASUS ROG Flow Z13 is limited.
OpenRGB does not support this model. asusctl does not work with it.
rogauracore only controls the keyboard backlight. HHD works on Bazzite kernels
but requires out-of-tree patches that reduce performance on CachyOS.

z13ctl implements the Aura HID protocol directly against the Linux `hidraw`
interface — no kernel patches, no external daemons. The protocol was
reverse-engineered from [g-helper](https://github.com/seerge/g-helper) (MIT).
A full protocol specification is available in the [Protocol Reference](protocol.md).

---

## Requirements

- Linux kernel with `hidraw` support
- 2025 ASUS ROG Flow Z13 (USB IDs `0b05:18c6` and `0b05:1a30`)
- Read/write access to the relevant `hidraw` and sysfs files — either run as root,
  or use [`z13ctl setup`](commands.md#setup) to install udev rules granting access
  to a group

---

## Next steps

Head to the [Installation](installation.md) guide to get z13ctl on your system,
then follow the [Quick Start](getting-started.md) to configure lighting, fan
curves, and performance profiles. For the full CLI reference, see
[Commands](commands.md).
