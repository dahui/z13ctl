---
title: Quick Start
description: Your first ten minutes with voltaire, in the GUI and on the command line.
---

# Quick Start

This page assumes you have [installed voltaire](/installation) and logged
back in, so the daemon and the GUI are running. Names and ranges below differ
between machines; your [device's page](/devices/) has yours.

## Open the GUI

Press your device's hardware button (the Armoury Crate button on ASUS
machines, the Keyboard button on the OneXPlayer), or open **Voltaire** from
your app menu. The drawer slides in from the edge of the screen. Press the
button again, tap outside the drawer or press **Escape** to close it.

Press the button twice quickly for the full window, with the Dashboard,
Profiles and Settings tabs. Touch, mouse and controller all work. See
[The GUI](/guide/gui).

## Pick a profile

The profile picker at the top of the drawer switches between your firmware's
profiles (for example Quiet, Balanced and Performance) and any profiles you
have saved yourself. On the OneXPlayer X2 Mini Pro the drawer shows a power
slider instead. See [Profiles and power](/guide/profiles-and-power).

## Set a charge limit

If your battery spends most of its life on the charger, a charge limit of
about 80 % helps it last. Set it with the battery-limit slider in the drawer
or on the full window's Dashboard. When you need a full battery for a trip,
the **Full charge** switch beside it on the Dashboard charges to 100 % once
and then puts the limit back. See [Battery](/guide/battery).

## Change the lighting

Pick a color, an effect, a speed and a brightness in the drawer's lighting
section. On the ROG Flow Z13 you can set the keyboard and the lightbar
separately. See [Lighting](/guide/lighting).

## Five useful commands

Everything in the GUI is also on the command line. These are the commands
worth knowing first.

**See what the machine is doing**: temperatures, fans, profile, power
limits and battery. Add `--watch` to keep it updating.

```sh
voltaire status
```

[Monitoring](/guide/monitoring)

**Switch profile.** `--list` shows the profiles your machine offers.

```sh
voltaire profile --list
voltaire profile --set balanced
```

[Profiles and power](/guide/profiles-and-power#firmware-profiles)

**Set the charge limit**, or charge to full once.

```sh
voltaire batterylimit --set 80
voltaire batterylimit --full-once
```

[Battery](/guide/battery#charge-limit)

**Set the lighting.** `--list-colors` shows every named color; any hex
color like `ff8800` works too.

```sh
voltaire apply --color cyan --brightness high
voltaire apply --mode breathe --color red --speed slow
voltaire off
```

[Lighting](/guide/lighting)

**Check power limits and the fan curve**, and the ranges your machine
accepts.

```sh
voltaire tdp --get
voltaire fancurve --get
```

[Power limits](/guide/profiles-and-power#power-limits) ·
[Fans](/guide/fans)

Add `--dry-run` before any command to see what it would change without
touching the hardware, for example `voltaire --dry-run apply --mode rainbow`.

## Next steps

- [Profiles and power](/guide/profiles-and-power): save your own profiles and
  switch automatically between the charger and battery.
- [Fans](/guide/fans): draw a fan curve or start from a preset.
- [Commands](/reference/commands): every command and option.
