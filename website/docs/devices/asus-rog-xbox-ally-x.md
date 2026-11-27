---
title: ASUS ROG Xbox Ally X
description: What voltaire controls on the ASUS ROG Xbox Ally X (Ryzen AI Z2 Extreme), and what does not work yet.
---

# ASUS ROG Xbox Ally X

voltaire supports the ASUS ROG Xbox Ally X (RC73XA, AMD Ryzen AI Z2 Extreme).
It is detected by its firmware's model name, so there is nothing to select.
It uses the same ASUS interfaces as the [ROG Flow Z13](/devices/asus-rog-flow-z13-2025),
with this machine's own limits. Everything here was measured on one machine
running Linux 7.3 with BIOS 317. The non-X ROG Xbox Ally (RC73YA) is a
different machine and is not supported yet.

## Compatibility

| Feature | Status | Notes |
|---|---|---|
| **Firmware power profiles** | Yes | Silent, Balanced and Turbo: about 13, 17 and 35 W sustained on AC; 13, 22 and 28 W on battery. |
| **TDP** | Yes | Three limits: sustained 7–35 W, slow 14–45 W, fast 19–55 W, the ranges the kernel publishes. |
| **Saved custom profiles** | Yes | Fan curve and TDP. |
| **AC / battery autoswitch** | Yes | |
| **CPU boost** | Yes | |
| **CPU undervolt** | No | ([why](#undervolt)) |
| **Fan curves** | Yes | Two fans, eight points. No presets yet ([why](#fan-presets)). |
| **Fan speed** | Yes | |
| **RGB lighting** | Yes | The joystick rings: static, breathe, colour cycle and strobe, four brightness levels, put back after a suspend. With InputPlumber it needs one InputPlumber setting ([how](#joystick-lighting)). |
| **Battery** | Yes | Level, charge and discharge rate, time estimate and pack health. |
| **Charge limit** | Yes | 40–100 %. On the desktop only: in Steam Gaming Mode, Steam sets it. |
| **Brightness and volume** | Yes | |
| **Refresh rate** | Yes | [KDE Plasma only](/devices/#refresh-rate). |
| **Hardware buttons** | Yes | The Armoury Crate button opens voltaire by default; the Library button can be chosen instead. |
| **Controller vibration and deadzones** | No | ([why](#controller-settings)) |
| **Firmware switches** | Yes | POST boot sound and controller power saving. |
| **Telemetry** | Yes | Temperatures, package, GPU, NPU and battery power, load, clocks, memory and network. |

## Buttons and InputPlumber

The Armoury Crate and Library buttons reach voltaire through
InputPlumber, which manages the Ally's controller, or directly when it is not
installed. [Using InputPlumber](/inputplumber) explains what voltaire changes
there: the button that opens voltaire stops doing its InputPlumber job while
it does.

## Joystick lighting

The rings around the sticks speak ASUS Aura, the same protocol as the Z13's
keyboard, on one of the controller's interfaces (HID interface 2). voltaire
sends them the packets G-Helper sends the Ally.

InputPlumber hides that interface along with the rest of the controller, so
on a machine where it runs (CachyOS's handheld edition starts it), the
lighting shows as unavailable until InputPlumber is told to leave the
interface visible. [Using InputPlumber](/inputplumber#ally-lighting) has the
one-line setting, what it costs, and how to undo it. Without InputPlumber
there is nothing to do.

The kernel's own LED device for the rings (`ally:rgb:joystick_rings`) is no
substitute: it only shows shades of blue.

## Not supported yet, and why

### Undervolt

There is no undervolt control. It needs the out-of-tree `ryzen_smu` module,
voltaire's Curve Optimizer support has not been tested on this processor,
and G-Helper does not offer undervolting on the Z2 Extreme either.

### Fan presets

Custom fan curves work, but this machine's fan controller does not respond to
low curve values in proportion: 30, 56 and 100 (out of 255) all ran the fans
at about 6300 rpm, and 255 at about 8100 rpm. Presets wait until the low end
is understood, so that "Quiet" really is quiet.

### Controller settings

Vibration strength, deadzones and response curves are part of a kernel driver
series still under review for Linux (ASUS ROG Ally support in `hid-asus`).
voltaire will offer them once a kernel carries it. The driver some
distributions ship in the meantime cannot set its own default values back.
