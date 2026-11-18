---
title: OneXPlayer X2 Mini Pro
description: What voltaire controls on the OneXPlayer X2 Mini Pro (Strix Halo), what it needs installed, and what does not work yet.
---

# OneXPlayer X2 Mini Pro

voltaire supports the OneXPlayer X2 Mini Pro (AMD Strix Halo). It is detected
by its firmware's model name, so there is nothing to select. Everything here
was measured on one machine running Linux 7.3 with BIOS 0.22. A different
kernel or BIOS may behave differently.

## Compatibility

| Feature | Status | Notes |
|---|---|---|
| **TDP** | Yes | One slider, like OneXConsole's: 10–85 W on AC and at most 55 W on battery. Needs the `ryzen_smu` module ([below](#tdp-needs-ryzen-smu)). |
| **Firmware power profiles** | Without ryzen_smu | Low power, balanced and performance, offered only when there is no TDP slider ([below](#tdp-needs-ryzen-smu)). |
| **Saved custom profiles** | Yes | Save the current TDP and undervolt under a name and load them back. Moving the sliders never changes a saved profile. |
| **AC / battery autoswitch** | Yes | A profile for each power source; in the drawer once switched on in Settings → Quickbar. |
| **CPU boost** | Yes | Put back at every boot. |
| **CPU undervolt** | Yes | Curve Optimizer, 0 to −40, a setting like the TDP slider ([below](#undervolt)). Needs the `ryzen_smu` module. |
| **Fan speed** | Yes | Shown in the GUI and in `voltaire status`. |
| **Fan curves** | No | The embedded controller keeps its own fan curve ([why](#fan-control)). |
| **RGB lighting** | No | The joystick rings are not supported yet ([why](#joystick-lighting)). |
| **Battery** | Yes | Level, charge and discharge rate, time estimate and pack health. |
| **Charge limit** | No | The kernel's threshold does not hold on this board ([why](#charge-limit)). |
| **Brightness and volume** | Yes | In the drawer and on the Dashboard, on any desktop and in Steam Gaming Mode. |
| **Refresh rate** | Yes | The panel's rate, and a rate per power source ([KDE Plasma only](/devices/#refresh-rate)). |
| **HDR** | No | voltaire has no HDR switch on any device ([why](/devices/#hdr)); use your desktop's display settings. |
| **Hardware buttons** | Yes | The Keyboard button opens voltaire by default; the OneXPlayer or Home button can be chosen instead ([below](#buttons-and-inputplumber)). |
| **Controller vibration** | Yes | Off to 5, in the drawer, on the Settings tab or with `voltaire rumble`. Put back after a reboot or a suspend. |
| **Firmware switches** | None | ([why](#firmware-settings)) |
| **Telemetry** | Yes | Temperatures, package, GPU and battery power, CPU and GPU load and clocks, memory and network, with up to an hour of history. No NPU ([why](#npu-telemetry)). |

The drawer starts with the TDP slider, CPU boost, the screen's brightness, the
volume, the controller's vibration strength and the refresh rate. The saved
profiles and autoswitch are a switch away in Settings → Quickbar, where you can
also rearrange it.

The vibration strength needs `sudo voltaire setup` (run again if you installed
voltaire before this setting existed): the kernel's attribute is root-only
until setup grants it.

## TDP needs ryzen_smu

Linux has no power-limit interface for this machine yet. voltaire sets the
limits (and the [undervolt](#undervolt)) through the SMU, using the out-of-tree
[`ryzen_smu`](https://github.com/amkillam/ryzen_smu) module: amkillam's fork,
since the original does not support Strix Halo. It is the same module that
undervolting uses on other machines.
[Installation](/installation#optional-ryzen_smu-kernel-module) has
the packages. On Arch and CachyOS:

```sh
yay -S ryzen_smu-dkms-git
sudo voltaire setup        # grants voltaire's group access to the module's files
```

The module loads by itself at boot once installed. Reboot, or load it with
`sudo modprobe ryzen_smu` and restart voltaire (`systemctl --user restart
voltaire`): the daemon checks for the module when it starts.

**Without the module** there is no TDP slider and no undervolt. voltaire says
what to install where the slider would be, in the Dashboard, `voltaire status`,
`voltaire tdp` and `voltaire undervolt`. The machine's three firmware power profiles are offered instead, and are
the only power control left. Measured under a full CPU load:

| Profile | Sustained power |
|---|---|
| Low Power | about 33 W |
| Balanced | about 61 W |
| Performance | about 61 W (the same thermal ceiling as Balanced) |

## How the slider behaves

- **Nothing is written until you first move it.** Until then the machine runs
  on the firmware's own limits.
- **One value for both power sources.** Set 70 W on AC and the machine runs at
  55 W on battery and goes back to 70 W when you plug in. A value set on
  battery is kept for AC too.
- **It survives what resets it.** The embedded controller rewrites the limits
  every time the charger is plugged in or pulled. voltaire puts your value back
  about two seconds later, and again after a suspend and at every boot.
- **It is not a profile.** If a saved profile is running, moving the slider
  leaves the profile and keeps the profile's own TDP as it was. To change a
  profile's TDP, edit it on the Profiles tab, or use `voltaire tdp --set N
  --profile NAME`.
- **The firmware profile stays on Balanced.** The firmware's Low Power profile
  caps the machine at about 33 W whatever the slider says. While the slider
  exists, voltaire therefore keeps Balanced selected and puts it back if
  another program (power-profiles-daemon on low battery, for example) changes
  it.
- `voltaire tdp --reset` forgets your value and writes the firmware's preset
  for the current power source: 80 W sustained on AC and 50 W on battery.

From the command line, `voltaire tdp --set 30` is the slider, and
`voltaire tdp --get` shows what is in force.

## Undervolt

The Curve Optimizer undervolt works as on the ROG Flow Z13, through the same
`ryzen_smu` module the TDP slider needs, and on this machine it behaves like
the slider: **one setting**, not part of a profile. Set it on the Dashboard's
POWER card, in the drawer (switch it on in Settings → Quickbar) or with
`voltaire undervolt --set -10`.

- **Nothing is written until you set one**, and `0` or `voltaire undervolt
  --reset` clears it.
- **It stays when the TDP moves.** The setting runs on the firmware profile
  and under any saved profile that has no undervolt of its own.
- **A saved profile can carry its own**, edited on the Profiles tab; it wins
  while that profile runs, and leaving the profile puts your setting back.
  Moving the undervolt slider while a saved profile runs leaves the profile,
  as the TDP slider does.
- **It is put back** at every boot and after a suspend, since the chip forgets
  it at both.
- `voltaire tdp --reset` leaves it alone; `voltaire tuning --reset` clears it
  along with everything else.

Start small. Too large an offset makes the machine unstable or crash, and the
point where that happens differs from chip to chip.

## Buttons and InputPlumber

The three buttons above the screen reach voltaire directly, or through
InputPlumber when it manages the controller, which it does as soon as it is
installed. [Using InputPlumber](/inputplumber) explains what voltaire
changes there. In short, the button that opens voltaire stops doing its
InputPlumber job while it does.

## Not supported yet, and why

### Joystick lighting

The RGB rings around the sticks cannot be controlled yet, and the cause is in
the kernel. Linux's `hid-oxp` driver registers a lighting device for them
(`oxp:rgb:joystick_rings`), but it speaks the lighting protocol of older
OneXPlayer controllers. The X2 controllers use a newer one, so the controller
ignores what the driver sends: on Linux 7.3, setting an effect or a speed
reports success and reads back unchanged, with or without InputPlumber
running. Other tools that go through the kernel driver are in the same
position.

A patch series adding the X2 family's protocol to `hid-oxp`, the rings
included, was posted to the kernel's input mailing list in September 2026
([LWN](https://lwn.net/Articles/1093595/)). It is not merged as of Linux
7.4-rc3. Once a kernel with it is released, voltaire will add a lighting
driver for the rings, and the lighting control will take its place in the
drawer (the default layout already lists it). Until then voltaire shows no
lighting control on this machine, rather than one that does nothing.

### Fan control

The fan follows the embedded controller's own curve, which voltaire leaves in
charge. The kernel's `oxp_ec` driver can only set one fixed fan speed, with no
curve that rises with temperature, and a fixed speed is either louder than
needed at idle or too slow under load. voltaire therefore only shows the
speed.

### Charge limit

The kernel offers a charge threshold on this board, but on Linux 7.3 it does
not hold: with the threshold at 62 %, the pack charged to 96 %. The driver
uses the embedded-controller registers of an older OneXPlayer model; Linux 7.4
gives the OneXPlayer APEX, which uses the same system board, registers of its
own, and this machine may need the same. voltaire offers no charge limit until a kernel's threshold is shown
to work, because a limit that is not enforced is worse than none.

### Firmware settings

ASUS machines expose BIOS settings (such as the Z13's boot sound) to Linux
through a firmware-settings interface. Linux has no such interface for this
machine (checked on 7.3), so there are no switches on the Settings tab.

### NPU telemetry

The NPU's driver cannot load while the kernel runs with `amd_iommu=off`, which
this BIOS needs for suspend to work ([below](#suspend)). Without the driver
there is nothing to read. The NPU card appears by itself if a later BIOS or
kernel makes the parameter unnecessary.

## Suspend

On BIOS 0.22 with Linux 7.3, suspend hard-locked the machine (screen off, fan
running, no wake) until two kernel parameters were added:

```
amd_iommu=off mem_sleep_default=s2idle
```

With them, suspend reaches the deepest idle state and resumes normally. The
cost is the NPU: its driver needs the IOMMU. This is a firmware or kernel
problem, not voltaire's, and newer BIOS or kernel versions may not need it.
