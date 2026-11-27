---
title: Commands
description: The complete voltaire CLI reference — every command, flag, and option.
---

# Commands

Every `voltaire` command and flag. For what each feature does and how to use it
from the GUI, see the [User Guide](/guide/gui); this page is the lookup table.

- Commands that change hardware need the access [`voltaire setup`](#setup)
  grants, or root.
- When the [daemon](#daemon) is running, commands go through it; otherwise they
  talk to the hardware directly. A few need the daemon, and say so below.
- Ranges, profile names and lighting zones are your device's own. `--get` and
  `--list` print them, and each [device page](/devices/) lists them.

## Global Flags

These flags apply to every command.

| Flag | Description |
|------|-------------|
| `--device <name\|path>` | Target one lighting zone by name (`keyboard` or `lightbar` on the Z13, `rings` on the ROG Xbox Ally X) or a `/dev/hidrawN` path. Without it, every zone is targeted. |
| `--dry-run` | Show what would be sent or written, without changing anything. Works with every command, including `setup`. |
| `--no-button` | Daemon only: do not watch the hardware button that opens voltaire. |
| `--no-sleep-release` | Daemon only: keep a custom fan curve in force through sleep instead of handing the fans back to the firmware. See [Sleep and resume](/reference/daemon#sleep-and-resume). |

## apply

Apply a lighting effect to every zone, or to one with `--device`.

```
voltaire apply [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--color` | `FF0000` | Primary color: 6-digit hex (`RRGGBB`) or a [named color](#colors) |
| `--color2` | `000000` | Secondary color for `breathe` |
| `--mode` | `static` | Lighting mode (see below) |
| `--speed` | `normal` | Animation speed: `slow`, `normal`, `fast` |
| `--brightness` | `high` | `off`, `low`, `medium`, `high`, or a number from 0 to the device maximum |
| `--list-colors` | | Print every named color with a swatch, and exit |

| Mode | Effect | Uses `--color` | Uses `--color2` | Uses `--speed` |
|------|--------|:-:|:-:|:-:|
| `static` | Solid color | yes | — | — |
| `breathe` | Fade between two colors | yes | yes | yes |
| `cycle` | Cycle through the spectrum | — | — | yes |
| `rainbow` | Rainbow wave across zones | — | — | yes |
| `strobe` | Rapid flash | yes | — | yes |

```sh
voltaire apply --color cyan --brightness high
voltaire apply --mode breathe --color hotpink --color2 blue --speed slow
voltaire apply --mode rainbow --device lightbar
```

**Notes**

- Not every device offers every mode; an unsupported one is refused. See
  [Lighting](/guide/lighting).
- `--color 000000` lets the firmware pick a color; it is not black. To turn the
  lights off, use [`off`](#off) or `--brightness off`.
- Settings sent to a raw `/dev/hidrawN` path are applied but not saved, since
  the number can change between boots. Use the zone name.

## brightness

Set the brightness without changing the effect or color.

```
voltaire brightness <level>
```

`<level>` is `off`, `low`, `medium` (or `med`), `high` (or `max`), or a number
from 0 to the device maximum.

```sh
voltaire brightness medium
voltaire brightness off
```

## off

Turn the lighting off, on every zone or on one with `--device`.

```
voltaire off
```

```sh
voltaire off
voltaire off --device lightbar
```

A zone turned off keeps its effect, so `voltaire brightness <level> --device <zone>`
brings it back.

## profile

Get or set the performance profile, and manage named custom profiles.

```
voltaire profile [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the active profile |
| `--set <profile>` | Switch to a firmware or custom profile |
| `--list` | List the firmware profiles and the saved custom profiles |
| `--create <name>` | Create an empty custom profile, without activating it |
| `--save-as <name>` | Copy the active custom profile under a new name (on a device with a TDP slider and no custom profile active, saves the current TDP setting) |
| `--delete <name>` | Delete a saved custom profile |

```sh
voltaire profile --list
voltaire profile --set performance
voltaire profile --create battery-uv
voltaire profile --set battery-uv
```

**Notes**

- Firmware profiles are the ones your machine's firmware offers. Switching to
  one resets the undervolt and hands the fans and power limits back to the
  firmware; your custom profiles are kept.
- A `fancurve`, `tdp` or `undervolt` `--set` made on a firmware profile creates
  and activates the custom profile `custom`, holding only that one setting.
  Their `--reset` on a firmware profile changes hardware only.
- Selecting a custom profile applies exactly what it holds: a setting it does
  not have goes back to the firmware's own.
- Custom profiles need the daemon. Names are 1–32 characters of `a-z`, `0-9`,
  `-` and `_`, and cannot be `custom` or any kernel firmware profile name
  (`low-power`, `cool`, `quiet`, `balanced`, `balanced-performance`,
  `performance`, `max-power`).
- The active profile, and one an [`autoswitch`](#autoswitch) target names,
  cannot be deleted.
- With the daemon running, a firmware profile is also passed on to
  power-profiles-daemon, if installed. Custom profiles are not.

See [Profiles and power](/guide/profiles-and-power#custom-profiles) for how
profiles work.

## autoswitch

Switch profile automatically when the charger is plugged in or unplugged.
Needs the daemon.

```
voltaire autoswitch [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the configuration and the current power source |
| `--ac <profile>` | Profile to use on AC power |
| `--battery <profile>` | Profile to use on battery |
| `--on` | Enable autoswitch with the configured targets |
| `--off` | Disable autoswitch, keeping the targets |
| `--clear` | Disable autoswitch and clear both targets |

```sh
voltaire autoswitch --ac balanced --battery battery-uv
voltaire autoswitch --ac "" --battery quiet    # leave AC to your desktop
voltaire autoswitch --get
```

**Notes**

- Setting `--ac` or `--battery` enables autoswitch (unless `--off` is also
  given) and changes only the side you name. An empty target leaves that side
  to your desktop's power management.
- Turning it on applies the profile for the current power source at once.
  After that it acts only when the power source changes, so a profile you pick
  by hand stays until the next plug or unplug.

See [Autoswitch](/guide/profiles-and-power#autoswitch).

## batterylimit

Get or set the battery charge limit.

```
voltaire batterylimit [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the charge limit, in percent |
| `--set <percent>` | Set the charge limit, within your device's range |
| `--full-once` | Charge to full once, then put the limit back; `--full-once=false` cancels |

```sh
voltaire batterylimit --get
voltaire batterylimit --set 80
voltaire batterylimit --full-once
```

**Notes**

- An out-of-range value is refused with the range your machine accepts.
  Setting the top of the range removes the limit.
- `--full-once` needs the daemon, and is refused when no limit is set. Setting
  a limit cancels it. See [Full charge once](/guide/battery#full-charge-once).

## bootsound

Get or set the firmware's boot (POST) sound, on ASUS machines that have it.
The same switch as `feature --set boot_sound=…`.

```
voltaire bootsound [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the setting (`0` or `1`) |
| `--set <value>` | `0` = off, `1` = on |

```sh
voltaire bootsound --set 0
```

## paneloverdrive

Get or set the display's panel overdrive, on ASUS machines that have it. The
same switch as `feature --set panel_overdrive=…`.

```
voltaire paneloverdrive [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the setting (`0` or `1`) |
| `--set <value>` | `0` = off, `1` = on |

```sh
voltaire paneloverdrive --set 1
```

## cpuboost

Turn the CPU's boost clocks on or off. With boost off every core stays at its
base clock, which lowers peak power and heat.

```
voltaire cpuboost [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print whether boost is on |
| `--set <value>` | `0` = off, `1` = on |

```sh
voltaire cpuboost --set 0
```

The kernel turns boost back on at every boot; the daemon restores your choice.
Set without the daemon, it lasts until the next reboot. See
[CPU boost](/guide/profiles-and-power#cpu-boost).

## rumble

Get or set the built-in controller's vibration strength, on devices with one.

```
voltaire rumble [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the vibration strength |
| `--set <value>` | From 0 (off) to the controller's maximum |

```sh
voltaire rumble --get
voltaire rumble --set 2
```

The daemon restores the value at startup and after sleep; set without it, the
value is not restored. See [Vibration](/guide/buttons-and-controllers#vibration).

## feature

Get or set the device's firmware switches by id. `bootsound` and
`paneloverdrive` are named shortcuts for two of them.

```
voltaire feature [flags]
```

| Flag | Description |
|------|-------------|
| `--list` | List this device's switches (id, kind, label) |
| `--get <id>` | Print a switch's value |
| `--set <id>=<value>` | Set a switch; on/off switches take `0` or `1` |

```sh
voltaire feature --list
voltaire feature --get boot_sound
voltaire feature --set panel_overdrive=0
```

Some switches only take effect after a restart; the GUI's Settings tab says
when one is pending.

## fancurve

Get, set or reset the custom fan curve. One curve is applied to every fan.

```
voltaire fancurve [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the curve, the fan mode, fan speeds and CPU temperature |
| `--set <curve>` | Set a custom curve: one `temp:speed` pair per curve point |
| `--preset <name>` | Apply one of the device's preset curves |
| `--list-presets` | List the preset curves this device offers |
| `--reset` | Hand every fan back to the firmware's automatic control |
| `--profile <name>` | Store the curve in this custom profile instead of the running one; nothing is applied. Needs the daemon. |

**Curve format:** comma-separated `temp:speed` pairs, exactly as many as your
device's curve has points (`--get` shows how many). Temperatures are in °C and
must increase. Speed is a PWM value (0–255) or a percentage (`40%`), must not
decrease, and the two forms can be mixed.

```sh
voltaire fancurve --get
voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"
voltaire fancurve --set "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"
voltaire fancurve --preset quiet
voltaire fancurve --reset
```

**Notes**

- The mode in `--get` is what counts: `custom` means your curve is running,
  `auto` means it is not, whatever points are listed.
- Any power profile change (a desktop power mode, plugging in the charger, a
  hotkey) drops a custom curve. The daemon puts it back within a couple of
  seconds; without it, run `--set` again.
- While the sustained power limit is above your device's safe maximum, every
  point must clear the fan floor, and `--reset` is refused. Lower the limit
  first with `tdp --reset`. See [Fan floor](/guide/fans#fan-floor).
- Which presets exist depends on the device. See [Presets](/guide/fans#presets).

## tdp

Get, set or reset the power limits (TDP).

```
voltaire tdp [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the current limits, the range each accepts, and the safe maximum |
| `--set <watts>` | Set every limit to this value |
| `--reset` | Go back to the device's default firmware profile and the firmware's own limits and fan control; also resets the undervolt |
| `--pl1 <watts>` | Set the sustained limit (PL1) on its own |
| `--pl2 <watts>` | Set the short boost limit (PL2) on its own |
| `--pl3 <watts>` | Set the fast boost limit (PL3) on its own |
| `--force` | Allow PL1 above the safe maximum, up to the kernel's maximum. The fans are held to the [fan floor](/guide/fans#fan-floor) first. |
| `--profile <name>` | Store the limits in this custom profile instead of applying them. Needs the daemon. |

| Limit | Meaning |
|-------|---------|
| PL1 | Sustained: the power the processor can draw indefinitely |
| PL2 | Short boost: allowed for a few seconds before falling back to PL1 |
| PL3 | Fast boost: allowed for brief spikes |

```sh
voltaire tdp --get
voltaire tdp --set 50
voltaire tdp --set 45 --pl2 55 --pl3 65
voltaire tdp --set 80 --force
voltaire tdp --reset
```

**Notes**

- Your device's ranges are on its [device page](/devices/) and in `--get`. A
  PL1 outside its range is refused; a PL2 or PL3 below its minimum is raised to
  it, with a note.
- Burst limits (PL2, PL3) need no `--force`.
- Run the daemon to keep a custom limit: a power profile change or a fan
  release makes the firmware put its own limits back, and the readback does not
  show it.
- On a firmware profile, `--get` shows that profile's stock limits.
- On a device with a single TDP slider (the OneXPlayer X2 Mini Pro), `--set`
  without `--profile` sets the slider: it is remembered across reboots, and
  leaves a running custom profile.

See [Power limits](/guide/profiles-and-power#power-limits).

## tuning

Clear the fan curve, power limits and undervolt together.

```
voltaire tuning [flags]
```

| Flag | Description |
|---|---|
| `--reset` | Clear all three, and return to the device's default firmware profile with firmware fan control and stock limits |
| `--profile <name>` | Clear them from this custom profile instead; nothing is applied. Needs the daemon. |

```sh
voltaire tuning --reset
voltaire tuning --reset --profile gaming
```

**Notes**

- Use this rather than three separate resets: it lowers power before releasing
  the fans, so the order cannot go wrong.
- Unlike `tdp --reset`, it also forgets the saved settings.
- Without the daemon, the bare form does what `tdp --reset` does and changes no
  saved profile.

See [Reset](/guide/profiles-and-power#reset).

## undervolt

Get or set an all-core CPU undervolt (the Curve Optimizer offset on AMD).
Negative values lower the voltage, for less heat at the same performance.

```
voltaire undervolt [flags]
```

| Flag | Description |
|------|-------------|
| `--get` | Print the offset and whether it is applied now |
| `--set <value>` | Set the offset: 0 or negative, within your device's range |
| `--reset` | Reset the offset to 0; does nothing if no offset is applied |
| `--profile <name>` | Store the offset in this custom profile instead of applying it. Needs the daemon. |

```sh
voltaire undervolt --get
voltaire undervolt --set -20
voltaire undervolt --reset
voltaire --dry-run undervolt --set -20
```

**Notes**

- Needs the `ryzen_smu` kernel module. See
  [Installation](/installation#ryzen-smu).
- There is no readback: `--get` reports what the daemon last applied, or "not
  set" without the daemon.
- The offset is lost on reboot and sleep. The daemon reapplies it.
- On a device with a single TDP slider (the OneXPlayer X2 Mini Pro), an offset
  set without `--profile` is a device setting that stays in force on firmware
  profiles too.

See [Undervolt](/guide/profiles-and-power#undervolt).

## status

Print a one-screen summary: CPU temperature, fans, profile, power source, power
limits, undervolt and battery.

```
voltaire status [flags]
```

| Flag | Description |
|---|---|
| `-w`, `--watch` | Redraw continuously until interrupted |
| `--interval <duration>` | How often to redraw with `--watch` (default `1s`, minimum `100ms`) |

```sh
voltaire status
voltaire status --watch --interval 5s
```

```
CPU:     62°C
Fans:    4200 / 4350 RPM, mode: auto
Profile: balanced
Power:   AC via USB-C
TDP:     52W (PL1) / 71W (PL2) / 70W (PL3)
UV:      available (use 'undervolt --get' for current values)
Battery: 74% (limit: 80%)
```

**Notes**

- Read-only. The daemon is not required; with it running, some readings come
  from it.
- With autoswitch on, the power line adds the profile it would select.
- `--watch` redraws in place without taking over the terminal; redirected
  output gets plain frames.

See [Monitoring](/guide/monitoring).

## list

List the lighting devices voltaire finds and whether each supports Aura.

```
voltaire list
```

Useful for checking that lighting is detected and that `setup` worked. Does not
need the daemon.

## setup

Grant a group the access this machine needs for voltaire to run without root,
and keep it across reboots. Distribution packages run it on install and
upgrade.

```
sudo voltaire setup [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `users` | Group to grant access to |
| `--machine` | | Use this device file id instead of detecting the machine (for the same hardware under another name, or with `--dry-run` to preview another machine) |
| `--perms-only` | | Apply the grants now without writing any files (what the boot unit runs) |
| `--remove` | | Remove what setup wrote and take the access back |
| `--inputplumber-passthrough` | | Where InputPlumber hides this machine's lighting device, write an InputPlumber override that leaves it visible, and restart InputPlumber; `=false` removes it. See [Ally lighting](/guide/buttons-and-controllers#ally-lighting) |

```sh
voltaire --dry-run setup    # preview, no root needed
sudo voltaire setup
sudo voltaire setup --remove
```

**Notes**

- A later run keeps the previous `--group`, `--machine` and
  `--inputplumber-passthrough` choices unless you give them again.
- On a machine no device file describes, setup grants nothing and prints what
  to report.
- Log out and back in (or run `newgrp <group>`) after joining the group.
- What it writes and why: [What setup does](/installation#what-setup-does).
  Files: [Configuration](/reference/configuration#setup-files).

## button

Choose which hardware button opens voltaire: one press opens the quickbar, a
quick second press the full window.

```
voltaire button [flags]
```

| Flag | Description |
|------|-------------|
| `--list` | List the buttons this device offers, marking the one in use |
| `--get` | Print the button that opens voltaire |
| `--set <choice>` | A button id from `--list`, `none`, or `default` |

```sh
voltaire button --list
voltaire button --set none
voltaire button --set default
```

**Notes**

- The choices are fixed by your device; voltaire never learns a key from the
  keyboard.
- `--set` needs the daemon. See
  [Opening voltaire](/guide/buttons-and-controllers#opening-voltaire).

## daemon

Run the voltaire daemon in the foreground. Normally systemd starts it; see
[The Daemon](/reference/daemon).

```
voltaire daemon
```

```sh
voltaire daemon
voltaire --no-button daemon
voltaire --no-sleep-release daemon
```

While the daemon runs, `apply`, `brightness`, `off`, `profile`,
`batterylimit`, `bootsound`, `paneloverdrive`, `feature`, `cpuboost`,
`rumble`, `fancurve`, `tdp`, `undervolt`, `tuning` and `status` go through it;
without it they use the hardware directly. These need the daemon: `autoswitch`,
`button --set`, `batterylimit --full-once`, custom profiles, and every
`--profile` flag.

## Colors

Named colors accepted by `--color` and `--color2`. Any 6-digit hex value
(`RRGGBB`, without `#`) works too. `voltaire apply --list-colors` shows them
as swatches in your terminal.

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
