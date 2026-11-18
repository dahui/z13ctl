---
title: OneXPlayer X2 Mini Pro
description: What voltaire controls on the OneXPlayer X2 Mini Pro (Strix Halo), what it needs installed, and what does not work yet.
---

voltaire supports the OneXPlayer X2 Mini Pro (AMD Strix Halo). It is detected
by its firmware's model name, so there is nothing to select. Everything here
was measured on one machine running Linux 7.3 with BIOS 0.22. A different
kernel or BIOS may behave differently.

## What it controls

| | |
|---|---|
| **TDP** | One slider, like OneXConsole's: 10–85 W on AC and at most 55 W on battery. Needs the `ryzen_smu` module (below). |
| **Saved profiles** | Save the current TDP under a name and load it back. Moving the slider never changes a saved profile. |
| **CPU boost** | On or off; voltaire puts your choice back at every boot. |
| **Brightness and volume** | Sliders in the drawer and on the Dashboard, on any desktop and in Steam Gaming Mode. |
| **Refresh rate** | The panel's rate, and a rate per power source if you want one (KDE). |
| **Controller vibration** | How strongly the controller rumbles, from off to 5, in the drawer, on the Settings tab or with `voltaire rumble`. Put back after a reboot or a suspend. |
| **Fan speed** | Shown in the GUI and in `voltaire status`. Read-only: the embedded controller keeps its own fan curve. |
| **Battery** | Charge level, charge and discharge rate, time estimate and pack health. |
| **Telemetry** | Temperatures, package, GPU and battery power, CPU and GPU load and clocks, memory and network, with up to an hour of history. |
| **Buttons** | The Keyboard button opens voltaire by default. The OneXPlayer and Home buttons can be chosen instead in Settings, or with `voltaire button`. |

The drawer starts with the TDP slider, CPU boost, the screen's brightness, the
volume, the controller's vibration strength and the refresh rate. The saved
profiles and autoswitch are a switch away in Settings → Quickbar, where you can
also rearrange it.

The vibration strength needs `sudo voltaire setup` (run again if you installed
voltaire before this setting existed): the kernel's attribute is root-only
until setup grants it.

## TDP needs ryzen_smu

Linux has no power-limit interface for this machine yet. voltaire sets the
limits through the SMU, using the out-of-tree
[`ryzen_smu`](https://github.com/amkillam/ryzen_smu) module: amkillam's fork,
since the original does not support Strix Halo. It is the same module that
undervolting uses on other machines.
[Installation](/voltaire/installation/#optional-ryzen_smu-kernel-module) has
the packages. On Arch and CachyOS:

```sh
yay -S ryzen_smu-dkms-git
sudo voltaire setup        # grants voltaire's group access to the module's files
```

The module loads by itself at boot once installed. Reboot, or load it with
`sudo modprobe ryzen_smu` and restart voltaire (`systemctl --user restart
voltaire`): the daemon checks for the module when it starts.

**Without the module** there is no TDP slider. voltaire says what to install
where the slider would be, in the Dashboard, `voltaire status` and `voltaire
tdp`. The machine's three firmware power profiles are offered instead, and are
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

## Buttons and InputPlumber

The three buttons above the screen reach voltaire directly, or through
InputPlumber when it manages the controller, which it does as soon as it is
installed. [Using InputPlumber](/voltaire/inputplumber/) explains what voltaire
changes there. In short, the button that opens voltaire stops doing its
InputPlumber job while it does.

## Not supported yet

- **Fan control.** The fan follows the embedded controller's own curve. The
  kernel's `oxp_ec` driver can set a fixed fan duty but has no curve, so
  voltaire only shows the speed.
- **Charge limit.** The kernel offers a charge threshold on this board, but on
  Linux 7.3 it does not hold: the pack charged to 96 % with the threshold at
  62 %. voltaire offers no charge-limit control until a kernel's threshold is
  shown to work.
- **Joystick lighting.** The kernel registers the joystick rings' LED, but the
  X2 controllers use a newer lighting protocol than today's `hid-oxp` driver
  speaks, so the controller ignores it. The lighting control appears on its
  own once a kernel with the X2 support is installed.
- **NPU telemetry** is missing while the NPU driver cannot load, which it
  cannot under `amd_iommu=off` (below).

## Suspend

On BIOS 0.22 with Linux 7.3, suspend hard-locked the machine (screen off, fan
running, no wake) until two kernel parameters were added:

```
amd_iommu=off mem_sleep_default=s2idle
```

With them, suspend reaches the deepest idle state and resumes normally. The
cost is the NPU: its driver needs the IOMMU. This is a firmware or kernel
problem, not voltaire's, and newer BIOS or kernel versions may not need it.
