# Commands

!!! tip "Beginners: consider z13gui instead"
    This page is the complete CLI reference. Driving the more advanced features
    (custom fan curves, TDP, undervolting) from the command line makes sense for
    Linux veterans and scripting, but if you're newer to Linux you'll likely
    have an easier time with **[z13gui](https://github.com/dahui/z13gui)**, the
    touch-friendly graphical frontend that exposes all of these commands as
    point-and-tap controls.

## Global Flags

These flags apply to every command.

| Flag | Description |
|------|-------------|
| `--device <name\|path>` | Target a single device: `keyboard`, `lightbar`, or a `/dev/hidrawN` path. Without this flag all matching devices are targeted. |
| `--dry-run` | Preview what would be sent or written without making any changes. Works for all commands including `setup`. |
| `--no-button` | Disable the Armoury Crate button watcher (daemon only). Use when another tool needs exclusive access to the device, or to keep the keypress from reaching z13ctl at all. |

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

!!! note "`--color 000000` means \"pick a color\", not black"
    An all-zero primary color sets the Aura protocol's random-color flag, so the
    firmware chooses a color itself. This matches the reference implementation
    the protocol was derived from. To turn lighting off, use
    [`z13ctl off`](#off) or `--brightness off` — not a black color.

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

Get or set the system performance profile, and manage named custom profiles.
Root or group access required; see [setup](#setup).

```
z13ctl profile [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the active profile |
| `--set <profile>` | Set the profile (firmware or custom) |
| `--list` | List saved custom profiles |
| `--create <name>` | Create an empty custom profile (does not activate it) |
| `--save-as <name>` | Copy the active custom profile under a new name |
| `--delete <name>` | Delete a saved custom profile |

### Firmware profiles

`quiet`, `balanced`, and `performance` are written to `platform_profile` via
asus-wmi. These three names are **reserved**: a custom profile can never take
one, so `--set balanced` always reaches the firmware profile.

Setting a firmware profile resets fan curves to firmware auto mode, resets the
CPU undervolt to stock, and writes that profile's measured stock PPT values back
to hardware. The firmware does *not* re-apply per-profile power limits on its
own, so z13ctl restores them explicitly. Your custom profiles are untouched and
can be selected again at any time. [`tdp --reset`](#tdp) behaves the same way,
since it also lands on a firmware profile.

### Custom profiles

A custom profile is z13ctl's own: a named set of fan curve, TDP, and Curve
Optimizer settings that z13ctl applies itself. It is **never** written to
`platform_profile`, so profile ownership stays with your desktop.

`custom` is the profile created automatically by the first `fancurve --set`,
`tdp --set`, or `undervolt --set` made while a firmware profile is active — the
behaviour z13ctl has always had. `--create` makes more.

Setting a fan curve, TDP, or undervolt edits the profile you are *running*, and
the change takes effect and persists immediately. There is no save step;
`--save-as` copies the active profile under a new name rather than committing
pending edits.

`--profile <name>` on [`fancurve`](#fancurve), [`tdp`](#tdp), and
[`undervolt`](#undervolt) edits a profile you are **not** running: the setting is
stored and nothing is applied to hardware. That is how you build the profile
[`autoswitch`](#autoswitch) selects on battery without applying it first.

Selecting a custom profile puts the machine into the state that profile
describes. A subsystem it does not set is cleared rather than left alone — a
profile with no fan curve releases the fans to firmware auto, one with no
undervolt resets it, and one with no TDP hands the power limits back to the
firmware profile underneath. That is what makes switching between two custom
profiles predictable.

Custom profiles live in daemon state, so every custom-profile operation requires
the daemon.

```sh
z13ctl profile --get
z13ctl profile --set performance

# build a profile without applying it
z13ctl profile --create battery-uv
z13ctl tdp --set 35 --profile battery-uv
z13ctl undervolt --set -25 --profile battery-uv

z13ctl profile --set battery-uv    # now apply it
z13ctl profile --list
z13ctl profile --save-as gaming    # copy the active profile
```

Profile names are lowercase, 1–32 characters of `a-z`, `0-9`, `-`, and `_`, and
may not be `quiet`, `balanced`, `performance`, or `custom`.

!!! note
    When the daemon is running, setting a firmware profile also updates
    `power-profiles-daemon` (if installed) to the equivalent PPD profile. Custom
    profiles deliberately do not.

!!! warning "Deleting a profile"
    z13ctl refuses to delete the active profile or one referenced by
    `autoswitch`. Switch away, or reconfigure autoswitch, then delete.

---

## autoswitch

Apply a different profile automatically when the charger is plugged or
unplugged. Requires the daemon, which watches the power source.

```
z13ctl autoswitch [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the configuration and the current power source |
| `--ac <profile>` | Profile to apply on AC power |
| `--battery <profile>` | Profile to apply on battery |
| `--on` | Enable autoswitch with the configured targets |
| `--off` | Disable autoswitch, keeping the configured targets |
| `--clear` | Disable autoswitch and clear both targets |

Each side takes any profile name, firmware or custom. Setting `--ac` or
`--battery` enables autoswitch unless `--off` is given, and changes only the side
you name — `--battery quiet` leaves the AC target as it was. An empty target
leaves that side alone, which hands it back to your desktop's power management:

```sh
z13ctl autoswitch --ac "" --battery battery-uv
```

The full recipe, matching the most common request — a firmware profile on AC and
a custom profile with an undervolt on battery:

```sh
z13ctl profile --create battery-uv
z13ctl tdp --set 35 --profile battery-uv
z13ctl undervolt --set -25 --profile battery-uv
z13ctl autoswitch --ac balanced --battery battery-uv
z13ctl autoswitch --get
```

Autoswitch acts **only when the power source actually changes**. A profile you
pick by hand therefore stays in force until the next plug or unplug, and z13ctl
does not contest the profile with `power-profiles-daemon` in between. The
transition is applied about two seconds after the event: that settle window lets
the desktop's own transition write land first, and stops a loose USB-C connector
from driving a profile change per bounce.

The daemon also resolves the right profile for the current power source at
startup, so a machine that was on AC when the daemon stopped and is on battery
when it starts lands on the battery profile directly.

!!! note "GNOME's Automatic Power Saver"
    That setting triggers on *low battery*, not on unplugging, so it can still
    move a firmware profile after autoswitch has acted. z13ctl deliberately
    yields between transitions rather than fighting for the profile. If you want
    your desktop to own one side entirely, give that side an empty target.

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
| `--profile <name>` | Store the setting in this custom profile instead of applying it to the active one. Requires the daemon. |

The **mode** in `--get` output is the one to read. `custom` means the kernel is
honouring your curve; `auto` means it is not, regardless of which points are
listed underneath — the driver keeps the curve data after disabling it.

```
$ z13ctl fancurve --get
Fans: 4400 RPM, mode: custom, APU: 43°C   # <- your curve is live
$ z13ctl fancurve --get
Fans: 1200 RPM, mode: auto, APU: 48°C     # <- it is not; see the warning below
```

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

!!! warning "A power profile change wipes your custom curve"
    The kernel's `asus-wmi` driver disables custom fan curves on every
    `platform_profile` write, silently. GNOME power modes and
    `power-profiles-daemon` do that on each AC/battery transition, so does Fn+F5,
    and so does `asusctl`. Run the [daemon](daemon.md) and it puts your curve back
    within a couple of seconds; without it, re-run `--set` after any profile
    change. Since 1.2.2 the command errors out instead of reporting success when
    the kernel refuses the curve.

    To confirm it for yourself, read the driver's own flag — `1` is custom,
    `2` is firmware auto:

    ```sh
    curve=$(grep -l asus_custom_fan_curve /sys/class/hwmon/hwmon*/name | xargs dirname)
    cat $curve/pwm1_enable
    ```

!!! warning "Fan control is restricted above 75 W sustained TDP"
    While sustained TDP (PL1) is above 75 W, every curve point must be at least
    127 PWM (50%), and `--reset` is refused — firmware auto mode has no floor,
    and dropping to it would remove the cooling the power limit depends on.
    Lower the limit first with [`z13ctl tdp --reset`](#tdp), which restores the
    balanced profile before releasing the fans.

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
| `--reset` | Switch to balanced profile, reset fan curves to auto and the undervolt to stock, and restore balanced's stock PPT |
| `--pl1 <watts>` | Override PL1/SPL independently |
| `--pl2 <watts>` | Override PL2/sPPT independently |
| `--pl3 <watts>` | Override PL3/fPPT independently |
| `--force` | Allow sustained TDP (PL1) above 75W (up to 93W). Burst limits (PL2/PL3) are allowed up to 93W without `--force`. When PL1 exceeds 75W, fans are set to a 50% minimum curve; custom curves must keep all PWM values at or above 127 (50%). |
| `--profile <name>` | Store the setting in this custom profile instead of applying it to the active one. Requires the daemon. |

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

Setting a custom TDP switches to the `custom` profile. Switching back to a stock
profile writes that profile's measured stock PPT values to hardware — the
firmware does *not* re-apply them on a `platform_profile` change, so z13ctl
restores them explicitly. The saved custom values are kept, so `custom` stays
re-selectable.

!!! note "PPT readback values"
    The values shown by `--get` are the kernel driver's cached values. After a
    fresh boot they hold a stale 5W default until something writes them; z13ctl
    substitutes the measured per-profile table in that case. Use `ryzenadj -i`
    if you need ground-truth PPT readings.

**Safety:**

- Default range: 5–75W for the sustained limit (PL1); `--force` extends it to
  5–93W. Burst limits (PL2/PL3) may go to 93W without `--force`, since short
  bursts are thermally safe.
- When the **sustained** limit exceeds 75W, both fans are held to a minimum of
  127 PWM (50%) before the TDP values are written. If that fan write fails — or
  the kernel accepts it and then drops the curve — the TDP is not applied at all.
  Burst limits above 75W do not trigger this on their own.
- The floor is only the bottom of the curve. Above 50 °C it climbs steadily and
  reaches 100% at 80 °C, which is the range a machine actually sustaining more
  than 75 W spends its time in:

    | Temp | 30 °C | 40 °C | 50 °C | 60 °C | 65 °C | 70 °C | 75 °C | 80 °C |
    |------|-------|-------|-------|-------|-------|-------|-------|-------|
    | PWM  | 127   | 127   | 140   | 165   | 190   | 215   | 235   | 255   |

    A custom curve may replace it as long as every point stays at or above 127.
    The floor was 204 (80%) through v1.2.1 — loud enough that users were turning
    the feature off rather than living with it, which protects nobody.

!!! danger "Run the daemon when sustaining above 75 W"
    The kernel releases custom fan curves on every `platform_profile` write, and
    the power limit survives it — so a GNOME power mode change or an AC/battery
    transition can leave the machine drawing >75 W sustained with the fans back on
    the firmware's ordinary curve. The [daemon](daemon.md) watches for that and
    restores the floor within a couple of seconds. Without it, that state persists
    until you re-apply the curve yourself.

```sh
# Read current TDP values
z13ctl tdp --get

# Set all PPT limits to 50W
z13ctl tdp --set 50

# Set with individual PL overrides
z13ctl tdp --set 45 --pl2 55 --pl3 60

# Force high sustained TDP (fans are held to a 50% floor first)
z13ctl tdp --set 85 --force

# Reset to balanced profile (restores balanced's stock PPT and clears the undervolt)
z13ctl tdp --reset
```

---

## undervolt

Get or set CPU Curve Optimizer (CO) offsets via the `ryzen_smu` kernel
module. Negative values reduce voltage (undervolt), improving efficiency and
thermals without reducing performance. Root or group access required; see
[setup](#setup).

```
z13ctl undervolt [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print current CO offset (from daemon state) |
| `--set <value>` | Set all-core CPU CO offset (0 to -40) |
| `--reset` | Reset CPU CO to stock (0) |
| `--profile <name>` | Store the setting in this custom profile instead of applying it to the active one. Requires the daemon. |

CO values have no sysfs readback — `--get` returns the last-applied values from
daemon state. If a stock profile is active (quiet/balanced/performance), the
output indicates that the saved offsets are not currently applied. If the daemon
is not running, reports "not set".

CO is volatile: values reset on reboot and sleep/resume. The daemon reapplies
them automatically on startup and resume when the custom profile is active.

**Safety limits (matching G-Helper defaults):**

| Parameter | Range |
|-----------|-------|
| CPU CO | 0 to -40 |

**Requires:** `ryzen_smu` kernel module. Install via:

- **Arch/CachyOS:** `ryzen_smu-dkms-git` (AUR)
- **Other distros:** build from [amkillam/ryzen_smu](https://github.com/amkillam/ryzen_smu) source

!!! warning "Strix Halo requires the amkillam fork"
    The original `leogx9r/ryzen_smu` does not support Strix Halo. Use the
    [amkillam/ryzen_smu](https://github.com/amkillam/ryzen_smu) fork instead.

If the module is not installed, undervolt commands return a helpful error.

```sh
# Read current CO value
z13ctl undervolt --get

# Set CPU CO to -20
z13ctl undervolt --set -20

# Reset to stock voltage
z13ctl undervolt --reset

# Preview without applying
z13ctl --dry-run undervolt --set -20
```

---

## status

Display a summary of all system metrics in a single view: APU temperature, fan
speed and mode, performance profile, TDP power limits, undervolt status, and
battery charge level with charge limit.

```
z13ctl status
```

This command is read-only and takes no flags. Values are read directly from
sysfs, with two exceptions: undervolt has no sysfs readback, so the line reports
availability rather than the active offset; and the TDP line asks the daemon
which profile is active, because a custom TDP of exactly 5 W is otherwise
indistinguishable from the kernel's stale 5 W boot cache.

```sh
z13ctl status
# APU:     62°C
# Fans:    4200 RPM, mode: auto
# Profile: balanced
# TDP:     52W (PL1) / 71W (PL2) / 70W (PL3)
# UV:      available (use 'undervolt --get' for current values)
# Battery: 74% (limit: 80%)
```

The undervolt line comes from the daemon, which tests Curve Optimizer support
once at startup. Without a daemon, `status` can only confirm that the module is
loaded and says so:

```
# UV:      ryzen_smu loaded (start the daemon to confirm Curve Optimizer support)
```

`status` deliberately does not run that support test itself — it works by writing
a zero offset, which is the same command as [`undervolt --reset`](#undervolt), so
a `status` that ran it would clear an active undervolt every time.

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
attributes (boot sound, panel overdrive), hwmon fan curve attributes,
asus-nb-wmi PPT power limit attributes for TDP control, and ryzen_smu sysfs
files for undervolting (if the module is loaded).

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
`undervolt`, `status`)
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
