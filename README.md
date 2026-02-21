# z13ctl

RGB lighting control for the 2025 ASUS ROG Flow Z13 via Linux hidraw.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

## Table of Contents

- [Background](#background)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [apply](#apply)
  - [brightness](#brightness)
  - [list](#list)
  - [off](#off)
  - [setup](#setup)
- [Global Flags](#global-flags)
- [Colors](#colors)
- [Contributing](#contributing)

## Background

Linux support for RGB control on the 2025 ASUS ROG Flow Z13 is frustratingly
sparse. The existing options all have significant problems:

- **[OpenRGB](https://openrgb.org/)** does not support the 2025 Z13 at all.
- **[asusctl](https://gitlab.com/asus-linux/asusctl)** does not work with this
  model.
- **[rogauracore](https://github.com/wroberts/rogauracore)** only controls the
  keyboard backlight; the edge lightbar is not supported.
- **[HHD](https://github.com/hhd-dev/hhd)** works but is broken on CachyOS
  without the Bazzite kernel. Using the Bazzite kernel instead of CachyOS's
  performance-tuned kernels (deckify or bore) causes a meaningful reduction in
  gaming performance -- around 10-20% depending on the workload. Getting HHD to
  work on a non-Bazzite kernel requires out-of-tree kernel patches that turn
  out to be useful only for HHD itself, as demonstrated by this tool working
  without them.

`z13ctl` implements the Aura HID protocol directly against the Linux `hidraw`
interface, with no kernel patches and no external daemons required. The
protocol was reverse-engineered from
[g-helper](https://github.com/seerge/g-helper) (MIT license), which documents
it in `app/USB/AsusHid.cs` and `app/USB/Aura.cs`. A detailed technical
description of the protocol is available in [PROTOCOL.md](PROTOCOL.md).

## Requirements

- Linux kernel with `hidraw` support
- 2025 ASUS ROG Flow Z13 (USB IDs `0b05:18c6` and `0b05:1a30`)
- Read/write access to `/dev/hidraw*` -- either run as root or use
  `z13ctl setup` to install a udev rule that grants access to a group

## Installation

**Pre-built binaries** are available on the
[Releases](../../releases) page. Download the archive for your
architecture (`amd64` or `arm64`), extract it, and place the binary
somewhere on your `PATH`.

**From source:**

```sh
git clone https://github.com/YOUR_ORG/z13ctl
cd z13ctl
make build
sudo cp z13ctl /usr/local/bin/
```

## Quick Start

```sh
# One-time setup: install udev rules so non-root users can control the device
sudo z13ctl setup

# Set the keyboard and lightbar to solid cyan at full brightness
z13ctl apply --color cyan --brightness high

# Breathing effect in red
z13ctl apply --mode breathe --color red --speed slow

# Turn off all lighting
z13ctl off
```

## Commands

### apply

Apply a lighting effect to the keyboard and lightbar.

```
z13ctl apply [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--color` | `FF0000` | Primary color: 6-digit hex (`RRGGBB`) or a [named color](#colors) |
| `--color2` | `000000` | Secondary color for `breathe` mode: 6-digit hex or name |
| `--mode` | `static` | Lighting mode (see table below) |
| `--speed` | `normal` | Animation speed: `slow`, `normal`, `fast` |
| `--brightness` | `high` | Brightness level: `off`, `low`, `medium`, `high` |
| `--list-colors` | | Print all named colors and exit |

**Modes:**

| Mode | Description |
|------|-------------|
| `static` | Solid color |
| `breathe` | Fade in and out. Uses `--color2` as the second color if set. |
| `cycle` | Cycle through the full color spectrum |
| `rainbow` | Rainbow wave across zones |
| `star` | Random pixels blink on and off |
| `rain` | Color drips down the keyboard |
| `strobe` | Rapid flash |
| `comet` | Trailing comet effect |
| `flash` | Single-color flash burst |

**Examples:**

```sh
z13ctl apply --color cyan --brightness high
z13ctl apply --color 00FF88 --mode rainbow --speed slow
z13ctl apply --mode breathe --color hotpink --color2 blue
z13ctl apply --list-colors
```

### brightness

Set the brightness level without changing the current lighting mode or color.

```
z13ctl brightness <level>
```

`<level>` is one of: `off`, `low`, `medium`, `high`

```sh
z13ctl brightness medium
z13ctl brightness off
```

### list

List all matching hidraw devices and show whether each one has Aura support.

```
z13ctl list
```

### off

Turn off all lighting zones.

```
z13ctl off
```

### setup

Install a udev rule that grants a group read/write access to the ASUS HID
devices, then reload and trigger udev. This command must be run as root.

```
sudo z13ctl setup [--group <group>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `users` | Group to grant access to the devices |

The rule is written to `/etc/udev/rules.d/99-z13ctl.rules`. After running
`setup`, log out and back in (or run `newgrp <group>`) for the group
membership to take effect in your current session.

## Global Flags

These flags apply to every command.

| Flag | Description |
|------|-------------|
| `--device <name\|path>` | Target a single device: `keyboard`, `lightbar`, or a `/dev/hidrawN` path. Without this flag, commands are sent to all matching devices. |
| `--dry-run` | Print the HID packets that would be sent without writing to any device. |

## Colors

Named colors accepted by `--color` and `--color2`. Any 6-digit hex value
(`RRGGBB`, without `#`) is also accepted.

| Name | Hex | Name | Hex |
|------|-----|------|-----|
| `red` | `FF0000` | `blue` | `0000FF` |
| `crimson` | `DC143C` | `navy` | `000080` |
| `orangered` | `FF4500` | `indigo` | `4B0082` |
| `coral` | `FF7F50` | `blueviolet` | `8A2BE2` |
| `orange` | `FF8000` | `purple` | `800080` |
| `gold` | `FFD700` | `magenta` | `FF00FF` |
| `yellow` | `FFFF00` | `deeppink` | `FF1493` |
| `chartreuse` | `7FFF00` | `hotpink` | `FF69B4` |
| `green` | `00FF00` | `violet` | `EE82EE` |
| `springgreen` | `00FF7F` | `turquoise` | `40E0D0` |
| `aquamarine` | `7FFFD4` | `brown` | `A52A2A` |
| `teal` | `008080` | `white` | `FFFFFF` |
| `cyan` | `00FFFF` | `deepskyblue` | `00BFFF` |
| `dodgerblue` | `1E90FF` | `royalblue` | `4169E1` |

## Contributing

Contributions are welcome. Please open an issue before starting work on a
significant change so the approach can be discussed first.

**Setup:**

```sh
git clone https://github.com/YOUR_ORG/z13ctl
cd z13ctl
go mod download
```

**Before submitting a pull request:**

```sh
make test    # run all tests
make lint    # run golangci-lint
```

Tests do not require hardware. The `internal/aura` and `internal/cli`
packages are fully unit-testable. Code that interacts with `/dev/hidraw*`
is intentionally isolated in `internal/hid`.

Pull requests should include tests for any new behavior and must pass
both `make test` and `make lint` without errors.
