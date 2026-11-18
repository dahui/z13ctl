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
| `method` | The power-limit driver: `asus-nb-wmi-ppt` (uses asus-armoury's attributes where they are writable), or `amd-smu`, voltaire's generic AMD driver for machines with no vendor interface: STAPM (`pl1`), slow (`pl2`) and fast (`pl3`) PPT written to the SMU through the `ryzen_smu` module (ryzenadj's messages; a CPU without a known command is refused before any write). `amd-smu` needs exactly one `stock_ppt` row, the firmware profile it runs under, and reads nothing back from the SMU: it reports what it wrote, or the firmware's row after a power-source change. Without `ryzen_smu` loaded the device has no power control, and `device-get`'s `unavailable` list says what to install (naming the fork the CPU needs, amkillam's for Strix Halo). |
| `tdp_min`, `tdp_max_safe`, `tdp_max_forced` | The sustained limit's range, and the safe maximum above which a request needs `--force` and the fan floor. `0 < tdp_min < tdp_max_safe <= tdp_max_forced`. A kernel that reports its own bounds replaces `tdp_min`/`tdp_max_forced`. |
| `tdp_default` | Optional starting value for a new custom TDP, within `tdp_min`..`tdp_max_safe`. |
| `floor_curve` | `[temp, pwm]` pairs the fans must hold, per point, while the sustained limit exceeds `tdp_max_safe`. Needs a `[fans]` block. Leave it out on a device that needs no floor. |
| `boot_cache_pl1` | The PL1 the attributes read after boot before anything writes them (5 on the Z13's asus-nb-wmi attributes). A readback equal to it describes no limit in force. Optional. |
| `limits` | Which settable limits to offer, in order, and their wording: `[[power.limits]]`. |
| `stock_ppt` | Each firmware profile's own limits, as a row. |
| `tdp_max_battery` | Optional cap on the sustained limit while on battery, within `tdp_min`..`tdp_max_forced` (55 W on the OneXPlayer X2 Mini Pro, OneXConsole's). |
| `stock_ppt_battery` | Optional: the firmware's own rows on battery, where they differ from `stock_ppt` (a OneXPlayer's EC writes a battery preset on every unplug). One row per `stock_ppt` row. |
| `slider` | Optional, `true` for the one-slider model: setting a TDP while no custom profile is active applies it and remembers it as a device setting instead of creating the `custom` profile, and the daemon re-applies it at boot, on resume and after a charger edge. Nothing is written until the user first sets one. Named custom profiles still hold their own TDP. |

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
| `pin` | Optional: the one firmware profile the daemon holds. When anything else selects another (power-profiles-daemon on low battery, a stray tool), the daemon puts it back and logs that it did. For a device whose firmware profiles cap power below what its TDP control offers. Must be in `names`, needs a `[power]` block, and only the pin needs a `stock_ppt` row. While the power control exists only the pin is offered. When it is unavailable on the machine (an `amd-smu` device without `ryzen_smu`), the pin is dropped and every name in `names` is offered, so list the profiles that are worth offering as the fallback there. |

## `[lighting]`: RGB

| Key | Meaning |
|---|---|
| `method` | `aura-hid`. |
| `zones` | `[[lighting.zones]]`, one per addressable zone. |
| `modes`, `speeds` | Optional subsets of the protocol's modes and speeds, in the order to offer them. Absent means all of them. What each mode takes (colours, speed) is the protocol's to say, so it is not here. |

Each `[[lighting.zones]]` has a `name` (the wire name, which must not start
with `/`), a `label`, the `usb` `vendor:product` of the HID device that carries
it, the `interface` on that device carrying the lighting reports, and its
protocol `zone` byte.

`interface` is the USB `bInterfaceNumber`, and it is required because it is
the only interface `voltaire setup` grants. A keyboard exposes several, one of
which is how it types: granting the whole device by `vendor:product` hands
every process in the group its raw keystrokes over hidraw. Find it with
`cat /sys/class/hidraw/hidraw*/device/../bInterfaceNumber` beside each node's
`report_descriptor`, and pick the one that carries the lighting report (on the
Aura protocol, report ID `0x5d`). The Z13's is interface `0` on both devices;
the cover's interface `2` is its typing keyboard.

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
| `fans` | A read-only fan speed source: the hwmon device's `name` (`oxp_ec`), whose `fanN_input` files are charted. For a device whose fans voltaire reads but does not control; with a `[fans]` section the fan driver reports the speed instead. |
| `history_seconds` | How much history the daemon keeps (default 3600, one hour at 1 Hz). |

Declare a source only if the driver reads it. A declared source with nothing
behind it draws a chart flat at zero, which reads as a measurement; the daemon's
declaration guards fail in both directions. A declared source whose hardware is
absent at runtime is dropped from `device-get`.

## `[[buttons]]`: the hardware buttons

The buttons a user may open voltaire with, one block per button. The user
picks among them (Settings, or `voltaire button --set`); nothing ever reads a
keyboard to learn a key, so this list is the whole of what can be chosen.

List only buttons you have captured on the machine itself (`evtest` on the
node, pressing the button) and that do nothing else there. A node's capability
bitmap is not evidence: the Z13's hotkey node advertises 38 keys, asus-wmi's
keymap for every ASUS model, and the kernel acts on some of them itself. The
watcher never grabs the node, so a key that already has a job would do both.

| Key | Meaning |
|---|---|
| `id` | Stable name stored with the user's choice (`armoury-crate`): lowercase letters, digits, hyphens. Never rename one that has shipped. |
| `label` | What the button is called, mid-sentence (`Armoury Crate button`). Required: the choice list shows it. |
| `default` | `true` on the one button that opens voltaire out of the box. At most one; with none, no button opens it until the user picks one. |
| `method` | `evdev-key` (one key on an input node), `evdev-chord` (several keys held together, as a handheld's buttons send key combinations), or `hidraw-report` (a byte in a vendor HID report, where a button never becomes an input event). |
| `device` | `evdev-*`: the input device's sysfs name (`Asus WMI hotkeys`). |
| `keycode` | `evdev-key`: the key to watch (`202`, KEY_PROG3). |
| `keys` | `evdev-chord`: every key of the chord (`[29, 56, 125]`, Ctrl+Alt+Meta). A press is the key-down that completes the chord; it re-arms when any key is released. All must be held, so one of them typed alone is not a press. |
| `usb` | `hidraw-report`: the HID device's `vendor:product`. `evdev-*`: optional, with `interface`, to name the node by its USB interface as well as its name, where one name is not unique (the OneXPlayer X2 Mini Pro's controller registers `HID 1a86:fe00` on interfaces 0 and 1). The grant is then written for that interface only. |
| `interface` | `hidraw-report`: its USB interface number, the only one granted (as for lighting zones; see `[lighting]`). `evdev-*`: required with `usb`. |
| `report_id` | `hidraw-report`: the report's first byte, or `0` for a device with unnumbered reports. |
| `match` | `hidraw-report`: the bytes of a press report, as `[offset, value]` pairs that must all hold (offsets count the report ID; 0–63). Each matching report is one press, so where the device reports a release as well, include the byte that tells them apart: the OneXPlayer X2 Mini Pro's Home button sends `…24…01…` then `…24…02…` per tap, and matching the button id alone reads one tap as a double press. |
| `inputplumber` | Optional: the gamepad button capability [InputPlumber](https://github.com/ShadowBlip/InputPlumber) gives this button on this machine (`QuickAccess2`), from its capability map for the device. When InputPlumber manages the device it grabs and hides the node the method reads, so the button would be silent; with this key the daemon instead loads a mapping from that capability to its own DBus action into InputPlumber's running profile (never a file), only for the button chosen to open voltaire, and takes it out again when the choice changes or the daemon stops. The button's previous mapping in that profile is restored. |
| `keyboard_node` | Only for a button that arrives on a node udev classes as a typing keyboard, such as a handheld whose buttons send key chords on its AT keyboard. The value says why that node is safe on this model. See below. |

### Typing keyboards are refused

`voltaire setup` grants the group read access to each button's input node, and
read access to a typing keyboard would let any process in the group see every
keystroke. Two guards refuse that:

- **The udev grant** carries `ENV{ID_INPUT_KEYBOARD}!="1"`, udev's own
  classification of a typing keyboard. A name that matches a keyboard grants
  nothing. Grants are read-only (`0640`): a writable input node accepts
  injected events.
- **The watcher** applies the same test to the node's key bitmap before it
  opens it, and refuses a keyboard. A grant widened by hand still does not
  make voltaire read one.
- **hidraw** has its own form of the hole, since a keyboard's hidraw node
  carries its raw keystrokes. A `hidraw-report` button names one interface,
  and the watcher reads that node's report descriptor first and refuses a
  top-level Keyboard or Keypad collection. `voltaire setup` checks the same on
  the machine it runs on and removes the group from any granted interface that
  turns out to be a keyboard.

`keyboard_node = "<why>"` opts one button's node out of both, for a machine
whose keyboard node carries nothing but button chords. Its grant is then
written scoped to this file's `[[device.match]]` DMI patterns
(`ATTR{[dmi/id]sys_vendor}`, `ATTR{[dmi/id]product_name}`), so it can never
apply on another machine, and those patterns may not contain udev glob
characters (`*?[|`). Validation refuses the key on anything but an evdev
method.

## `[quickbar]`: the default drawer

| Key | Meaning |
|---|---|
| `controls` | The drawer's sections for a user who has not customized it, in order: the control IDs [GUI configuration](/voltaire/gui/configuration/) lists (`tdp`, `profile`, `cpu-boost`, …), including the ones that are otherwise off by default. A section the device cannot offer is dropped, so listing `lighting` before the RGB driver exists is harmless. A user's own choice (Settings → Quickbar, or `gui.toml`) still wins. Omit the block for the built-in default. |

A handheld wants different quick controls from a laptop: the OneXPlayer X2 Mini
Pro lists the TDP slider, CPU boost, the screen's brightness, the volume, the
controller's vibration strength and the refresh rate.

## `[rumble]`: controller vibration strength

| Key | Meaning |
|---|---|
| `method` | The driver: `hid-oxp`, the kernel's OneXPlayer controller driver, whose HID device carries `rumble_intensity`. The scale is the kernel's (`rumble_intensity_range`, 0–5 on the X2 Mini Pro), where 0 is off. |
| `usb` | The controller's USB identifier, `vvvv:pppp` in hex, so another device bound to the same driver is never the one written. |

The daemon records the value the user sets and puts it back at startup and
after a suspend. `voltaire setup` grants the attribute on every device hid-oxp
binds. The control is offered only while the controller is present; check that
the controller actually obeys the value (feel a rumble at two strengths)
before declaring it.

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
interface = 0

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

[[buttons]]
id      = "command-center"
label   = "Command Center button"
default = true
method  = "evdev-key"
device  = "Asus WMI hotkeys"
keycode = 202

[[buttons]]
id        = "armoury-crate"
label     = "Armoury Crate button"
method    = "hidraw-report"
usb       = "0b05:1abe"
interface = 2
report_id = 0x5a
match     = [[1, 0xa6]]

[[buttons]]
id            = "quick-access"
label         = "Quick Access button"
method        = "evdev-chord"
device        = "AT Translated Set 2 keyboard"
keys          = [29, 56, 125]
keyboard_node = "on the EX1 the AT keyboard carries only the button chords"
```
<!-- example-device:end -->

## Checking a new file

`make test` validates every embedded device file. It also checks that each
lighting zone and button the files name is granted in the packaged udev rules
(`TestEveryDeviceFileTargetIsGranted`), so a new device's hardware is
reachable on a package install. Run `sudo voltaire setup` after adding a file
on the machine itself, then `voltaire status` and `voltaire tdp --get` to see
what the daemon read.
