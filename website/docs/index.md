---
title: Overview
description: Power limits, profiles, fan curves, RGB lighting, battery and controller settings for supported ASUS and OneXPlayer laptops and handhelds on Linux.
---

# voltaire

voltaire controls the hardware settings that Linux leaves out on a handful of
gaming laptops and handhelds: power limits, performance profiles, fan curves,
RGB lighting, the battery charge limit and the firmware's own switches. It
comes as a touch-friendly overlay, `voltaire-gui`, that works on the desktop
and in Steam Gaming Mode, and a command-line tool, `voltaire`, whose
background daemon keeps your settings in force. It was called z13ctl and
z13gui before 2.0.

## What it does

- **Performance profiles**: switch between your firmware's profiles, or save
  your own named profiles with their own power limits, fan curve and
  undervolt.
- **Automatic profile switching**: use one profile on the charger and another
  on battery.
- **Power limits (TDP)**: set how much power the processor may draw, kept in
  force when the firmware tries to reset them.
- **Fan curves**: draw your own curve, or start from a preset.
- **CPU undervolt and boost**: lower the CPU voltage, or turn boost clocks off
  to save battery.
- **RGB lighting**: color, effect, speed and brightness.
- **Battery**: a charge limit to look after the battery, with a one-time
  charge to full when you need it.
- **Controller and buttons**: open voltaire with a hardware button, navigate it
  with the built-in controller, and set the controller's vibration strength.
- **Monitoring**: temperatures, power draw, clocks, fan speeds and battery,
  with up to an hour of history.

Settings survive reboots and suspends, and nothing needs root after a
one-time setup step.

## Supported devices

- [ASUS ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025)
- [ASUS ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x)
- [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro)

Not every feature is available on every machine.
[Supported devices](/devices/) shows what works on yours, and why something
does not.

::: tip New to Linux? Start with the GUI
Everything voltaire does is in the overlay: press your device's hardware
button (or open **Voltaire** from your app menu) and change settings by touch,
mouse or controller. The command line is there for scripting and fine
tuning, but you never need it. See [The GUI](/guide/gui).
:::

## Where to start

- [Installation](/installation): install the package and run the one-time
  setup.
- [Quick Start](/quick-start): your first ten minutes with voltaire.
- [Upgrading from z13ctl](/migrating-from-z13ctl): what changes and what
  carries over.

## Background

voltaire began on the 2025 ROG Flow Z13, whose lighting no existing Linux tool
could control. It talks to the hardware through the kernel's own interfaces,
with no kernel patches and no extra daemons, and its lighting speaks the ASUS
Aura protocol, reverse-engineered from
[G-Helper](https://github.com/seerge/g-helper) and documented in the
[HID protocol reference](/dev/hid-protocol).
