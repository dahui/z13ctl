# Commands

## Global Flags

These flags apply to every command.

| Flag | Description |
|------|-------------|
| `--device <name\|path>` | Target a single device: `keyboard`, `lightbar`, or a `/dev/hidrawN` path. Without this flag all matching devices are targeted. |
| `--dry-run` | Preview what would be sent or written without making any changes. Works for all commands including `setup`. |
| `--no-button` | Disable the Armoury Crate button watcher (daemon only). Use when another tool needs exclusive access to the button device. |

---

## apply

Apply a lighting effect to the keyboard backlight, the edge lightbar, or both.

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
| `--list-colors` | | Print all named colors with swatches and exit |

**Modes:**

| Mode | Description | `--color` | `--color2` | `--speed` |
|------|-------------|:---------:|:----------:|:---------:|
| `static` | Solid color | yes | — | — |
| `breathe` | Fade between two colors | yes | yes | yes |
| `cycle` | Auto-cycle full spectrum | — | — | yes |
| `rainbow` | Rainbow wave across zones | — | — | yes |
| `strobe` | Rapid flash | yes | — | yes |

All modes accept `--brightness`.

```sh
z13ctl apply --color cyan --brightness high
z13ctl apply --mode rainbow --speed slow
z13ctl apply --mode breathe --color hotpink --color2 blue --speed slow
z13ctl apply --list-colors
```

---

## brightness

Set the brightness level without changing the current lighting mode or color.

```
z13ctl brightness <level>
```

`<level>` is one of: `off`, `low`, `medium`, `high`

```sh
z13ctl brightness medium
z13ctl brightness off
```

---

## off

Turn off all lighting zones (or a specific zone with `--device`).

```
z13ctl off
```

```sh
z13ctl off
z13ctl off --device lightbar
```

---

## profile

Get or set the system performance profile via the asus-wmi `platform_profile`
sysfs interface. Root or group access required; see [setup](#setup).

```
z13ctl profile [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the active performance profile |
| `--set <profile>` | Set the performance profile |

Valid profiles: `quiet`, `balanced`, `performance`

```sh
z13ctl profile --get
z13ctl profile --set performance
z13ctl profile --set balanced
```

!!! note
    When the daemon is running, setting a profile also updates
    `power-profiles-daemon` (if installed) to the equivalent PPD profile.

---

## batterylimit

Get or set the battery charge limit via the Linux ACPI `power_supply` sysfs
interface. Root or group access required; see [setup](#setup).

```
z13ctl batterylimit [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current battery charge limit (percentage) |
| `--set <percent>` | Set the battery charge limit (40–100) |

Writing `100` removes any limit (charges to full).

```sh
z13ctl batterylimit --get
z13ctl batterylimit --set 80
```

---

## bootsound

Get or set the POST boot sound via the `asus-armoury` firmware-attributes sysfs
interface. Root or group access required; see [setup](#setup).

```
z13ctl bootsound [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current boot sound setting (`0` or `1`) |
| `--set <value>` | Set boot sound: `0` = off, `1` = on |

```sh
z13ctl bootsound --get
z13ctl bootsound --set 0
```

---

## paneloverdrive

Get or set display panel refresh overdrive via the `asus-armoury`
firmware-attributes sysfs interface. Root or group access required; see
[setup](#setup).

```
z13ctl paneloverdrive [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current panel overdrive setting (`0` or `1`) |
| `--set <value>` | Set panel overdrive: `0` = off, `1` = on |

```sh
z13ctl paneloverdrive --get
z13ctl paneloverdrive --set 1
```

---

## list

List all matching hidraw devices and show whether each has Aura support.

```
z13ctl list
```

Useful for diagnosing missing devices or verifying that `setup` worked. Does
not require the daemon to be running.

---

## setup

Install udev rules and a boot service granting a group read/write access to
the ASUS HID devices, performance profile, battery charge limit, and
firmware attributes (boot sound, panel overdrive).

```
sudo z13ctl setup [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `users` | Group to grant device access to |

Use `--dry-run` to preview exactly what would be written — no root required:

```sh
z13ctl --dry-run setup    # preview (no root needed)
sudo z13ctl setup         # apply
```

After running setup, log out and back in (or run `newgrp <group>`) for the
group membership to take effect in your current session.

For a detailed explanation of what `setup` installs and why the battery limit
requires a separate systemd service, see [Installation](installation.md#what-setup-does).

---

## daemon

Start the z13ctl daemon. Normally started automatically via the systemd socket
unit — see [Daemon](daemon.md). You can also start it directly for testing.

```
z13ctl daemon
```

```sh
z13ctl daemon               # with Armoury Crate button watcher
z13ctl --no-button daemon   # without button watcher
```

When the daemon is running, all other commands (`apply`, `brightness`, `off`,
`profile`, `batterylimit`, `bootsound`, `paneloverdrive`) route through the
daemon socket automatically. If the daemon is not running they fall back to
direct hardware or sysfs access.

---

## Colors

Named colors accepted by `--color` and `--color2`. Any 6-digit hex value
(`RRGGBB`, without `#`) is also accepted.

Run `z13ctl apply --list-colors` to see ANSI true-color swatches in your terminal.

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
