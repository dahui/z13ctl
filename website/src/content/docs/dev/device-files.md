---
title: Device files
description: The TOML that describes one family of machines — every key, what it means, and a complete annotated example.
---

A device file describes one family of machines: how to recognise it, which
kernel interface drives each capability, and the numbers the kernel cannot
report. Device files live in `internal/device/devices/` and are embedded in the
binary; the daemon picks the one whose `[[device.match]]` fits the running
machine's DMI identity.

Adding a machine whose hardware an existing driver already speaks is a device
file and nothing else. A new kernel interface also needs a driver:
[Writing a driver](/voltaire/dev/writing-a-driver/).

## Ground rules

- **Absence is the answer.** Leave out a block and the machine does not have
  that capability: no control is drawn and no command is offered. A block that
  declares nothing (an empty `[toggles]`, a `[battery]` with neither a charge
  limit nor health) is refused, so absence is said by omission.
- **The kernel wins where it can answer.** A value the kernel reports (the
  fan-curve point count, asus-armoury's power-limit ranges, which limits exist)
  overrides the file's. The file supplies what the kernel cannot say, and the
  fallback when it is not there to ask.
- **Every number is a measurement.** Record next to each value how it was
  established (the Z13's file does). A device file is reviewed like code, and
  an unexplained number cannot be.
- **Strict parsing.** An unknown key is an error, not a typo silently ignored.
  `Config.Validate` checks the rest: ranges, orderings and cross-references
  between blocks.

## `[device]`: identity

| Key | Required | Meaning |
|---|---|---|
| `id` | yes | Stable identifier (`asus-rog-flow-z13-2025`). Logs and bug reports key on it; never reuse or rename one. |
| `model` | yes | Short hardware name for display (`GZ302`). |
| `name` | no | Display name (`ASUS ROG Flow Z13 (2025)`): help text, the GUI and `device-get`'s `name`. Clients fall back to `model`. |
| `match` | yes | One or more `[[device.match]]` DMI patterns; any one matching claims the machine. |

Each `[[device.match]]` has a `vendor` (exact `sys_vendor`) and exactly one of
`product` (exact `product_name`) or `product_prefix`. Prefix is the common
case: vendors encode SKU suffixes into `product_name`, and the Z13's reads
`ROG Flow Z13 GZ302EA_GZ302EA`.

## `[fans]`: the fan curve

| Key | Meaning |
|---|---|
| `method` | The fan driver: `asus-nb-wmi`. |
| `points` | Points per curve. The kernel's count wins where the driver can enumerate it; on a mismatch the file's shape stands (the presets and floor are sized for it) and every curve write is refused until they agree. |
| `temp_min`, `temp_max` | The curve editor's temperature axis, in °C. Must be at least `points`−1 degrees wide. |
| `labels` | A name for each fan, in reading order. Without it the kernel's `fan<N>_label` is tidied, else "Fan N". |
| `rpm_max` | Measured top speed; a chart axis hint only. |
| `sleep_release_settle_ms` | How long the daemon holds a suspend after handing the fans back to firmware, for a machine whose EC wakes a suspend that starts too soon after (issue #24). Under 5000; 0 or absent holds nothing. |
| `presets` | Named starting-point curves, `[[fans.presets]]`. |

Each `[[fans.presets]]` has a `name` (unique, case-insensitively), a `label`, a
`description`, and a `curve` of exactly `points` `[temp, pwm]` pairs, with
temperatures strictly increasing and PWM never decreasing. A preset is applied
only when a user picks it. No profile switch selects one.

## `[power]`: power limits

| Key | Meaning |
|---|---|
| `method` | The power-limit driver: `asus-nb-wmi-ppt` (uses asus-armoury's attributes where they are writable). |
| `tdp_min`, `tdp_max_safe`, `tdp_max_forced` | The sustained limit's range, and the safe maximum above which a request needs `--force` and the fan floor. `0 < tdp_min < tdp_max_safe <= tdp_max_forced`. A kernel that reports its own bounds replaces `tdp_min`/`tdp_max_forced`. |
| `tdp_default` | Optional starting value for a new custom TDP, within `tdp_min`..`tdp_max_safe`. |
| `floor_curve` | `[temp, pwm]` pairs the fans must hold, per point, while the sustained limit exceeds `tdp_max_safe`. Needs a `[fans]` block. Leave it out on a device that needs no floor. |
| `boot_cache_pl1` | The PL1 the attributes read after boot before anything writes them (5 on the Z13's asus-nb-wmi attributes). A readback equal to it describes no limit in force. Optional. |
| `limits` | Which settable limits to offer, in order, and their wording: `[[power.limits]]`. |
| `stock_ppt` | Each firmware profile's own limits, as a row. |

Each `[[power.limits]]` has an `id` (`pl1`, `pl2` or `pl3`, the `tdp`
command's fields), an optional `label` and an optional `description`. Without
the list the driver offers every limit the kernel exposes, in its own words;
with it, a listed limit the kernel lacks is still dropped, and an entry with no
wording takes the driver's. `pl1` is required when the list is given.

`[power.stock_ppt]` has one row per entry in `profiles.names`, no more and no
fewer. Each row is an inline table in watts: `pl1`, `pl2`, `fppt` (PL3), and
`apu` and `platform`, the APU and platform sPPT limits on interfaces that
expose them (written verbatim, never mirrored from `pl2`). Switching to a
firmware profile writes its row first (bringing a high custom limit down before
the fans lose their floor), then releases the fans, which makes the firmware
re-apply its own limits. The default profile's `pl1` must be within
`tdp_max_safe`.

## `[profiles]`: firmware profiles

| Key | Meaning |
|---|---|
| `method` | `platform-profile`. |
| `names` | The firmware profiles to offer, in order. Each must be a kernel `platform_profile` name; the kernel's own `choices` filter it at runtime. |
| `handler` | The platform-profile class device that owns the profile, by its `name` (`asus-wmi`). Empty picks one heuristically. |
| `default` | Where `tdp --reset` and `tuning --reset` land. Must be in `names`. |
| `labels` | Display labels by name, overriding the built-in spellings. |

## `[lighting]`: RGB

| Key | Meaning |
|---|---|
| `method` | `aura-hid`. |
| `zones` | `[[lighting.zones]]`, one per addressable zone. |
| `modes`, `speeds` | Optional subsets of the protocol's modes and speeds, in the order to offer them. Absent means all of them. What each mode takes (colours, speed) is the protocol's to say, so it is not here. |

Each `[[lighting.zones]]` has a `name` (the wire name, which must not start
with `/`), a `label`, the `usb` `vendor:product` of the HID device that carries
it, and its protocol `zone` byte.

## `[toggles]`: firmware switches

| Key | Meaning |
|---|---|
| `method` | `asus-armoury`. |
| `entries` | `[[toggles.entries]]`, the wording for each toggle. |

Each entry has an `id` (the attribute name), a `label`, a `description` to
show beside the switch, and `hidden` (true) to suppress one the kernel offers.
Which toggles exist is the kernel's answer filtered through the driver's
allowlist, which lives in code because some attribute names on other models can
black-screen a machine. The file supplies the words.

## `[battery]`: charging

| Key | Meaning |
|---|---|
| `method` | `power-supply`. |
| `charge_limit` | Whether the charge threshold is settable. Defaults to true when the block is present. |
| `charge_limit_min`, `charge_limit_max` | The threshold's range in percent (the kernel publishes none). Default 40–100. |
| `health` | Whether state of health is reported. |
| `charger` | Where the charger kind (adapter or USB-C) comes from: absent for the generic `power_supply` view, `asus-armoury` for ASUS's `charge_mode`. |

## `[cpu]`: CPU controls

| Key | Meaning |
|---|---|
| `boost` | The driver behind the global boost switch: `cpufreq`. |

## `[undervolt]`: CPU undervolt

| Key | Meaning |
|---|---|
| `method` | `ryzen-smu-co` (the AMD Curve Optimizer). |
| `min`, `max` | The offset's range: `min <= max <= 0`. |
| `label`, `short_label`, `unit`, `description` | Optional wording; each empty one takes the driver's ("CPU Curve Optimizer", "CO", unitless). |

## `[telemetry]`: readings

| Key | Meaning |
|---|---|
| `method` | `hwmon-rapl`. |
| `power_draw` | The package-power source: `rapl`. |
| `gpu`, `cpu_stats`, `npu`, `net` | The expanded sources: `amdgpu`, `procfs`, `amdxdna`, `procfs`. |
| `history_seconds` | How much history the daemon keeps (default 3600, one hour at 1 Hz). |

Declare a source only if the driver reads it. A declared source with nothing
behind it draws a chart flat at zero, which reads as a measurement; the daemon's
declaration guards fail in both directions. A declared source whose hardware is
absent at runtime is dropped from `device-get`.

## `[button]`: the hardware button

| Key | Meaning |
|---|---|
| `method` | `evdev-key`. |
| `device` | The input device's sysfs name (`Asus WMI hotkeys`). |
| `keycode` | The key to watch (`202`, KEY_PROG3). |
| `kind` | The event kind the daemon receives for a press. |
| `label` | What the button is called, mid-sentence (`Armoury Crate button`). |

## A complete example

A fictional device that uses every block. The numbers are illustrative, not a
real machine's: copy the shape, then measure your own. This block is checked by
the test suite, which parses it and assembles it with the real drivers, so it
cannot drift from the schema.

<!-- example-device:start -->
```toml
[device]
id    = "example-handheld"
model = "EX1"
name  = "Example Handheld"

[[device.match]]
vendor         = "Example Corp."
product_prefix = "Example Handheld EX1"

[fans]
method                  = "asus-nb-wmi"
points                  = 4
temp_min                = 40
temp_max                = 100
labels                  = ["Fan"]
rpm_max                 = 6000
sleep_release_settle_ms = 0

[[fans.presets]]
name        = "quiet"
label       = "Quiet"
description = "Stopped at idle, a late ramp."
curve       = [[40, 0], [60, 0], [80, 140], [100, 255]]

[power]
method         = "asus-nb-wmi-ppt"
tdp_min        = 5
tdp_max_safe   = 25
tdp_max_forced = 30
tdp_default    = 15
boot_cache_pl1 = 0
# Fans at half speed or more while the sustained limit is above 25 W.
floor_curve    = [[40, 127], [70, 190], [85, 255]]

# Offer the sustained limit and one boost limit, in this device's words.
[[power.limits]]
id          = "pl1"
label       = "TDP"
description = "The power the processor may draw for as long as it likes."

[[power.limits]]
id          = "pl3"
label       = "Boost"
description = "The peak the processor may reach for a moment."

[power.stock_ppt]
quiet       = { pl1 = 10, pl2 = 12, fppt = 15, apu = 12, platform = 12 }
balanced    = { pl1 = 15, pl2 = 20, fppt = 25, apu = 20, platform = 20 }
performance = { pl1 = 25, pl2 = 28, fppt = 30, apu = 28, platform = 28 }

[profiles]
method  = "platform-profile"
names   = ["quiet", "balanced", "performance"]
handler = "asus-wmi"
default = "balanced"
labels  = { quiet = "Silent" }

[lighting]
method = "aura-hid"
modes  = ["static", "breathe", "rainbow"]
speeds = ["slow", "normal", "fast"]

[[lighting.zones]]
name  = "sticks"
label = "Stick rings"
usb   = "0b05:1abe"
zone  = 0

[toggles]
method = "asus-armoury"

[[toggles.entries]]
id          = "boot_sound"
label       = "Startup sound"
description = "Play a sound when the device powers on."
hidden      = false

[battery]
method           = "power-supply"
charge_limit     = true
charge_limit_min = 50
charge_limit_max = 100
health           = true
charger          = "asus-armoury"

[cpu]
boost = "cpufreq"

[undervolt]
method      = "ryzen-smu-co"
min         = -30
max         = 0
label       = "CPU Curve Optimizer"
short_label = "CO"
unit        = ""
description = "All-core offset; negative lowers the voltage curve."

[telemetry]
method          = "hwmon-rapl"
power_draw      = "rapl"
gpu             = "amdgpu"
cpu_stats       = "procfs"
npu             = "amdxdna"
net             = "procfs"
history_seconds = 1800

[button]
method  = "evdev-key"
device  = "Asus WMI hotkeys"
keycode = 202
kind    = "armoury-crate"
label   = "Command Center button"
```
<!-- example-device:end -->

## Checking a new file

`make test` validates every embedded device file. It also checks that each
lighting zone and button the files name is granted in the packaged udev rules
(`TestEveryDeviceFileTargetIsGranted`), so a new device's hardware is
reachable on a package install. Run `sudo voltaire setup` after adding a file
on the machine itself, then `voltaire status` and `voltaire tdp --get` to see
what the daemon read.
