---
title: ASUS ROG Flow Z13 (2025)
description: What voltaire controls on the 2025 ASUS ROG Flow Z13 (GZ302, Strix Halo), what it needs installed, and its limits.
---

# ASUS ROG Flow Z13 (2025)

The 2025 ROG Flow Z13 (GZ302 series, AMD Strix Halo) is the machine voltaire
began on, and the one with the fullest support.

## At a glance

- **Detected as:** every GZ302 variant (GZ302EA and others), by its firmware's
  model name.
- **Firmware profiles:** Quiet, Balanced and Performance.
- **Power limits:** sustained, slow and fast; 28–80 W sustained on the
  GZ302EA, with a fan floor above 75 W.
- **Fans:** two fans on one shared 8-point curve, with Quiet, Balanced and
  Turbo presets.
- **Lighting:** the detachable keyboard's backlight and the edge lightbar.
- **Charge limit:** 40–100 %.
- **Button:** the Armoury Crate button opens voltaire.

## Compatibility

| Feature | Status | Notes |
|---|---|---|
| **Power** | | |
| Power limits (TDP) | ✅ | Sustained, slow and fast limits; 28–80 W sustained on the GZ302EA ([below](#power-limits-and-the-fan-floor)). |
| Firmware power profiles | ✅ | Quiet, Balanced and Performance ([wattages](#profiles)). |
| Saved custom profiles | ✅ | Each holds its own power limits, fan curve and undervolt. |
| Switch profile on AC / battery | ✅ | A profile for each power source. |
| CPU boost on/off | ✅ | Put back at every boot. |
| CPU undervolt | ⚠️ | 0 to −40. Needs the `ryzen_smu` module ([what it needs](#what-it-needs)). |
| **Cooling** | | |
| Fan speed readout | ✅ | Both fans. |
| Custom fan curves | ✅ | One 8-point curve drives both fans. |
| Fan curve presets | ✅ | Quiet, Balanced and Turbo. |
| **Lighting** | | |
| RGB lighting | ✅ | Keyboard and lightbar, together or separately: static, breathe, cycle, rainbow and strobe. |
| **Battery** | | |
| Charge level, rate, time left | ✅ | Also shows whether it is charging over USB-C or the power adapter. |
| Charge limit | ✅ | 40–100 %. |
| Full charge once | ✅ | Needs the daemon running. |
| **Display and sound** | | |
| Screen brightness and volume | ✅ | On any desktop and in Steam Gaming Mode. |
| Refresh rate | ⚠️ | The panel's rate, and a rate per power source ([KDE Plasma only](/devices/#refresh-rate)). |
| HDR on/off | ❌ | The panel does not support HDR ([more](/devices/#hdr)). |
| **Buttons and controller** | | |
| Hardware button opens voltaire | ✅ | The Armoury Crate button ([below](#buttons)). |
| Controller vibration strength | — | No built-in controller. |
| Controller power saving | — | No built-in controller. |
| **Firmware settings** | | |
| Firmware switches | ✅ | POST boot sound and panel overdrive. |
| **Telemetry** | | |
| Temperatures, power, load, clocks, memory, network | ✅ | Including package, GPU and battery power. |
| NPU power and load | ✅ | |
| Up to an hour of history | ✅ | |

## What it needs {#what-it-needs}

- **`sudo voltaire setup`**, once, and membership of the group it grants
  (`users` by default), so voltaire can reach the lighting, fans, power
  limits, battery and firmware switches without root. The packages run setup
  for you; see [Installation](/installation#what-setup-does).
- **A 7.x kernel for the power limits.** Its asus-armoury driver knows the
  Z13's power-limit ranges. On older kernels voltaire falls back to an older
  power interface, which exists only if your distribution still builds it.
- **The `ryzen_smu` module for undervolting**, from the amkillam fork (the
  original does not support Strix Halo). See
  [Installation](/installation#ryzen-smu). Everything else works without it.

## Profiles

The firmware's three profiles hold these sustained power limits:

| Profile | Sustained power |
|---|---|
| Quiet | 40 W |
| Balanced | 52 W |
| Performance | 70 W |

`voltaire tdp --reset` returns to Balanced and its own limits. A saved
custom profile of your own can hold any limit in the machine's range; see
[Profiles and power](/guide/profiles-and-power#custom-profiles).

## Power limits and the fan floor {#power-limits-and-the-fan-floor}

The kernel reports the ranges. On the GZ302EA the sustained limit runs from
28 to 80 W, the slow limit from 32 W and the fast limit from 45 W; a slow or
fast limit set below its minimum is raised to it. `voltaire tdp --get` prints
the ranges on your machine.

Above **75 W sustained**, the safe maximum, voltaire holds both fans to at
least half speed, rising to full speed by 80 °C. It sets the fans before the
power limit and refuses the limit if it cannot. Your own fan curve is kept
wherever it is already above that floor, and the Turbo preset is above it
everywhere. On the command line, going past 75 W takes `--force`. See
[the fan floor](/guide/fans#fan-floor).

The firmware puts its own limits back whenever the power profile changes or
the fans return to automatic control. The daemon notices and restores your
limits within about two seconds, so a custom power limit needs the daemon
running.

## Buttons

Only the **Armoury Crate** button can open voltaire. The Z13's other hotkeys
already do something (Fn+F5 changes the power profile, for example), and
voltaire never takes a key away from the desktop.

The press also reaches the desktop. To leave the button alone, choose
**No button** in Settings, or run `voltaire button --set none`. See
[Buttons and controllers](/guide/buttons-and-controllers#opening-voltaire).

## The detachable keyboard

The keyboard forgets its lighting when it is detached. voltaire notices it
come back and restores your effect within about two seconds.

## Sleep

Before the machine sleeps, voltaire turns the lighting off. If a custom fan
curve is running, it also hands the fans back to the firmware so they can stop
during sleep, then waits three seconds before letting the suspend go ahead: a
suspend straight after that hand-back is often woken early. Everything is put
back on resume.

## Good to know

- **Both fans share one curve.** They cool the same chip, and the kernel takes
  one curve for both.
- **Quiet and Balanced presets keep silent idle.** The fans stop at low
  temperatures only while the curve's low points are zero, which is why those
  presets start at zero. Turbo keeps the fans running.
- **Only two firmware switches.** Some other ASUS models offer settings such as
  GPU mode switching; voltaire never offers those, because a wrong value can
  leave a machine without a display.
