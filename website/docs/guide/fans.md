---
title: Fans
description: Custom fan curves, presets, the high-power fan floor, and what happens to your curve on a profile change or a suspend.
---

# Fans

A **fan curve** tells the fans how fast to spin at each temperature. By
default the firmware runs its own curve. A custom curve lets you trade noise
for temperature: keep the machine silent at light loads, or start the fans
early so it stays cool under a game.

A curve is a short list of points, each a temperature and a fan speed. Between
points the speed rises in a straight line. One curve drives every fan, and it
is part of a [custom profile](/guide/profiles-and-power#custom-profiles).

| Device | Curve | Temperature range |
|---|---|---|
| [ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | 8 points, both fans | 35–105 °C |
| [ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | 8 points, both fans | 30–100 °C |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | No custom curves; the fan speed is shown only | — |

## In the GUI

Open the full window's **Profiles** tab. The **FAN CURVE** card shows the
curve of the profile you are editing as a chart: drag a point to move it.
Points keep their order, so a point can't pass its neighbors, and the speed
never falls as the temperature rises.

A dashed vertical line marks the current processor temperature, and the dot
where it meets the curve shows the speed the curve asks for right now. When
the [fan floor](#fan-floor) is in force, it is drawn as a second line your
points can't go below.

Changes are not written until you press **Apply Changes** (or **Save
Changes** for a profile that is not running) in the bar along the bottom.
**Reset Fans** removes the curve from the profile and hands the fans back to
the firmware.

The drawer has no fan curve controls. Fan speeds appear in the drawer's header
and on the Dashboard's **Fan** chart.

## On the command line

```sh
voltaire fancurve --get
voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"
voltaire fancurve --set "48:1%,53:9%,57:12%,60:17%,63:22%,65:27%,70:35%,76:40%"
voltaire fancurve --set "50:0,60:20%,70:40%,80:60%,85:75%,90:85%,95:95%,100:100%" --profile quiet-work
voltaire fancurve --reset
```

A curve is a comma-separated list of `temp:speed` pairs, exactly as many as
your device's curve has points. The speed is either a value from 0 to 255 or
a percentage with `%`, and the two can be mixed. Temperatures must rise from
point to point and speeds must not fall.

Setting a curve while a firmware profile is running creates the `custom`
profile, as described in [Custom profiles](/guide/profiles-and-power#custom-profiles).
`--profile` stores the curve in a profile you are not running without
applying it.

In the `--get` output, **mode** tells you whether your curve is in force:
`custom` means it is, `auto` means the firmware's own curve is running,
whatever points are listed.

## Presets {#presets}

Presets are ready-made curves to start from, so you don't have to draw one.
Choosing a preset is the same as typing its points yourself: it lands in the
profile you are editing, and you can change it afterwards. Nothing ever
selects a preset for you; switching profiles leaves your curve alone.

| Device | Presets |
|---|---|
| ROG Flow Z13 (2025) | Quiet, Balanced, Turbo |
| ROG Xbox Ally X | None yet ([why](/devices/asus-rog-xbox-ally-x)) |
| OneXPlayer X2 Mini Pro | No fan curves |

On the Z13, **Quiet** and **Balanced** keep the fans stopped at low
temperatures, so the machine stays silent at idle. **Turbo** runs them all the
time and is the one preset already above the [fan floor](#fan-floor)
everywhere.

### In the GUI

The preset buttons sit above the fan curve chart on the Profiles tab.
Choosing one loads its points into the editor without writing anything, so you
can adjust them before you apply. A preset's button stays highlighted while
the curve still matches it exactly.

### On the command line

```sh
voltaire fancurve --list-presets      # names, descriptions and points
voltaire fancurve --preset quiet
```

## The fan floor {#fan-floor}

Running a machine above its safe sustained power limit makes heat that the
firmware's quiet fan behavior can't keep up with. So while the sustained
limit (PL1) is above the device's safe maximum, voltaire keeps the fans at
**at least** a minimum speed that rises with temperature, so the machine can't
cook itself.

- Your curve is kept wherever it is already above the floor. Only points below
  it are raised, and only while the high limit is in force; the curve you saved
  is unchanged and comes back when you lower the limit.
- voltaire sets the fans first and the power limit second. If the fans can't be
  set, the high power limit is refused.
- While the high limit is in force, `fancurve --set` refuses a curve with
  points below the floor, and `fancurve --reset` is refused entirely
  (automatic fans have no floor). Lower the limit first with
  `voltaire tdp --reset`, or use `voltaire tuning --reset`, which does both
  in the safe order.

Only the [ROG Flow Z13](/devices/asus-rog-flow-z13-2025) has a floor; its
device page gives the safe maximum and the floor's speeds. On the Ally and the
OneXPlayer the safe maximum is the top of the range, so the floor never
applies.

## Device notes

- **ROG Flow Z13 and ROG Xbox Ally X:** one curve drives both fans.
- **OneXPlayer X2 Mini Pro:** the fan follows the embedded controller's own
  curve, and voltaire only shows its speed.

## Things to know

### A power profile change drops your curve

The kernel switches the fans back to the firmware's curve whenever anything
changes the power profile: your desktop does this when you plug in or unplug,
and so does the Z13's Fn+F5 key. With the daemon running, voltaire notices and
puts your curve back within about two seconds. Without the daemon, the curve
stays off until you set it again.

### Handing the fans back to the firmware

The firmware takes over the fans again when you switch to a
[firmware profile](/guide/profiles-and-power#firmware-profiles), switch to a
custom profile with no fan curve, press **Reset Fans**, or run
`voltaire fancurve --reset`.

### Before the machine sleeps

On some machines a custom curve survives into sleep and keeps the fans
turning all night. So before a suspend, if a custom profile is running, the
daemon hands the fans back to the firmware (lowering a power limit above the
safe maximum first), and puts your curve back after resume. On the Z13 it
also waits three seconds before letting the suspend go ahead, because a
suspend straight after the hand-back is often woken early.

If you would rather keep your curve through sleep, run the daemon with
`--no-sleep-release`:

```sh
systemctl --user edit voltaire.service
# add:
#   [Service]
#   ExecStart=
#   ExecStart=voltaire daemon --no-sleep-release
systemctl --user restart voltaire.service
```

See [Daemon](/reference/daemon#sleep-and-resume) for what else happens
around sleep.
