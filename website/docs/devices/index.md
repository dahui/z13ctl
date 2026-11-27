---
title: Supported devices
description: Which machines voltaire supports, and which features work on each one.
---

# Supported devices

voltaire recognizes a supported machine by its firmware's model name, so
there is nothing to select or configure. Each machine offers only what its
hardware and kernel support: a feature it lacks is left out of the GUI, and
the command line refuses it with a reason.

| Device | Detected as |
|---|---|
| [ASUS ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | every GZ302 variant (GZ302EA and others) |
| [ASUS ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | the Xbox Ally X (RC73XA) |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | the X2 Mini Pro (AMD Strix Halo) |

## Compatibility

| Symbol | Meaning |
|---|---|
| ✅ | Works |
| ⚠️ | Works, with a condition (follow the link) |
| ❌ | Not supported yet (follow the link for why) |
| — | Not applicable to this hardware |

Most gaps are in the kernel, not in voltaire, and close when a kernel with the
support is released.

| Feature | ROG Flow Z13 (2025) | ROG Xbox Ally X | OneXPlayer X2 Mini Pro |
|---|---|---|---|
| **Power** | | | |
| Power limits (TDP) | ✅ three limits, 28–80 W sustained | ⚠️ three limits, 7–35 W sustained; [desktop only](/devices/asus-rog-xbox-ally-x#steam-gaming-mode) | ⚠️ one slider, 10–85 W; [needs ryzen_smu](/devices/onexplayer-x2-mini-pro#tdp-needs-ryzen-smu) |
| Firmware power profiles | ✅ Quiet, Balanced, Performance | ✅ Silent, Balanced, Turbo | ⚠️ [only without ryzen_smu](/devices/onexplayer-x2-mini-pro#tdp-needs-ryzen-smu) |
| Saved custom profiles | ✅ power limits, fan curve, undervolt | ✅ power limits, fan curve | ✅ power limit, undervolt |
| Switch profile on AC / battery | ✅ | ✅ | ✅ |
| CPU boost on/off | ✅ | ✅ | ✅ |
| CPU undervolt | ⚠️ [needs ryzen_smu](/devices/asus-rog-flow-z13-2025#what-it-needs) | ❌ [why](/devices/asus-rog-xbox-ally-x#undervolt) | ⚠️ [needs ryzen_smu](/devices/onexplayer-x2-mini-pro#undervolt) |
| **Cooling** | | | |
| Fan speed readout | ✅ two fans | ✅ two fans | ✅ |
| Custom fan curves | ✅ | ✅ | ❌ [why](/devices/onexplayer-x2-mini-pro#fan-control) |
| Fan curve presets | ✅ Quiet, Balanced, Turbo | ❌ [why](/devices/asus-rog-xbox-ally-x#fan-presets) | — |
| **Lighting** | | | |
| RGB lighting | ✅ keyboard and lightbar | ⚠️ joystick rings; [one setup option with InputPlumber](/devices/asus-rog-xbox-ally-x#joystick-lighting) | ❌ [why](/devices/onexplayer-x2-mini-pro#joystick-lighting) |
| **Battery** | | | |
| Charge level, rate, time left | ✅ | ✅ | ✅ |
| Charge limit | ✅ 40–100 % | ⚠️ 40–100 %; [desktop only](/devices/asus-rog-xbox-ally-x#steam-gaming-mode) | ❌ [why](/devices/onexplayer-x2-mini-pro#charge-limit) |
| Full charge once | ✅ | ⚠️ [desktop only](/devices/asus-rog-xbox-ally-x#steam-gaming-mode) | ❌ [needs the charge limit](/devices/onexplayer-x2-mini-pro#charge-limit) |
| **Display and sound** | | | |
| Screen brightness | ✅ | ✅ | ✅ |
| Volume | ✅ | ✅ | ✅ |
| Refresh rate | ⚠️ [KDE Plasma only](#refresh-rate) | ⚠️ [KDE Plasma only](#refresh-rate) | ⚠️ [KDE Plasma only](#refresh-rate) |
| HDR on/off | ❌ [why](#hdr) | ❌ [why](#hdr) | ❌ [why](#hdr) |
| **Buttons and controller** | | | |
| Hardware button opens voltaire | ✅ Armoury Crate button | ✅ Armoury Crate or Library button | ✅ Keyboard, OneXPlayer or Home button |
| Controller vibration strength | — | ❌ [why](/devices/asus-rog-xbox-ally-x#controller-settings) | ✅ off to 5 |
| Controller power saving | — | ✅ | — |
| **Firmware settings** | | | |
| Firmware switches | ✅ POST boot sound, panel overdrive | ✅ POST boot sound, controller power saving | ❌ [why](/devices/onexplayer-x2-mini-pro#firmware-settings) |
| **Telemetry** | | | |
| Temperatures, power, load, clocks, memory, network | ✅ | ✅ | ✅ |
| NPU power and load | ✅ | ✅ | ❌ [why](/devices/onexplayer-x2-mini-pro#npu-telemetry) |
| Up to an hour of history | ✅ | ✅ | ✅ |

The ranges above are what each machine's kernel reports today. Each device's
page has the details: what to install, the numbers for each profile, and what
does not work yet.

## On every device

### Refresh rate

The refresh-rate control, and the option to switch rate with the power source,
work on **KDE Plasma** only. A screen's video mode belongs to the desktop, and
KDE's is the one voltaire can drive. Other desktops get no refresh-rate
control. In Steam Gaming Mode it is hidden, because Steam's Quick Access menu
sets the refresh rate there.

Brightness and volume work on every desktop and in Gaming Mode.

### HDR

voltaire has no HDR switch on any device. Only the desktop (or gamescope, in
Gaming Mode) can turn HDR on or off, and each does it differently, so use your
desktop's display settings, or Steam's in Gaming Mode. Whether HDR is possible
at all depends on the screen: the ROG Flow Z13's panel does not support it,
and the X2 Mini Pro's does.

## A machine that is not listed

On any other machine voltaire does not start: the daemon and the command line
stop with `no device file matches this machine`, followed by the machine's
firmware vendor and model names. To ask for a machine, open an issue on
[GitHub](https://github.com/dahui/voltaire/issues) with those two names.
Adding a machine means writing a device file that describes its hardware
([Device Files](/dev/device-files)).
