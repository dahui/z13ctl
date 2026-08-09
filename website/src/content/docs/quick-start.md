---
title: Quick Start
description: First steps with the voltaire CLI — lighting, profiles, fan curves, TDP, undervolting, and custom profiles.
---

These examples assume you have [installed voltaire](/voltaire/installation/)
and run `sudo voltaire setup`. The daemon does not need to be running —
commands fall back to direct hardware access automatically. See
[Daemon](/voltaire/reference/daemon/) for why you probably want the daemon
running anyway.

:::tip[Prefer not to use the command line? Use the GUI]
Everything below can be done from a graphical interface instead. If you're new
to Linux or simply prefer not to type commands, use **voltaire-gui** — the
touch-friendly overlay that drives all of these features (lighting, fan
curves, TDP, undervolt, profiles) through the daemon. See the
[GUI overview](/voltaire/gui/). The CLI examples here remain useful for
scripting and advanced tuning.
:::

## Lighting

```sh
# Solid cyan at full brightness (keyboard + lightbar)
voltaire apply --color cyan --brightness high

# Breathing red, slow pulse
voltaire apply --mode breathe --color red --speed slow

# Breathing between two colors
voltaire apply --mode breathe --color hotpink --color2 blue

# Rainbow wave across both zones
voltaire apply --mode rainbow --speed normal

# Spectrum cycle, fast
voltaire apply --mode cycle --speed fast

# Strobe white
voltaire apply --mode strobe --color white

# Turn off all lighting
voltaire off
```

## Brightness

```sh
# Adjust brightness without changing mode or color
voltaire brightness high
voltaire brightness medium
voltaire brightness low
voltaire brightness off
```

## Performance profile

```sh
# Check current profile
voltaire profile --get

# Switch profiles
voltaire profile --set performance
voltaire profile --set balanced
voltaire profile --set quiet
```

## Battery charge limit

```sh
# Check current limit
voltaire batterylimit --get

# Cap charging at 80% (recommended for mostly-plugged-in use)
voltaire batterylimit --set 80

# Remove the limit (charge to 100%)
voltaire batterylimit --set 100
```

## Fan curves

Both physical fans cool the same APU, so the same curve is always applied to
both fans simultaneously.

The kernel discards custom fan curves whenever the system power profile
changes — which GNOME power modes and `power-profiles-daemon` do on every
AC/battery transition. Run the [daemon](/voltaire/reference/daemon/) and it
re-applies your curve automatically; otherwise re-run `--set` after any profile
change.

```sh
# Check current fan curves
voltaire fancurve --get

# Set a custom fan curve using PWM values (8 temp:speed pairs, both fans)
voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"

# Or use percentages (0–100%)
voltaire fancurve --set "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"

# Reset both fans to firmware auto mode
voltaire fancurve --reset
```

## TDP control

TDP (Thermal Design Power) controls how much power the APU can draw. There are
three limits that form a hierarchy: PL1 is the sustained (continuous) limit,
PL2 allows short-term bursts above PL1 for several seconds, and PL3 allows
instantaneous spikes for milliseconds. Setting all three to the same value
gives a flat power cap; setting PL2 and PL3 higher allows bursty workloads to
temporarily exceed PL1.

Setting a custom TDP switches to the `custom` profile. Switching back to a
stock profile restores that profile's stock PPT values to hardware, while
keeping the custom values saved so `custom` stays re-selectable.

Sustaining more than 75 W holds both fans to a 50% PWM floor that rises to
100% at 80 °C. Run the [daemon](/voltaire/reference/daemon/) if you use that: a
system power profile change releases the floor in the kernel while the power
limit stays in force, and the daemon is what puts it back.

```sh
# Check current TDP/PPT values
voltaire tdp --get

# Set all PPT limits to 50W
voltaire tdp --set 50

# Set with individual PL overrides
voltaire tdp --set 45 --pl2 55 --pl3 60

# Force high TDP (above 75W, fans set to 50% minimum)
voltaire tdp --set 85 --force

# Reset to balanced profile (restores balanced's stock PPT)
voltaire tdp --reset
```

## Undervolting (Curve Optimizer)

Undervolting reduces CPU voltage via AMD Curve Optimizer (CO), lowering
temperatures and power draw without reducing performance. Requires the
`ryzen_smu` kernel module (optional — see
[Installation](/voltaire/installation/#optional-ryzen_smu-kernel-module-for-undervolting)).

CO values are volatile — they reset on reboot and sleep. The daemon reapplies
them automatically on startup and resume when the custom profile is active.

```sh
# Check current CO value
voltaire undervolt --get

# Set CPU CO to -20
voltaire undervolt --set -20

# Reset to stock voltage
voltaire undervolt --reset
```

Safety limit (matching G-Helper defaults): CPU 0 to -40.

## Custom profiles

A custom profile is a named set of fan curve, TDP, and undervolt settings that
voltaire applies itself. The first custom setting you make while a firmware
profile is active creates one called `custom`:

```sh
voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"
voltaire tdp --set 50

# Recall it later
voltaire profile --set custom

# Switch back to a firmware profile (resets fan curves and TDP)
voltaire profile --set balanced
```

Editing a setting changes the profile you are running and persists
immediately — there is no save step. Give a setup its own name with
`--create`, or copy the one you are running with `--save-as`:

```sh
voltaire profile --save-as quiet-work
voltaire profile --list
```

Requires the daemon, which is what stores and recalls custom profiles.

## A different profile on AC and battery

Build the profile you want on battery first. `--profile` stores a setting in a
profile you are *not* running, so nothing is applied to the machine while you
set it up:

```sh
voltaire profile --create battery-uv
voltaire tdp --set 35 --profile battery-uv
voltaire undervolt --set -25 --profile battery-uv
```

Then hand the pair to the daemon:

```sh
voltaire autoswitch --ac balanced --battery battery-uv
voltaire autoswitch --get
```

That command also turns autoswitch on, which applies the profile for the
source you are already on right away — plugged in, you land on `balanced`
immediately.

From then on it acts only on a real transition: unplug the charger and the
battery profile takes effect a couple of seconds later; plug it back in and
you are on `balanced` again. A profile you pick by hand in between stays until
the next plug or unplug.

To hand one side back to your desktop's power management, leave its target
empty:

```sh
voltaire autoswitch --ac "" --battery battery-uv
```

## Per-device control

Use `--device` to target only the keyboard or lightbar:

```sh
# Keyboard to red, lightbar to blue
voltaire apply --color red --device keyboard
voltaire apply --color blue --device lightbar

# Turn off just the lightbar
voltaire off --device lightbar
```

## Preview without applying changes

`--dry-run` shows exactly what packets or writes would be sent, without
touching any hardware:

```sh
voltaire --dry-run apply --mode rainbow --speed fast
voltaire --dry-run setup
```

## See all named colors

```sh
voltaire apply --list-colors
```

This prints a live swatch table in your terminal. Any 6-digit hex value
(`RRGGBB`, without `#`) is also accepted wherever a color name is.

## Next steps

- [Commands](/voltaire/reference/commands/) — every flag and option for every
  command
- [Daemon](/voltaire/reference/daemon/) — set up the daemon for state
  persistence, boot restore, and sleep/resume recovery
- [The GUI](/voltaire/gui/) — graphical interface for every feature
  (touch-friendly GTK4 overlay)
