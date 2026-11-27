---
title: Profiles and power
description: Firmware profiles, custom profiles, AC/battery switching, power limits, undervolt and CPU boost.
---

# Profiles and power

A **profile** decides how much power your machine may draw and how it cools
itself. voltaire offers two kinds: the **firmware profiles** your machine's
firmware ships with, and **custom profiles** you build yourself from a fan
curve, power limits and an undervolt. You can also switch profile
automatically when the charger is plugged in or pulled out.

This page covers profiles and the power settings that go into them. Fan curves
have their own page, [Fans](/guide/fans).

## Firmware profiles {#firmware-profiles}

Firmware profiles are the power modes built into the machine. Choosing one
hands power and cooling back to the firmware: its own power limits, its own
fan behavior, and no undervolt.

| Device | Profiles (GUI label, then CLI name) |
|---|---|
| [ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | Quiet `quiet`, Balanced `balanced`, Performance `performance` |
| [ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | Silent `low-power`, Balanced `balanced`, Turbo `performance` |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | None while the TDP slider is available; voltaire keeps the firmware on Balanced. Without the `ryzen_smu` module: Low Power `low-power`, Balanced `balanced`, Performance `performance` |

On the Ally the GUI uses ASUS's names (Silent, Turbo) and the command line
uses the kernel's (`low-power`, `performance`). They are the same profiles.

On the OneXPlayer the TDP slider is the power control, and its firmware's Low
Power profile would cap the machine far below the slider. voltaire therefore
keeps Balanced selected and puts it back if another program changes it. See
[Power limits](#power-limits).

### In the GUI

The drawer's **Performance profile** section has one button per firmware
profile. On the full window's **Dashboard**, the same buttons are the
**Profile** row of the POWER card. Tap one and it takes effect at once.

### On the command line

```sh
voltaire profile --list               # this machine's profiles, least power first
voltaire profile --get
voltaire profile --set balanced
```

### Things to know

- Switching to a firmware profile never deletes your custom profiles. Select
  one again whenever you like.
- With the daemon running, setting a firmware profile also moves
  power-profiles-daemon (if installed) to the matching profile.
- Firmware profile names (`quiet`, `balanced`, `low-power`, `performance` and
  the other kernel names) are reserved and can't be used for a custom profile.

## Custom profiles {#custom-profiles}

A custom profile is a named set of settings that voltaire applies itself.
Depending on the device it can hold:

| Device | A custom profile holds |
|---|---|
| ROG Flow Z13 (2025) | Fan curve, power limits, undervolt |
| ROG Xbox Ally X | Fan curve, power limits |
| OneXPlayer X2 Mini Pro | Power limits, undervolt |

Selecting a custom profile puts the machine into exactly the state it
describes. A setting the profile leaves empty is handed back to the firmware:
a profile with no fan curve leaves the fans on automatic, one with no
undervolt clears it. That makes switching between two custom profiles
predictable.

Custom profiles are stored by the daemon, so it must be running to create,
switch to or edit one.

### In the GUI

The full window's **Profiles** tab is the profile editor. The drawer only
**picks** profiles; it has no editor.

- **The profile dropdown** at the top names the profile you are editing. A
  dot marks the one that is running.
- **Activate** switches the machine to the profile you are editing.
- **+ New** creates an empty profile with a name you choose.
- **Save As** copies the running profile under a new name.
- **Delete Profile** removes a profile (click it twice). You can't delete the
  running profile or one autoswitch uses.

The **POWER** card holds the power limits (and the undervolt, under
**Advanced**), and the **FAN CURVE** card the fan curve. Change anything and
the bar along the bottom shows what is unsaved. Its button reads **Apply
Changes** when the profile is running (the hardware changes at once) or
**Save Changes** when it is not (stored for when you activate it).

To switch profiles from the drawer, use the dropdown beside the firmware
profile buttons in the **Performance profile** section. A profile with nothing
saved in it is shown greyed out as "(empty)". On the Dashboard, **Custom…**
opens the Profiles tab.

On the OneXPlayer the section is called **Saved profile** and is off by
default in the drawer (turn it on in Settings → Quickbar). Its **No profile**
entry goes back to the TDP slider's setting.

### On the command line

The first fan curve, power limit or undervolt you set while a firmware profile
is running creates a profile called `custom` and switches to it. That `custom`
holds only the setting you just made, replacing whatever it held before, so
give a setup you want to keep its own name.

```sh
voltaire tdp --set 50                 # creates and activates "custom"
voltaire profile --save-as gaming     # copy the running profile under a new name
voltaire profile --set gaming
voltaire profile --list
voltaire profile --delete custom
```

Editing a setting changes the profile that is running and is saved at once;
there is no separate save step. To build a profile **without** applying it,
create it and add `--profile` to each setting:

```sh
voltaire profile --create battery-saver
voltaire tdp --set 25 --profile battery-saver
voltaire undervolt --set -20 --profile battery-saver
voltaire profile --set battery-saver  # now apply it
```

Profile names are 1 to 32 characters of lowercase letters, digits, `-` and
`_`.

### Things to know

- On the OneXPlayer, moving the TDP or undervolt slider while a saved profile
  is running leaves the profile; the profile keeps its own values. Change a
  saved profile on the Profiles tab or with `--profile`.
- On the ROG Xbox Ally X in Steam Gaming Mode, a custom profile's power limits
  are not applied, because Steam owns them there. Its fan curve still is. See
  [What Steam controls](/guide/gaming-mode#steam-controls).

## Switching on AC and battery {#autoswitch}

Autoswitch applies one profile when the charger is plugged in and another when
it is pulled out. Each side can be any firmware or custom profile, or left
alone so your desktop's power settings decide.

It acts only when the power source actually changes, about two seconds after
you plug in or unplug. A profile you pick by hand in between stays until the
next change. Turning autoswitch on applies the right profile for the power
source you are on straight away.

### In the GUI

Turn on the switch in the drawer's **AC/battery autoswitch** section, or the
**Autoswitch** row in the Dashboard's POWER card. Two rows appear, **On AC**
and **On battery**; pick a profile for each, or **(don't change)** to leave
that side alone.

### On the command line

```sh
voltaire autoswitch --ac balanced --battery battery-saver
voltaire autoswitch --get
voltaire autoswitch --off             # stop switching, keep the two choices
voltaire autoswitch --on
voltaire autoswitch --ac "" --battery battery-saver   # leave AC to the desktop
voltaire autoswitch --clear
```

### Things to know

- Autoswitch needs the daemon, which watches the power source.
- GNOME's Automatic Power Saver reacts to low battery, not to unplugging, so
  it can still change a firmware profile after autoswitch has acted. Leave a
  side empty if you want your desktop to own it entirely.

## Power limits (TDP) {#power-limits}

The power limits, often called TDP, set how much power the processor may draw.
More power means more performance, more heat and more fan noise; less power
means a cooler, quieter machine and longer battery life.

There are three limits:

| Limit | What it controls |
|---|---|
| **PL1, sustained** | The power the processor can draw indefinitely. This is the one that matters most. |
| **PL2, slow** | A higher limit allowed for tens of seconds before it drops back to PL1. |
| **PL3, fast** | The highest limit, for very short bursts. |

Setting all three to the same value gives a flat cap. Setting PL2 and PL3
higher lets short, bursty work run faster.

Each machine has its own range, read from the kernel where it publishes one;
the device pages list them. `voltaire tdp --get` shows the current limits, the
range your machine accepts and its **safe maximum**.

### In the GUI

On the Z13 and the Ally, power limits are part of a custom profile. Open the
full window's **Profiles** tab: the POWER card has one slider for all three
limits, or one per limit with **Advanced** ticked. On the Z13 the advanced
sliders reach beyond the safe maximum, with a warning, and going there also
raises the fans (see [The fan floor](/guide/fans#fan-floor)).

On the OneXPlayer the power limit is a single **TDP** slider in the drawer and
on the Dashboard's POWER card. It sets all three limits and is a setting of
its own, not part of a profile: nothing is written until you first move it,
and the one value is used on both AC and battery (capped on battery to the
battery maximum).

### On the command line

```sh
voltaire tdp --get                        # current limits, range and safe maximum
voltaire tdp --set 45                     # all three limits to 45 W
voltaire tdp --set 45 --pl2 55 --pl3 65   # sustain 45 W, burst higher
voltaire tdp --set 80 --force             # above the safe maximum (Z13)
voltaire tdp --reset                      # back to the default firmware profile
```

A PL2 or PL3 below its minimum is raised to it, and the command says so. A PL1
above the safe maximum is refused unless you add `--force`. On the Z13 the
fans are then held to a minimum speed first; see
[The fan floor](/guide/fans#fan-floor).

`tdp --reset` switches to the device's default firmware profile (Balanced)
and hands the power limits back to the firmware. On the OneXPlayer it forgets
your slider value and writes the firmware's own preset for the power source.

### Device notes

- **ROG Flow Z13 and ROG Xbox Ally X:** three separate limits. On the Ally
  the safe maximum is the top of the range, so `--force` is never needed.
- **OneXPlayer X2 Mini Pro:** one slider, through the `ryzen_smu` module
  ([installing it](/installation#ryzen-smu)). Without it there is no TDP
  control and the firmware profiles are offered instead.
- **ROG Xbox Ally X in Steam Gaming Mode:** Steam sets the power limits;
  set them on the desktop. See [What Steam controls](/guide/gaming-mode#steam-controls).

### Things to know

::: warning Keep the daemon running for a custom power limit
The firmware puts its own limits back on its own: on the Z13 and the Ally
whenever anything changes the power profile (your desktop does this when you
plug in or unplug) or hands the fans back to automatic, and on the OneXPlayer
whenever the charger is connected or disconnected. The reported value does
not change, so nothing looks wrong. The daemon notices and puts your limit
back within about two seconds; without it, your limit is lost until you set
it again.
:::

- The daemon also puts your power limits back after a suspend and at boot.
- `tdp --get` on a firmware profile shows that profile's stock limits, not a
  live reading. To see what the processor really draws, use the Dashboard's
  power chart (see [Monitoring](/guide/monitoring)).

## Undervolt {#undervolt}

An undervolt runs the processor at a slightly lower voltage for the same
clock speed, which means less heat and power for the same work. voltaire uses
AMD's Curve Optimizer: a negative offset, from 0 (stock) down to your device's
limit.

It needs the out-of-tree `ryzen_smu` kernel module
([installing it](/installation#ryzen-smu)). Without it the undervolt controls
are not shown.

::: warning Start small
Too large an offset makes the machine unstable or crash, and the point where
that happens differs from chip to chip. Try a small offset such as -10, use
the machine for a while, and go further in small steps.
:::

### In the GUI

- **ROG Flow Z13:** part of a custom profile. On the full window's
  **Profiles** tab, tick **Advanced** in the POWER card to show the undervolt
  slider.
- **OneXPlayer X2 Mini Pro:** a setting of its own, like the TDP slider. Set
  it on the Dashboard's POWER card, or turn on the drawer's **Undervolt**
  section in Settings → Quickbar. A saved profile can still carry its own
  offset, which wins while that profile runs.

### On the command line

```sh
voltaire undervolt --get
voltaire undervolt --set -15
voltaire undervolt --set -20 --profile battery-saver   # store it in a profile
voltaire undervolt --reset                              # back to stock
voltaire --dry-run undervolt --set -15                  # preview
```

### Device notes

| Device | Undervolt |
|---|---|
| ROG Flow Z13 (2025) | Yes, in custom profiles; needs `ryzen_smu` |
| OneXPlayer X2 Mini Pro | Yes, as a device setting; needs `ryzen_smu` |
| ROG Xbox Ally X | No ([why](/devices/asus-rog-xbox-ally-x)) |

### Things to know

- The processor forgets the offset at every reboot and suspend. The daemon
  applies it again at startup and after resume.
- Switching to a firmware profile clears the offset (on the Z13; on the
  OneXPlayer your device setting stays).
- There is no way to read the offset back from the processor, so
  `undervolt --get` reports what the daemon last applied.

## CPU boost {#cpu-boost}

CPU boost lets the processor run above its base clock when it has the power
and thermal room. Turning it off caps every core at its base clock: less peak
speed, but less peak power and heat. All three devices support it.

The kernel turns boost back on at every boot, so the daemon remembers your
choice and restores it when it starts.

### In the GUI

The **CPU boost** switch is in the Dashboard's POWER card. It is also an
optional drawer section; turn it on in Settings → Quickbar.

### On the command line

```sh
voltaire cpuboost --get
voltaire cpuboost --set 0     # off
voltaire cpuboost --set 1     # on
```

Set with the daemon stopped, the change applies now but does not survive a
reboot, and the command says so.

## Resetting everything {#reset}

To return to stock behavior in one step, reset the tuning: the fan curve, the
power limits and the undervolt together. The machine lands on its default
firmware profile (Balanced) with automatic fans and stock power, and the
profile you were editing forgets those three settings.

### In the GUI

On the full window's **Profiles** tab, **Reset All** at the left of the
bottom bar clears all three from the profile you are editing. The cards' own
**Reset TDP**, **Reset Fans** and **Reset UV** buttons each clear just one.

### On the command line

```sh
voltaire tuning --reset                     # clear everything, back to stock
voltaire tuning --reset --profile gaming    # clear a profile you are not running
```

### Things to know

- Use `tuning --reset` rather than the three separate resets in a row. It runs
  them in the safe order (power down before the fans are released), which is
  easy to get wrong by hand.
- `tdp --reset` keeps a saved undervolt so you can get it back with
  `voltaire profile --set custom`; `tuning --reset` forgets it.
- `--profile` needs the daemon.
