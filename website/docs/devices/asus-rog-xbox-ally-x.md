---
title: ASUS ROG Xbox Ally X
description: What voltaire controls on the ASUS ROG Xbox Ally X (Ryzen AI Z2 Extreme), what it needs installed, and what does not work yet.
---

# ASUS ROG Xbox Ally X

The ROG Xbox Ally X (RC73XA, AMD Ryzen AI Z2 Extreme) uses the same ASUS
interfaces as the [ROG Flow Z13](/devices/asus-rog-flow-z13-2025), with its
own limits, and shares its power limits and charge limit with Steam.

## At a glance

- **Detected as:** the Xbox Ally X (RC73XA), by its firmware's model name. The
  non-X ROG Xbox Ally (RC73YA) is a different machine and is not supported
  yet.
- **Firmware profiles:** Silent, Balanced and Turbo.
- **Power limits:** sustained 7–35 W, slow 14–45 W, fast 19–55 W. In Steam
  Gaming Mode, Steam sets them ([below](#steam-gaming-mode)).
- **Fans:** two fans, one 8-point curve.
- **Lighting:** the joystick rings.
- **Charge limit:** 40–100 %, set by Steam in Gaming Mode.
- **Buttons:** the Armoury Crate button opens voltaire; the Library button can
  be chosen instead.

## Compatibility

| Feature | Status | Notes |
|---|---|---|
| **Power** | | |
| Power limits (TDP) | ⚠️ | Sustained 7–35 W, slow 14–45 W, fast 19–55 W. On the desktop only: [Steam sets them in Gaming Mode](#steam-gaming-mode). |
| Firmware power profiles | ✅ | Silent, Balanced and Turbo ([wattages](#profiles)). |
| Saved custom profiles | ✅ | Each holds its own power limits and fan curve. |
| Switch profile on AC / battery | ✅ | |
| CPU boost on/off | ✅ | |
| CPU undervolt | ❌ | [Why](#undervolt). |
| **Cooling** | | |
| Fan speed readout | ✅ | Both fans. |
| Custom fan curves | ✅ | One 8-point curve drives both fans. |
| Fan curve presets | ❌ | [Why](#fan-presets). |
| **Lighting** | | |
| RGB lighting | ⚠️ | The joystick rings: static, breathe, cycle and strobe, four brightness levels. With InputPlumber it needs one setup option ([how](#joystick-lighting)). |
| **Battery** | | |
| Charge level, rate, time left | ✅ | |
| Charge limit | ⚠️ | 40–100 %. On the desktop only: [Steam sets it in Gaming Mode](#steam-gaming-mode). |
| Full charge once | ⚠️ | On the desktop only, like the charge limit. Needs the daemon running. |
| **Display and sound** | | |
| Screen brightness and volume | ✅ | On any desktop and in Steam Gaming Mode. |
| Refresh rate | ⚠️ | [KDE Plasma only](/devices/#refresh-rate). |
| HDR on/off | ❌ | No HDR switch on any device ([why](/devices/#hdr)). |
| **Buttons and controller** | | |
| Hardware button opens voltaire | ✅ | Armoury Crate (the default) or Library ([below](#buttons)). |
| Controller vibration strength | ❌ | [Why](#controller-settings). |
| Controller power saving | ✅ | A firmware switch ([below](#controller-power-saving)). |
| **Firmware settings** | | |
| Firmware switches | ✅ | POST boot sound and controller power saving. |
| **Telemetry** | | |
| Temperatures, power, load, clocks, memory, network | ✅ | Including package, GPU and battery power. |
| NPU power and load | ✅ | |
| Up to an hour of history | ✅ | |

## What it needs {#what-it-needs}

- **`sudo voltaire setup`**, once, and membership of the group it grants
  (`users` by default). The packages run setup for you; see
  [Installation](/installation#what-setup-does).
- **InputPlumber** manages the Ally's controller on most handheld
  distributions, and voltaire works with it: the hardware button and the
  controller's navigation of voltaire's drawer both go through InputPlumber.
  That needs your user in the **`wheel`** or **`inputplumber`** group (check
  with `id`); most handheld distributions put the first user in `wheel`.
  Without it the button stays silent and the controller cannot drive the
  drawer. See [InputPlumber](/guide/buttons-and-controllers#inputplumber).
- **For the ring lighting under InputPlumber**, one opt-in setup option,
  then a daemon restart:

  ```sh
  sudo voltaire setup --inputplumber-passthrough
  systemctl --user restart voltaire.service
  ```

  Without InputPlumber there is nothing to do. See
  [the joystick lighting](#joystick-lighting).
- **In Steam Gaming Mode, Steam owns the power limits and the charge limit.**
  Set them in Steam's own menus there, or switch to the desktop to set them
  in voltaire ([below](#steam-gaming-mode)).

## Profiles

The firmware's three profiles hold these sustained power limits:

| Profile | On the charger | On battery |
|---|---|---|
| Silent | 13 W | 13 W |
| Balanced | 17 W | 22 W |
| Turbo | 35 W | 28 W |

The whole range, up to 35 W sustained, is safe on this machine, so there is
no `--force` step and no fan floor.

## Steam Gaming Mode

In Gaming Mode, Steam's performance menu sets the power limits and Steam sets
the charge limit, the same ones voltaire would set. So that the two never
fight, voltaire leaves both to Steam there: the drawer and the command line
show them as unavailable, with the reason, and point you to the desktop.
Profiles, fan curves, lighting and everything else stay voltaire's.

When you switch between Gaming Mode and the desktop, voltaire notices within
about ten seconds and offers the controls that belong to it in the new
session. See [Steam Gaming Mode](/guide/gaming-mode#steam-controls).

## Joystick lighting

The rings around the sticks take color, effect (static, breathe, cycle or
strobe) and four brightness levels, and voltaire puts them back after a
suspend.

InputPlumber hides the part of the controller the lighting goes through, so
on a machine where InputPlumber runs, the lighting shows as unavailable until
you run `sudo voltaire setup --inputplumber-passthrough`. That tells
InputPlumber to leave that part visible, keeps doing so through InputPlumber's
updates, and leaves controller input and rumble working.
`sudo voltaire setup --inputplumber-passthrough=false` undoes it.
[The Ally's lighting](/guide/buttons-and-controllers#ally-lighting) explains
exactly what it changes.

## Buttons

The **Armoury Crate** button (left of the screen) opens voltaire by default;
the **Library** button (right of the screen) can be chosen instead, in
Settings or with `voltaire button --set library`. With InputPlumber running,
the chosen button stops doing its InputPlumber job while it opens voltaire;
the other keeps its own. See
[Buttons and controllers](/guide/buttons-and-controllers#opening-voltaire).

## Controller power saving

**Controller power saving** is a firmware switch on the Settings tab. On, the
controller is powered down while the handheld sleeps: less battery drain
overnight, at the cost of a slightly slower wake. From the command line:

```sh
voltaire feature --get mcu_powersave
voltaire feature --set mcu_powersave=1
```

## Not supported yet, and why

### Undervolt

There is no undervolt control. It would need the separately installed
`ryzen_smu` module, voltaire's undervolting has not been tested on this
processor, and G-Helper does not offer undervolting on it either.

### Fan presets

Custom fan curves work, but this machine's fans do not slow down in proportion
to low curve values: low and middling settings all run the fans at much the
same speed. Presets wait until that is understood, so that a "Quiet" preset is
really quiet.

### Controller settings

Vibration strength, deadzones and response curves need a kernel driver for the
Ally's controller that is not in released kernels yet. voltaire will offer them
once a kernel carries it.

## Good to know

- **The kernel's own ring-lighting device is no substitute.** It can only show
  shades of blue, which is why voltaire talks to the rings directly.
- **Default drawer:** the profile picker, brightness, volume, CPU boost,
  refresh rate and lighting. Rearrange it in Settings → Quickbar.
