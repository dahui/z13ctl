---
title: Commands
description: The complete voltaire CLI reference — every command, flag, and option.
---

:::tip[Beginners: consider the GUI instead]
This page is the complete CLI reference. Driving the more advanced features
(custom fan curves, TDP, undervolting) from the command line makes sense for
Linux veterans and scripting, but if you're newer to Linux you'll likely have
an easier time with **[voltaire-gui](/voltaire/gui/)**, the touch-friendly
graphical frontend that exposes all of these commands as point-and-tap
controls.
:::

## Global Flags

These flags apply to every command.

| Flag | Description |
|------|-------------|
| `--device <name\|path>` | Target a single device: `keyboard`, `lightbar`, or a `/dev/hidrawN` path. Without this flag all matching devices are targeted. |
| `--dry-run` | Preview what would be sent or written without making any changes. Works for all commands including `setup`. |
| `--no-button` | Disable the Armoury Crate button watcher (daemon only). Use when another tool needs exclusive access to the device, or to keep the keypress from reaching voltaire at all. |
| `--no-sleep-release` | Keep a custom fan curve in force through sleep instead of handing the fans back to the firmware (daemon only). See [sleep/resume recovery](/voltaire/reference/daemon/#on-sleep--the-fans-are-handed-back-to-the-firmware). |

## apply

Apply a lighting effect to the keyboard backlight, the edge lightbar, or both.

```
voltaire apply [flags]
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

:::note[`--color 000000` means "pick a color", not black]
An all-zero primary color sets the Aura protocol's random-color flag, so the
firmware chooses a color itself. This matches the reference implementation the
protocol was derived from. To turn lighting off, use [`voltaire off`](#off) or
`--brightness off` — not a black color.
:::

```sh
voltaire apply --color cyan --brightness high
voltaire apply --mode rainbow --speed slow
voltaire apply --mode breathe --color hotpink --color2 blue --speed slow
voltaire apply --list-colors
```

## brightness

Set the brightness level without changing the current lighting mode or color.

```
voltaire brightness <level>
```

`<level>` is one of: `off`, `low`, `medium`, `high`

```sh
voltaire brightness medium
voltaire brightness off
```

## off

Turn off all lighting zones (or a specific zone with `--device`).

```
voltaire off
```

```sh
voltaire off
voltaire off --device lightbar
```

## profile

Get or set the system performance profile, and manage named custom profiles.
Root or group access required; see [setup](#setup).

```
voltaire profile [flags]
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
CPU undervolt to stock, and writes that profile's measured stock PPT values
back to hardware. The firmware does *not* re-apply per-profile power limits on
its own, so voltaire restores them explicitly. Your custom profiles are
untouched and can be selected again at any time. [`tdp --reset`](#tdp) behaves
the same way, since it also lands on a firmware profile.

### Custom profiles

A custom profile is voltaire's own: a named set of fan curve, TDP, and Curve
Optimizer settings that voltaire applies itself. It is **never** written to
`platform_profile`, so profile ownership stays with your desktop.

`custom` is the profile created automatically by a `fancurve --set`,
`tdp --set`, or `undervolt --set` made while a firmware profile is active.
`--create` makes more.

Such a bare edit gives `custom` **only the setting you just set** — a TDP set
from `balanced` leaves the fans on firmware auto, and a fan curve set there
leaves the power limits at stock. Anything `custom` stored before is replaced;
save settings you want to keep under a name of their own (`profile --save-as`).
Editing `custom` while it is the active profile, or explicitly via
`--profile custom`, edits the stored bundle in place as before. (Through 2.0
development builds the bare edit re-applied everything `custom` stored, so
setting a TDP could silently bring back an old fan curve — or an old 93W power
limit — you had long since stopped using.)

:::note[`--reset` while a firmware profile is active does not touch your profiles]
Only `--set` creates and activates `custom`. `fancurve --reset`, `tdp --reset`
and `undervolt --reset` run while a firmware profile is selected affect
**hardware only**: the fans go to firmware auto, the power limits to that
profile's stock values, the Curve Optimizer to zero, and every saved custom
profile is left exactly as it was.

Through v1.3.0 (as z13ctl) they resolved to `custom` and committed it
cleared — so `tdp --reset` on `balanced` silently deleted the fan curve and
power limits saved under `custom`, and switched the reported profile to the
one it had just emptied. Use `profile --set custom` to recall those settings.
:::

Setting a fan curve, TDP, or undervolt edits the profile you are *running*,
and the change takes effect and persists immediately. There is no save step;
`--save-as` copies the active profile under a new name rather than committing
pending edits.

`--profile <name>` on [`fancurve`](#fancurve), [`tdp`](#tdp), and
[`undervolt`](#undervolt) edits a profile you are **not** running: the setting
is stored and nothing is applied to hardware. That is how you build the
profile [`autoswitch`](#autoswitch) selects on battery without applying it
first.

Selecting a custom profile puts the machine into the state that profile
describes. A subsystem it does not set is cleared rather than left alone — a
profile with no fan curve releases the fans to firmware auto, one with no
undervolt resets it, and one with no TDP hands the power limits back to the
firmware profile underneath. That is what makes switching between two custom
profiles predictable.

Custom profiles live in daemon state, so every custom-profile operation
requires the daemon.

```sh
voltaire profile --get
voltaire profile --set performance

# build a profile without applying it
voltaire profile --create battery-uv
voltaire tdp --set 35 --profile battery-uv
voltaire undervolt --set -25 --profile battery-uv

voltaire profile --set battery-uv    # now apply it
voltaire profile --list
voltaire profile --save-as gaming    # copy the active profile
```

Profile names are lowercase, 1–32 characters of `a-z`, `0-9`, `-`, and `_`,
and may not be `quiet`, `balanced`, `performance`, or `custom`.

:::note
When the daemon is running, setting a firmware profile also updates
`power-profiles-daemon` (if installed) to the equivalent PPD profile. Custom
profiles deliberately do not.
:::

:::caution[Deleting a profile]
Voltaire refuses to delete the active profile or one referenced by
`autoswitch`. Switch away, or reconfigure autoswitch, then delete.
:::

## autoswitch

Apply a different profile automatically when the charger is plugged or
unplugged. Requires the daemon, which watches the power source.

```
voltaire autoswitch [flags]
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
`--battery` enables autoswitch unless `--off` is given, and changes only the
side you name — `--battery quiet` leaves the AC target as it was. An empty
target leaves that side alone, which hands it back to your desktop's power
management:

```sh
voltaire autoswitch --ac "" --battery battery-uv
```

The full recipe, matching the most common request — a firmware profile on AC
and a custom profile with an undervolt on battery:

```sh
voltaire profile --create battery-uv
voltaire tdp --set 35 --profile battery-uv
voltaire undervolt --set -25 --profile battery-uv
voltaire autoswitch --ac balanced --battery battery-uv
voltaire autoswitch --get
```

Turning autoswitch on is a **one-shot**: it applies the profile for the source
you are already on, so enabling it takes effect immediately instead of waiting
for a transition. Turning it off, or changing the targets while it is already
on, applies nothing.

After that, autoswitch acts **only when the power source actually changes**. A
profile you pick by hand therefore stays in force until the next plug or
unplug, and voltaire does not contest the profile with `power-profiles-daemon`
in between. The transition is applied about two seconds after the event: that
settle window lets the desktop's own transition write land first, and stops a
loose USB-C connector from driving a profile change per bounce.

The daemon also resolves the right profile for the current power source at
startup, so a machine that was on AC when the daemon stopped and is on battery
when it starts lands on the battery profile directly.

:::note[GNOME's Automatic Power Saver]
That setting triggers on *low battery*, not on unplugging, so it can still
move a firmware profile after autoswitch has acted. Voltaire deliberately
yields between transitions rather than fighting for the profile. If you want
your desktop to own one side entirely, give that side an empty target.
:::

## batterylimit

Get or set the battery charge limit via the Linux ACPI `power_supply` sysfs
interface. Root or group access required; see [setup](#setup).

```
voltaire batterylimit [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current battery charge limit (percentage) |
| `--set <percent>` | Set the battery charge limit (40–100) |

Writing `100` removes any limit (charges to full).

```sh
voltaire batterylimit --get
voltaire batterylimit --set 80
```

## bootsound

Get or set the POST boot sound via the `asus-armoury` firmware-attributes
sysfs interface. Root or group access required; see [setup](#setup).

```
voltaire bootsound [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current boot sound setting (`0` or `1`) |
| `--set <value>` | Set boot sound: `0` = off, `1` = on |

```sh
voltaire bootsound --get
voltaire bootsound --set 0
```

## paneloverdrive

Get or set display panel refresh overdrive via the `asus-armoury`
firmware-attributes sysfs interface. Root or group access required; see
[setup](#setup).

```
voltaire paneloverdrive [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current panel overdrive setting (`0` or `1`) |
| `--set <value>` | Set panel overdrive: `0` = off, `1` = on |

```sh
voltaire paneloverdrive --get
voltaire paneloverdrive --set 1
```

## cpuboost

Get or set the CPU's opportunistic boost clocks, through cpufreq's global boost
switch (`/sys/devices/system/cpu/cpufreq/boost`). Root or group access
required; see [setup](#setup).

Disabling boost caps every core at its base clock — on the Z13's Ryzen AI Max+
395 that is 3.0 GHz instead of 5.19 GHz — which lowers peak power and heat at
the cost of peak single-thread performance. One write moves every cpufreq
policy at once.

Unlike the firmware toggles it resembles, this is a **kernel runtime setting**:
the kernel re-enables boost on every boot. The daemon records your choice and
restores it at startup, so setting it with the daemon stopped changes the
hardware now but will not survive a reboot — the command says so when that
happens.

```
voltaire cpuboost [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print whether boost is enabled |
| `--set <value>` | `0` = disabled, `1` = enabled |

```sh
voltaire cpuboost --get
voltaire cpuboost --set 0
```

The same switch is on the full window's Dashboard, in the **Power** card.

## feature

Get or set the device's firmware toggles (BIOS switches) by their id — the
generic form of `bootsound` and `paneloverdrive`. The set of toggles comes
from the device data this build carries; `--list` shows what this machine
offers, and the daemon serves the same list to GUIs in its `device-get`
document, so a toggle added in device data is reachable without new commands
anywhere.

```
voltaire feature [flags]
```

| Flag | Description |
|------|-------------|
| `--list` | List the device's firmware toggles (id, kind, label) |
| `--get <id>` | Print a toggle's current value |
| `--set <id>=<value>` | Set a toggle; boolean toggles take `0` or `1` |

```sh
voltaire feature --list
voltaire feature --get boot_sound
voltaire feature --set panel_overdrive=0
```

## fancurve

Get, set, or reset custom fan curves via the asus-wmi hwmon sysfs interface.
Both physical fans cool the same APU, so the same curve is always applied to
both fans simultaneously. Root or group access required; see [setup](#setup).

```
voltaire fancurve [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current fan curve, mode, RPM, and APU temperature |
| `--set <curve>` | Set a custom 8-point fan curve (applied to both fans) |
| `--reset` | Reset both fans to firmware auto mode |
| `--profile <name>` | Store the setting in this custom profile instead of applying it to the active one. Requires the daemon. |

The **mode** in `--get` output is the one to read. `custom` means the kernel
is honouring your curve; `auto` means it is not, regardless of which points
are listed underneath — the driver keeps the curve data after disabling it.

```
$ voltaire fancurve --get
Fans: 4400 RPM, mode: custom, APU: 43°C   # <- your curve is live
$ voltaire fancurve --get
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
- Temperatures must be monotonically increasing (0–120 °C)
- Speed values must be non-decreasing (0–255 PWM or 0–100%)

:::caution[A power profile change wipes your custom curve]
The kernel's `asus-wmi` driver disables custom fan curves on every
`platform_profile` write, silently. GNOME power modes and
`power-profiles-daemon` do that on each AC/battery transition, so does Fn+F5,
and so does `asusctl`. Run the [daemon](/voltaire/reference/daemon/) and it
puts your curve back within a couple of seconds; without it, re-run `--set`
after any profile change. Since 1.2.2 the command errors out instead of
reporting success when the kernel refuses the curve.

To confirm it for yourself, read the driver's own flag — `1` is custom, `2` is
firmware auto:

```sh
curve=$(grep -l asus_custom_fan_curve /sys/class/hwmon/hwmon*/name | xargs dirname)
cat $curve/pwm1_enable
```
:::

:::caution[Fan control is restricted above 75 W sustained TDP]
While sustained TDP (PL1) is above 75 W, `fancurve --set` requires every point
to clear the built-in high-TDP curve — an up-front refusal so you can see why,
rather than storing a curve that would be altered on the way to the hardware.
If you raise the limit *after* setting a curve, the points below 127 are
quietly raised to it instead and the rest are left alone (see [`tdp`](#tdp));
the curve you saved is kept intact for when the limit comes back down. "Below
the floor" means below the built-in curve's value *at that temperature*, not
below its 127 PWM bottom.

`--reset` is refused outright — firmware auto mode has no floor at all, and
dropping to it would remove the cooling the power limit depends on. Lower the
limit first with [`voltaire tdp --reset`](#tdp), which restores the balanced
profile before releasing the fans.
:::

```sh
# Read current fan curves
voltaire fancurve --get

# Set a custom fan curve using PWM values (both fans)
voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"

# Set a custom fan curve using percentages
voltaire fancurve --set "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"

# Reset both fans to auto mode
voltaire fancurve --reset
```

## tdp

Get, set, or reset TDP (Thermal Design Power) limits via the asus-nb-wmi PPT
(Package Power Tracking) sysfs attributes. Root or group access required; see
[setup](#setup).

```
voltaire tdp [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print current PPT values |
| `--set <watts>` | Set all PPT limits to the specified wattage |
| `--reset` | Switch to balanced profile, reset fan curves to auto and the undervolt to stock, and restore balanced's stock PPT |
| `--pl1 <watts>` | Override PL1/SPL independently |
| `--pl2 <watts>` | Override PL2/sPPT independently |
| `--pl3 <watts>` | Override PL3/fPPT independently |
| `--force` | Allow sustained TDP (PL1) above 75W (up to 93W). Burst limits (PL2/PL3) are allowed up to 93W without `--force`. When PL1 exceeds 75W, each fan curve point is raised to the built-in high-TDP curve's value if it falls below it; points above it are left exactly as you set them. |
| `--profile <name>` | Store the setting in this custom profile instead of applying it to the active one. Requires the daemon. |

**PPT attributes:**

| Attribute | Limit | Description |
|-----------|-------|-------------|
| `ppt_pl1_spl` | PL1 — Sustained | Continuous power budget the APU can draw indefinitely. This is your effective base TDP. |
| `ppt_pl2_sppt` | PL2 — Short-term boost | Power the APU can draw for several seconds before throttling to PL1. |
| `ppt_fppt` | PL3 — Fast boost | Maximum instantaneous power for millisecond-scale spikes. |
| `ppt_apu_sppt` | APU short-term | APU-specific short-term limit; automatically mirrors PL2. |
| `ppt_platform_sppt` | Platform short-term | Platform-level short-term limit; automatically mirrors PL2. |

With `--set`, all three limits default to the same value. Use `--pl1`,
`--pl2`, and `--pl3` to set them independently — a stepped configuration like
`--set 45 --pl2 55 --pl3 65` sustains 45W with short bursts to 55W and
instantaneous peaks to 65W.

Setting a custom TDP switches to the `custom` profile. Switching back to a
stock profile writes that profile's measured stock PPT values to hardware —
the firmware does *not* re-apply them on a `platform_profile` change, so
voltaire restores them explicitly. The saved custom values are kept, so
`custom` stays re-selectable.

:::note[PPT readback values]
The values shown by `--get` are the kernel driver's cached values. After a
fresh boot they hold a stale 5W default until something writes them; voltaire
substitutes the measured per-profile table in that case. Use `ryzenadj -i` if
you need ground-truth PPT readings.
:::

**Safety:**

- Default range: 5–75W for the sustained limit (PL1); `--force` extends it to
  5–93W. Burst limits (PL2/PL3) may go to 93W without `--force`, since short
  bursts are thermally safe.
- When the **sustained** limit exceeds 75W, both fans are held to a minimum of
  127 PWM (50%) before the TDP values are written. If that fan write fails —
  or the kernel accepts it and then drops the curve — the TDP is not applied
  at all. Burst limits above 75W do not trigger this on their own.
- **The floor is a per-point minimum, not a replacement curve.** Your curve is
  raised point by point to whichever is higher — your value, or the built-in
  curve's value **at that point's own temperature**, interpolated between the
  rows of the table below. Nothing is ever lowered, and a point you set above
  the built-in curve is applied exactly as you drew it. A curve at 100%
  everywhere stays at 100%.

    The comparison is by temperature and not by position in the list, so a
    curve whose points sit at unusual temperatures cannot slip under the ramp:
    a point at `70:130` is measured against the 215 the floor requires at
    70 °C, not against whichever built-in point happens to be seventh.

    z13ctl through v1.3.0 replaced the *whole* curve whenever the sustained
    limit exceeded 75 W, which is why a custom curve appeared to "reset to
    stock" after every sleep/resume and every `tdp --set`.

- The built-in curve is the floor, and the whole of it matters — not just its
  127 PWM bottom. It is written whole only when the profile has no curve of
  its own, but its rising section is what your curve is measured against at
  higher temperatures:

    | Temp | 30 °C | 40 °C | 50 °C | 60 °C | 65 °C | 70 °C | 75 °C | 80 °C |
    |------|-------|-------|-------|-------|-------|-------|-------|-------|
    | PWM  | 127   | 127   | 140   | 165   | 190   | 215   | 235   | 255   |

    Below 30 °C the floor holds at 127 rather than tapering off, and above
    80 °C it stays at 255. Between rows it interpolates, so a point at 55 °C
    is measured against roughly 152.

    So a curve flat at 50% clears the *bottom* of the floor everywhere but is
    still raised above 40 °C, because a machine genuinely sustaining more than
    75 W lives well past 60 °C — and the ramp, not the 127 minimum, is what
    protects the APU there. A curve like `35:80%,40:99%,50:100%,…` is above
    the built-in curve at every position and is left completely alone.

    The 127 bottom was 204 (80%) through v1.2.1 — loud enough that users were
    turning the feature off rather than living with it, which protects nobody.

- `--reset` cannot drop to firmware auto while a high limit is in force, since
  firmware auto has no floor at all.

:::danger[Run the daemon when sustaining above 75 W]
The kernel releases custom fan curves on every `platform_profile` write, and
the power limit survives it — so a GNOME power mode change or an AC/battery
transition can leave the machine drawing >75 W sustained with the fans back on
the firmware's ordinary curve. The [daemon](/voltaire/reference/daemon/)
watches for that and restores the floor within a couple of seconds. Without
it, that state persists until you re-apply the curve yourself.
:::

```sh
# Read current TDP values
voltaire tdp --get

# Set all PPT limits to 50W
voltaire tdp --set 50

# Set with individual PL overrides
voltaire tdp --set 45 --pl2 55 --pl3 60

# Force high sustained TDP (fans are held to a 50% floor first)
voltaire tdp --set 85 --force

# Reset to balanced profile (restores balanced's stock PPT and clears the undervolt)
voltaire tdp --reset
```

## tuning

Manage the tuning overrides — fan curve, power limits and Curve Optimizer
offset — as one group.

**Flags:**

| Flag | Description |
|---|---|
| `--reset` | Clear the fan curve, power limits and Curve Optimizer offset |
| `--profile <name>` | Clear them from a profile you are NOT running (stores only) |

`--reset` returns the machine to the `balanced` profile with firmware fan
control and stock power limits, and forgets the saved fan curve, limits and
offset in the profile it edits.

**This is not the same as running the three resets by hand.** Each of
`fancurve --reset`, `tdp --reset` and `undervolt --reset` has to lower power
before releasing the fans; issuing them in the wrong order leaves the machine at
a high sustained limit with no fan floor. `tuning --reset` is a single daemon
operation, so the ordering cannot be got wrong.

It also differs from `tdp --reset` in what it *forgets*: `tdp --reset` keeps the
saved Curve Optimizer offset so it can be recalled with `profile --set custom`,
while `tuning --reset` is the command that says to discard all of it.

```bash
# Clear every tuning override and return to stock behaviour
voltaire tuning --reset

# Clear them from a profile you are not running
voltaire tuning --reset --profile gaming
```

Without the daemon running, `--profile` is refused (only the daemon owns the
saved profiles) and the bare form performs the same hardware sequence as
`tdp --reset`.

## undervolt

Get or set CPU Curve Optimizer (CO) offsets via the `ryzen_smu` kernel module.
Negative values reduce voltage (undervolt), improving efficiency and thermals
without reducing performance. Root or group access required; see
[setup](#setup).

```
voltaire undervolt [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print current CO offset (from daemon state) |
| `--set <value>` | Set all-core CPU CO offset (0 to -40) |
| `--reset` | Reset CPU CO to stock (0) |
| `--profile <name>` | Store the setting in this custom profile instead of applying it to the active one. Requires the daemon. |

CO values have no sysfs readback — `--get` returns the last-applied values
from daemon state. If a stock profile is active (quiet/balanced/performance),
the output indicates that the saved offsets are not currently applied. If the
daemon is not running, reports "not set".

CO is volatile: values reset on reboot and sleep/resume. The daemon reapplies
them automatically on startup and resume when the custom profile is active.

**Safety limits (matching G-Helper defaults):**

| Parameter | Range |
|-----------|-------|
| CPU CO | 0 to -40 |

**Requires:** `ryzen_smu` kernel module. Install via:

- **Arch/CachyOS:** `ryzen_smu-dkms-git` (AUR)
- **Other distros:** build from
  [amkillam/ryzen_smu](https://github.com/amkillam/ryzen_smu) source

:::caution[Strix Halo requires the amkillam fork]
The original `leogx9r/ryzen_smu` does not support Strix Halo. Use the
[amkillam/ryzen_smu](https://github.com/amkillam/ryzen_smu) fork instead.
:::

If the module is not installed, undervolt commands return a helpful error.

```sh
# Read current CO value
voltaire undervolt --get

# Set CPU CO to -20
voltaire undervolt --set -20

# Reset to stock voltage
voltaire undervolt --reset

# Preview without applying
voltaire --dry-run undervolt --set -20
```

## status

Display a summary of all system metrics in a single view: APU temperature, fan
speed and mode, performance profile, TDP power limits, undervolt status, and
battery charge level with charge limit.

```
voltaire status
```

This command is read-only. Values are read directly from sysfs, with two
exceptions: undervolt has no sysfs readback, so the line reports availability
rather than the active offset; and the TDP line asks the daemon which profile is
active, because a custom TDP of exactly 5 W is otherwise indistinguishable from
the kernel's stale 5 W boot cache.

**Flags:**

| Flag | Description |
|---|---|
| `-w`, `--watch` | Redraw continuously until interrupted |
| `--interval <duration>` | How often to redraw with `--watch` (default `1s`) |

```sh
voltaire status
# APU:     62°C
# Fans:    4200 RPM, mode: auto
# Profile: balanced
# TDP:     52W (PL1) / 71W (PL2) / 70W (PL3)
# UV:      available (use 'undervolt --get' for current values)
# Battery: 74% (limit: 80%)
```

The undervolt line comes from the daemon, which tests Curve Optimizer support
once at startup. Without a daemon, `status` can only confirm that the module
is loaded and says so:

```
# UV:      ryzen_smu loaded (start the daemon to confirm Curve Optimizer support)
```

`status` deliberately does not run that support test itself — it works by
writing a zero offset, which is the same command as
[`undervolt --reset`](#undervolt), so a `status` that ran it would clear an
active undervolt every time.

### Watching

`--watch` redraws the report in place until you interrupt it:

```sh
voltaire status --watch
voltaire status --watch --interval 5s
```

The default interval is one second, matching the daemon's sampler — redrawing
faster than that shows the same numbers again. Intervals under 100 ms are
refused for the same reason.

It is deliberately not a full-screen interface: no alternate screen, no raw
mode, no key handling. It walks the cursor back over the previous frame and
clears from there down, so your scrollback is untouched and `Ctrl-C` leaves the
last reading on screen. Piped or redirected output gets plain frames with no
escape sequences, so `voltaire status --watch > log.txt` stays readable.

The daemon is not required — `status` reads sysfs directly either way.

For a richer terminal view, [z13-panel](https://github.com/ayixiayi/z13-panel)
is a third-party TUI built on the same daemon socket.

## list

List all matching hidraw devices and show whether each has Aura support.

```
voltaire list
```

Useful for diagnosing missing devices or verifying that `setup` worked. Does
not require the daemon to be running.

## setup

Install udev rules and a boot service granting a group read/write access to
the ASUS HID devices, performance profile, battery charge limit, firmware
attributes (boot sound, panel overdrive), hwmon fan curve attributes,
asus-nb-wmi PPT power limit attributes for TDP control, and ryzen_smu sysfs
files for undervolting (if the module is loaded).

```
sudo voltaire setup [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `users` | Group to grant device access to |
| `--perms-only` | | Reapply sysfs permissions without writing udev rules or the service |

On a machine that previously ran `z13ctl setup`, the old rules file and
permissions unit are cleaned up automatically — see
[Migrating from z13ctl](/voltaire/migrating-from-z13ctl/).

Use `--dry-run` to preview exactly what would be written — no root required:

```sh
voltaire --dry-run setup    # preview (no root needed)
sudo voltaire setup         # apply
```

After running setup, log out and back in (or run `newgrp <group>`) for the
group membership to take effect in your current session.

For a detailed explanation of what `setup` installs and why the battery limit
requires a separate systemd service, see
[Installation](/voltaire/installation/#what-setup-does).

## daemon

Start the voltaire daemon. Normally started automatically via the systemd
socket unit — see [Daemon](/voltaire/reference/daemon/). You can also start it
directly for testing.

```
voltaire daemon
```

```sh
voltaire daemon               # with Armoury Crate button watcher
voltaire --no-button daemon   # without button watcher
```

When the daemon is running, all other commands (`apply`, `brightness`, `off`,
`profile`, `batterylimit`, `bootsound`, `paneloverdrive`, `fancurve`, `tdp`,
`undervolt`, `status`) route through the daemon socket automatically. If the
daemon is not running they fall back to direct hardware or sysfs access.

## Colors

Named colors accepted by `--color` and `--color2`. Any 6-digit hex value
(`RRGGBB`, without `#`) is also accepted.

Run `voltaire apply --list-colors` to see ANSI true-color swatches in your
terminal.

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
