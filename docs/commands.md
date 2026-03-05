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

Valid profiles: `quiet`, `balanced`, `performance`, `custom`

The `custom` profile is a virtual profile that recalls saved fan curves and TDP
settings from the daemon's state file. It does **not** write to `platform_profile`.
Setting `custom` requires the daemon to be running, and at least one custom fan
curve or TDP value must have been previously set.

Setting a stock profile (`quiet`, `balanced`, `performance`) resets any active
custom fan curves and TDP values back to firmware defaults.

```sh
z13ctl profile --get
z13ctl profile --set performance
z13ctl profile --set balanced
z13ctl profile --set custom
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

## fancurve

Get, set, or reset custom fan curves via the asus-wmi hwmon sysfs interface.
Both physical fans cool the same APU, so the same curve is always applied to
both fans simultaneously. Root or group access required; see [setup](#setup).

```
z13ctl fancurve [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current fan curve, mode, RPM, and APU temperature |
| `--set <curve>` | Set a custom 8-point fan curve (applied to both fans) |
| `--reset` | Reset both fans to firmware auto mode |

**Curve format:** 8 comma-separated `temp:speed` pairs. Speed can be a PWM
value (0–255) or a percentage with a `%` suffix (0–100%). Both formats can be
mixed in the same curve.

```
"48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"   # PWM values
"48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"  # percentages
```

**Validation rules:**

- Exactly 8 points required
- Temperatures must be monotonically increasing (0–120 &deg;C)
- Speed values must be non-decreasing (0–255 PWM or 0–100%)

```sh
# Read current fan curves
z13ctl fancurve --get

# Set a custom fan curve using PWM values (both fans)
z13ctl fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"

# Set a custom fan curve using percentages
z13ctl fancurve --set "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"

# Reset both fans to auto mode
z13ctl fancurve --reset
```

---

## tdp

Get, set, or reset TDP (Thermal Design Power) limits via the asus-nb-wmi PPT
(Package Power Tracking) sysfs attributes. Root or group access required; see
[setup](#setup).

```
z13ctl tdp [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print current PPT values |
| `--set <watts>` | Set all PPT limits to the specified wattage |
| `--reset` | Switch to balanced profile (firmware manages PPT and fan curves) |
| `--pl1 <watts>` | Override PL1/SPL independently |
| `--pl2 <watts>` | Override PL2/sPPT independently |
| `--pl3 <watts>` | Override PL3/fPPT independently |
| `--force` | Allow sustained TDP (PL1) above 75W (up to 93W). Burst limits (PL2/PL3) are allowed up to 93W without `--force`. When PL1 exceeds 75W, fans are set to an 80% minimum curve; custom curves must keep all PWM values at or above 204 (80%). |

**PPT attributes:**

| Attribute | Limit | Description |
|-----------|-------|-------------|
| `ppt_pl1_spl` | PL1 — Sustained | Continuous power budget the APU can draw indefinitely. This is your effective base TDP. |
| `ppt_pl2_sppt` | PL2 — Short-term boost | Power the APU can draw for several seconds before throttling to PL1. |
| `ppt_fppt` | PL3 — Fast boost | Maximum instantaneous power for millisecond-scale spikes. |
| `ppt_apu_sppt` | APU short-term | APU-specific short-term limit; automatically mirrors PL2. |
| `ppt_platform_sppt` | Platform short-term | Platform-level short-term limit; automatically mirrors PL2. |

With `--set`, all three limits default to the same value. Use `--pl1`, `--pl2`,
and `--pl3` to set them independently — a stepped configuration like
`--set 45 --pl2 55 --pl3 65` sustains 45W with short bursts to 55W and
instantaneous peaks to 65W.

Stock profiles (quiet/balanced/performance) let the firmware manage TDP
dynamically — the firmware sets per-profile PPT values automatically on profile
change. Setting a custom TDP switches to the `custom` profile.

!!! note "PPT readback values"
    The values shown by `--get` are the kernel driver's cached values, which may
    not reflect the actual EC limits (especially after a fresh boot or profile
    change). Use `ryzenadj -i` if you need ground-truth PPT readings.

**Safety:**

- Default range: 5–75W
- `--force` extends the range to 5–93W
- When any PPT value exceeds 75W, **both fans are forced to full speed** before
  the TDP values are written. If the fan write fails, TDP is not applied.

```sh
# Read current TDP values
z13ctl tdp --get

# Set all PPT limits to 50W
z13ctl tdp --set 50

# Set with individual PL overrides
z13ctl tdp --set 45 --pl2 55 --pl3 60

# Force high TDP (fans will be set to full speed)
z13ctl tdp --set 85 --force

# Reset to balanced profile (firmware manages PPT)
z13ctl tdp --reset
```

---

## status

Display a summary of all system metrics in a single view: APU temperature, fan
speed and mode, performance profile, TDP power limits, and battery charge level
with charge limit.

```
z13ctl status
```

This command is read-only and takes no flags. All values are read directly from
sysfs.

```sh
z13ctl status
# APU:     62°C
# Fans:    4200 RPM, mode: auto
# Profile: balanced
# TDP:     52W (PL1) / 71W (PL2) / 70W (PL3)
# Battery: 74% (limit: 80%)
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
the ASUS HID devices, performance profile, battery charge limit, firmware
attributes (boot sound, panel overdrive), hwmon fan curve attributes, and
asus-nb-wmi PPT power limit attributes for TDP control.

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
`profile`, `batterylimit`, `bootsound`, `paneloverdrive`, `fancurve`, `tdp`,
`status`)
route through the daemon socket automatically. If the daemon is not running
they fall back to direct hardware or sysfs access.

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
