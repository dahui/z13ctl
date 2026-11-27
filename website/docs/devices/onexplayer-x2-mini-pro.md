---
title: OneXPlayer X2 Mini Pro
description: What voltaire controls on the OneXPlayer X2 Mini Pro (Strix Halo), what it needs installed, and what does not work yet.
---

# OneXPlayer X2 Mini Pro

On the OneXPlayer X2 Mini Pro (AMD Strix Halo) voltaire works like
OneXConsole: one power slider, an undervolt setting, and saved profiles when
you want them.

## At a glance

- **Detected as:** the X2 Mini Pro, by its firmware's model name.
- **Power:** one TDP slider, 10–85 W on the charger and at most 55 W on
  battery. Needs the `ryzen_smu` module ([below](#tdp-needs-ryzen-smu)).
- **Undervolt:** 0 to −40, a setting like the slider ([below](#undervolt)).
- **Fans:** speed shown; the machine keeps its own fan curve.
- **Buttons:** the Keyboard button opens voltaire; the OneXPlayer or Home
  button can be chosen instead.
- **Controller vibration:** off to 5.
- **Suspend** needs two kernel parameters ([below](#suspend)).

## Compatibility

| Feature | Status | Notes |
|---|---|---|
| **Power** | | |
| Power limits (TDP) | ⚠️ | One slider: 10–85 W on the charger, at most 55 W on battery. Needs `ryzen_smu` ([below](#tdp-needs-ryzen-smu)). |
| Firmware power profiles | ⚠️ | Low Power, Balanced and Performance, offered only without `ryzen_smu` ([below](#tdp-needs-ryzen-smu)). |
| Saved custom profiles | ✅ | Save the current power limit and undervolt under a name and load them back ([below](#saved-profiles)). |
| Switch profile on AC / battery | ✅ | Add it to the drawer in Settings → Quickbar. |
| CPU boost on/off | ✅ | Put back at every boot. |
| CPU undervolt | ⚠️ | 0 to −40, a setting like the slider. Needs `ryzen_smu` ([below](#undervolt)). |
| **Cooling** | | |
| Fan speed readout | ✅ | In the GUI and `voltaire status`. |
| Custom fan curves | ❌ | [Why](#fan-control). |
| Fan curve presets | — | No fan curves. |
| **Lighting** | | |
| RGB lighting | ❌ | The joystick rings are not supported yet ([why](#joystick-lighting)). |
| **Battery** | | |
| Charge level, rate, time left | ✅ | |
| Charge limit | ❌ | [Why](#charge-limit). |
| Full charge once | ❌ | Needs the charge limit. |
| **Display and sound** | | |
| Screen brightness and volume | ✅ | On any desktop and in Steam Gaming Mode. |
| Refresh rate | ⚠️ | The panel's rate, and a rate per power source ([KDE Plasma only](/devices/#refresh-rate)). |
| HDR on/off | ❌ | No HDR switch on any device; use your desktop's display settings ([why](/devices/#hdr)). |
| **Buttons and controller** | | |
| Hardware button opens voltaire | ✅ | Keyboard (the default), OneXPlayer or Home ([below](#buttons)). |
| Controller vibration strength | ✅ | Off to 5 ([below](#vibration)). |
| Controller power saving | — | |
| **Firmware settings** | | |
| Firmware switches | ❌ | [Why](#firmware-settings). |
| **Telemetry** | | |
| Temperatures, power, load, clocks, memory, network | ✅ | Including package, GPU and battery power. |
| NPU power and load | ❌ | [Why](#npu-telemetry). |
| Up to an hour of history | ✅ | |

## What it needs {#what-it-needs}

- **`sudo voltaire setup`**, once, and membership of the group it grants
  (`users` by default). The packages run setup for you; see
  [Installation](/installation#what-setup-does). Run it again after
  installing `ryzen_smu`, so voltaire can reach the module.
- **The `ryzen_smu` module** (the amkillam fork) for the TDP slider and the
  undervolt ([below](#tdp-needs-ryzen-smu)).
- **InputPlumber**, if installed, manages the controller, and the buttons then
  reach voltaire through it. That needs your user in the **`wheel`** or
  **`inputplumber`** group (check with `id`). See
  [InputPlumber](/guide/buttons-and-controllers#inputplumber).
- **Two kernel parameters for suspend**, without which the machine locks up
  when it sleeps ([below](#suspend)).

## TDP needs ryzen_smu {#tdp-needs-ryzen-smu}

Linux has no power-limit interface for this machine, so voltaire sets the
limits, and the [undervolt](#undervolt), through the separately installed
[`ryzen_smu`](https://github.com/amkillam/ryzen_smu) module. Use amkillam's
fork: the original does not support Strix Halo. On Arch and CachyOS:

```sh
yay -S ryzen_smu-dkms-git
sudo voltaire setup        # lets voltaire's group reach the module
```

The module loads by itself at boot once installed. Reboot, or load it with
`sudo modprobe ryzen_smu` and restart the daemon with
`systemctl --user restart voltaire`. Other distributions:
[Installation](/installation#ryzen-smu).

**Without the module** there is no TDP slider and no undervolt, and voltaire
says what to install where the slider would be. The machine's three firmware
profiles are offered instead and are the only power control:

| Profile | Sustained power under full load |
|---|---|
| Low Power | about 33 W |
| Balanced | about 61 W |
| Performance | about 61 W (the same thermal ceiling as Balanced) |

## How the slider behaves

- **Nothing is written until you first move it.** Until then the machine runs
  on its firmware's own limits.
- **One value for both power sources.** Set 70 W on the charger and the
  machine runs at 55 W on battery, then goes back to 70 W when you plug in.
- **It survives what resets it.** The machine rewrites its limits every time
  the charger is plugged in or pulled; voltaire puts your value back about two
  seconds later, and again after a suspend and at every boot.
- **The firmware profile stays on Balanced.** Low Power would cap the machine
  at about 33 W whatever the slider says, so while the slider exists voltaire
  keeps Balanced selected and puts it back if another program changes it.
- `voltaire tdp --reset` forgets your value and returns to the firmware's
  own limits: 80 W sustained on the charger and 50 W on battery.

From the command line, `voltaire tdp --set 30` is the slider, and
`voltaire tdp --get` shows what is in force.

## Saved profiles

A saved profile stores a power limit and an undervolt under a name; save the
current ones with `voltaire profile --save-as NAME` or from the Profiles tab,
and load them back from the drawer or with `voltaire profile --set NAME`.

The slider is not a profile. Moving it while a saved profile runs leaves the
profile and keeps the profile's own values as they were. To change a saved
profile, edit it on the Profiles tab or use
`voltaire tdp --set N --profile NAME`. The profile picker and autoswitch can be
added to the drawer in Settings → Quickbar.

## Undervolt

The Curve Optimizer undervolt goes through the same `ryzen_smu` module, and on
this machine it is a setting like the slider, not part of a profile. Set it on
the Dashboard, in the drawer (add it in Settings → Quickbar) or with
`voltaire undervolt --set -10`.

- **Nothing is written until you set one.** `0` or `voltaire undervolt
  --reset` clears it.
- **It stays when the slider moves**, and runs under any saved profile that
  has no undervolt of its own.
- **A saved profile can carry its own**, which wins while that profile runs.
- **It is put back** at every boot and after a suspend, since the chip forgets
  it at both.
- `voltaire tdp --reset` leaves it alone; `voltaire tuning --reset` clears it
  along with everything else.

::: warning Start small
Too large an offset makes the machine unstable or crash, and the point where
that happens differs from chip to chip.
:::

## Buttons

The three buttons above the screen can open voltaire: the **Keyboard** button
by default, or the **OneXPlayer** or **Home** button, chosen in Settings or
with `voltaire button --set home` (`voltaire button --list` shows the
choices). When InputPlumber manages the controller, the
chosen button stops doing its InputPlumber job (by default, the Keyboard
button takes screenshots) while it opens voltaire. See
[Buttons and controllers](/guide/buttons-and-controllers#opening-voltaire).

## Vibration

Set the controller's vibration strength from off to 5 in the drawer, on the
Settings tab or with `voltaire rumble --set 3`. voltaire puts it back after a
reboot or a suspend. It needs `sudo voltaire setup`, so run setup again if you
installed voltaire before this setting existed. See
[Vibration](/guide/buttons-and-controllers#vibration).

## Suspend

Suspend locks this machine up (screen off, fan running, no wake) unless the
kernel is started with two parameters:

```
amd_iommu=off mem_sleep_default=s2idle
```

Add them to your bootloader's kernel command line (on CachyOS with Limine,
`KERNEL_CMDLINE` in `/etc/default/limine`, then `sudo limine-update`). With
them, suspend and resume work normally. The cost is the NPU, whose driver
needs what the first parameter turns off. This is a firmware or kernel
problem, and a later BIOS or kernel may not need them.

## Not supported yet, and why

### Joystick lighting

The RGB rings around the sticks need a kernel driver update that is not in
released kernels yet: the current driver speaks an older OneXPlayer lighting
protocol, which the X2's controller ignores. Once a kernel with the update is
released, voltaire will add support, and the lighting control will appear in
the drawer by itself.

### Fan control

The fan follows the machine's own curve, which voltaire leaves in charge.
Linux can only set this fan to one fixed speed, with no curve that rises with
temperature, and a fixed speed is either louder than needed at idle or too
slow under load. voltaire therefore only shows the speed.

### Charge limit

The kernel offers a charge limit on this machine, but it does not hold: the
battery charges past it. The fix is a small kernel change that has been
submitted but is not in released kernels yet. voltaire will offer the charge
limit on kernels that carry it, since a limit that is not enforced is worse
than none.

### Firmware settings

Linux has no interface to this machine's firmware settings, so there are no
switches on the Settings tab.

### NPU telemetry

The NPU's driver cannot load while suspend needs `amd_iommu=off`
([above](#suspend)), so there is nothing to read. The NPU card appears by
itself if a later BIOS or kernel makes the parameter unnecessary.

## Good to know

- **The battery has its own recharge rule.** Once full, it does not start
  charging again until it falls below 90 %.
- **Default drawer:** the TDP slider, CPU boost, brightness, volume, vibration
  strength and refresh rate. The saved profiles, autoswitch and undervolt are
  in Settings → Quickbar, where you can also rearrange it.
