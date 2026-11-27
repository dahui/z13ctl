---
title: Socket protocol
description: The voltaire daemon's JSON socket protocol, for writing clients.
---

# Socket protocol

This page is for people writing their own client for the voltaire daemon. The
CLI and the GUI are clients of the same protocol; Go programs should use the
[`api` module](/dev/go-api) instead of speaking it directly.

## Wire format

The daemon listens on a Unix socket, `$XDG_RUNTIME_DIR/voltaire/voltaire.sock`
(see [Running without systemd](/reference/daemon#running-without-systemd) for
the fallback and the legacy path).

The wire format is newline-delimited JSON: one request object per line, one
response object per line. Every response carries `ok` (bool) and, on failure,
`error` (string). Commands that return data use `value` (a string,
JSON-encoded where the payload is structured), or a typed field where the
tables below name one.

Connections are single-shot — the daemon replies once and closes — except
`subscribe` and `gamepad-intercept`, which stay open and stream events.

## Device capabilities

| Command | Request | Response |
|---|---|---|
| Device document | `{"cmd":"device-get"}` | `ok`, `device` |
| Telemetry history | `{"cmd":"telemetry-history","seconds":60}` | `ok`, `history` |

`device-get` returns the capability/limits document for the device the daemon
was assembled with — what the machine can do, and the bounds to validate and
render controls against, instead of hardcoding one device's numbers:

```json
{"ok":true,"device":{
  "id":"asus-rog-flow-z13-2025","model":"GZ302","name":"ASUS ROG Flow Z13 (2025)",
  "fans":{"points":8,"temp_min":35,"temp_max":105,"pwm_max":255,
          "labels":["Fan 1","Fan 2"],"rpm_max":8900,
          "presets":[{"name":"quiet","label":"Quiet","description":"Fans stopped until 60°C, ...",
                      "curve":[{"temp":35,"pwm":0},{"temp":50,"pwm":0}, "..."]}, "..."]},
  "power":{"tdp_min":28,"tdp_max_safe":75,"tdp_max_forced":80,
           "interface":"asus-armoury",
           "pl2":{"min":32,"max":92},"pl3":{"min":45,"max":93},
           "limits":[{"id":"pl1","label":"PL1 (SPL)","description":"Sustained power limit — ..."},
                     {"id":"pl2","label":"PL2 (sPPT)","description":"Short boost — ..."},
                     {"id":"pl3","label":"PL3 (fPPT)","description":"Fast boost — ..."}],
           "floor_curve":[{"temp":30,"pwm":127},{"temp":40,"pwm":127}, "..."],
           "stock_profile_ppt":{"balanced":{"pl1_spl":52,"pl2_sppt":71,"fppt":70}, "...":{}}},
  "profiles":{"names":["quiet","balanced","performance"],"default":"balanced",
              "entries":[{"name":"quiet","label":"Quiet"}, "..."]},
  "lighting":{"zones":["keyboard","lightbar"],"labels":["Keyboard","Lightbar"],
              "modes":[{"name":"static","label":"Static","color":true},
                       {"name":"breathe","label":"Breathe","color":true,"color2":true,"speed":true}, "..."],
              "speeds":["slow","normal","fast"],"brightness_max":3,
              "brightness_names":["off","low","medium","high"]},
  "toggles":[{"id":"boot_sound","label":"POST boot sound","kind":"bool","source":"core"},
             {"id":"panel_overdrive","label":"Panel overdrive","kind":"bool","source":"core",
              "description":"Faster pixel response; may cause ghosting ..."}],
  "cpu":{"boost":true},
  "undervolt":{"min":-40,"max":0,"label":"CPU Curve Optimizer","short_label":"CO",
               "description":"All-core Curve Optimizer offset: ..."},
  "battery":{"charge_limit":true,"charge_limit_min":40,"charge_limit_max":100,"health":true},
  "telemetry":{"power_draw":"rapl","gpu":"amdgpu","cpu_stats":"procfs","npu":"amdxdna","net":"procfs",
               "history_seconds":3600,"clock_max_mhz":5187,"temp_limit_c":100},
  "buttons":true,"button_label":"Armoury Crate button",
  "hardware_buttons":[{"id":"armoury-crate","label":"Armoury Crate button","default":true}]}}
```

`cpu.boost` says the machine has a global CPU boost switch, so the
`cpuboost`/`cpuboost-get` commands work; the section is absent otherwise.
`rumble` (`{"min":0,"max":5}` on the OneXPlayer X2 Mini Pro) says the built-in
controller has a settable vibration strength on that scale, where `min` is the
weakest (0: off); the `rumble`/`rumble-get` commands work. The range is the
kernel's, and the section is absent while the controller is not present. Each
toggle's `source` is `"core"` for one a compiled-in driver provides, or
`"plugin:<id>"`, and is always present; `description` is prose to show beside
the switch, absent when the device data has none.

`name` is the machine's display name, for titles and bug reports (absent when
the device data gives none: show `model`).

`power.limits` lists the limits a user may set, in the order to show them,
each with its `id` (`pl1`, `pl2` or `pl3` — the `tdp` command's fields), a
`label` and a one-line `description`. It is the kernel's answer where the
interface reports which limits exist, filtered by the device data: a device
that lets you set only the sustained limit lists `pl1` alone, and a client
offers one control rather than three. The ranges stay where they are
(`tdp_min`..`tdp_max_forced` for `pl1`, `pl2`, `pl3`). Absent from a daemon older
than the field: offer all three.

`undervolt`'s `label`, `short_label`, `unit` and `description` name the offset
on this device — "CPU Curve Optimizer" / "CO" on AMD, unitless steps — so a
client need not know which vendor's mechanism it is driving. Any of them may be
absent; fall back to your own wording.

`lighting.brightness_names` names each brightness level from 0 to
`brightness_max`, lower case, when the protocol has names for them; absent
when the levels are only numbers. It always has `brightness_max`+1 entries.

`buttons` says the daemon watches a hardware button (and emits the button
events); `button_label` is what the device's default button is called, written
to sit mid-sentence, and is absent when the device names none.
`hardware_buttons` lists every button a user may choose to open voltaire with,
in the device's order, the default marked; `button-set` picks one. The list is
the device file's: voltaire never reads a keyboard to learn a key.

Capability discovery is **by absence**: a section that is missing means the
device does not have that capability — never an error — and a client hides the
corresponding controls. The document is built from device data, plus the power
limit ranges the kernel reports. Fetch it at startup **and again whenever you
reconnect** — a restarted daemon may report different limits (an upgrade, or a
kernel interface that became writable) — and take the power ranges from
`get-state`'s `tdp_limits` on each refresh, since those are live; see below.
Bound every control from the document, never from numbers of your own: every
value in it varies by device.

Bounds worth knowing: `power.tdp_min`..`tdp_max_forced` is the range the
sustained limit (PL1) accepts, and `power.pl2`/`pl3` the burst limits' ranges
(absent means PL1's). Where the kernel reports its own bounds the daemon serves
those rather than the device data's — on the GZ302EA through asus-armoury, named
in `power.interface` — so they can differ from one kernel to the next, and on a
device whose firmware keeps separate AC and battery tables, between power
sources (the GZ302EA's agree). A `tdp` request below a burst limit's minimum is
raised to it, and one above its maximum refused. While the embedded controller
is not answering after a resume, the daemon serves the last ranges it read
rather than reading the kernel, since on asus-armoury each read is a live ACPI
call. `fans.points` is how many points a curve holds — the kernel's count
where the daemon can read it — and `fans.pwm_max` the speed ceiling; size a
curve editor from both, since a curve of any other length is refused.
`fans.labels` names each fan in the order `get-state`'s `rpm` lists them, so
its length is the number of fans (absent from an older daemon: label by
position). The `telemetry` sources name what the daemon reads, and a source is
listed only where its hardware is present — a declared GPU on a machine with no
amdgpu card, or an NPU on one without amdxdna, is left out rather than framed
as a chart that never fills. `telemetry.clock_max_mhz` (the highest clock any
charted clock reaches), `telemetry.temp_limit_c` (the firmware's throttle
point, the ACPI passive trip) and `fans.rpm_max` are chart hints: where to put
an axis's top, never a clamp, since a reading past one is real. Each is absent
when the machine cannot say. `fans.temp_min`/`temp_max` are the curve editor's
display axis, not validation limits; `power.floor_curve` is the fan floor
enforced while the sustained TDP exceeds `tdp_max_safe` (draw it under the
user's curve); `toggles[].values` lists the legal values where the firmware
reports them (absent means 0 and 1); `toggles[].id` is the wire identifier the `feature` commands
below take.

`profiles.names` is the firmware profiles the device offers, least power first
— the kernel's list, filtered by the device data — and the only list to build
firmware-profile controls from. `profiles.default` is where `tdp-reset` and
`tuning-reset` land (its stock limits are within the safe sustained maximum),
and `profiles.entries` gives each a display label. A daemon older than those
two fields omits them. The names a *custom* profile may never take are wider
than `profiles.names`: every kernel profile name is reserved on every device.

`profiles.pin` names the one firmware profile the daemon holds on this device;
absent on most. The OneXPlayer X2 Mini Pro pins `balanced`, because its
`low-power` caps power below anything the TDP control sets. While pinned,
`profiles.names` lists the pin alone, and the daemon puts it back (and logs it)
when another program selects a different one. Offer no firmware-profile
choice there: the pin means "no saved profile", and voltaire's own GUI labels
it **No profile** beside the saved custom profiles. The pin holds only while
the device's TDP control exists. When that control is unavailable (see
`unavailable` below) the pin is absent, and every firmware profile is offered
as the only power control left.

`power.slider` says a `tdp` set with no `profile` field is a **device
setting**, not a profile edit: one remembered value, applied, re-applied at
startup, on resume and after a charger edge, and capped on battery by
`get-state`'s `tdp_limits`. With a custom profile running it leaves that
profile, unchanged. Loading a profile recalls the TDP it saved, and changing
a profile's TDP takes an explicit `profile` field. Absent on devices where a
bare `tdp` set edits the active profile (creating `custom` from a firmware
profile), which is everywhere else.

`quickbar` is the drawer's default contents on this device, as control IDs in
order; a user's own choice wins. Absent means the client's built-in default.

`unavailable` lists capabilities the device has that this machine cannot
offer, each with a `capability` (the section that is therefore absent:
`"power"`) and a `reason` saying what is missing and how to get it. The
OneXPlayer X2 Mini Pro's TDP control needs the out-of-tree `ryzen_smu` module.
Without it the `power` section is absent as usual, and this list says why, so
show the reason where the control would be. The daemon assembles the device
at startup, so after the module is installed it needs a restart (or a
reboot). Absent when nothing is missing, and from an older daemon.

`telemetry.fans` names a read-only fan-speed source (an hwmon device name) on
a device whose fans voltaire does not control: `get-state`'s `rpm` and the
history's `rpm` carry its readings, and there is no `fans` section (no curve).

`fans.presets` lists the device's named starting-point curves, in the order to
offer them. There is **no preset command** — applying one is an ordinary
`fancurve` set with the preset's points, so every rule that governs a
hand-drawn curve governs a preset too, and a client that offers presets needs
no new protocol. The list is absent on a device that declares none, in which
case show no preset control rather than an empty one. `name` is the identifier
(matched case-insensitively by the CLI), `label` is what to display, and
`description` is optional prose: what a curve does to *this* machine is device
knowledge a client cannot derive. Note there is deliberately no preset for
firmware auto — that is a fan *mode*, not a curve, and `fancurve-reset` is how
you reach it.

Each toggle also carries `label` and an optional `description` — prose to show
beside the control, including any consequence worth warning about ("may cause
ghosting"). The description is served rather than left to the client because it
is device knowledge: whether panel overdrive ghosts is a fact about that panel,
and a client rendering rows generically has no way to derive it. Pair each entry
with its current value from `get-state`'s `features` map and a full toggle row
needs nothing else.

`battery` and `telemetry` are sections rather than plain `true` because their
contents are independently absent. `battery.charge_limit` says the
`batterylimit` commands work, and `battery.charge_limit_min`/`charge_limit_max`
are the percentages they accept — device data, because the kernel publishes no
range for the threshold (a daemon older than the fields omits them; assume
40–100 there). `battery.health` says `get-state` reports
`battery_health`, the pack's full-charge capacity as a percentage of its design
capacity (it is not clamped to 100 — a freshly calibrated pack reads slightly
above design). `telemetry.history_seconds` is the largest history window worth
requesting, and `telemetry.power_draw` names the package-power source
(`"rapl"`; `"pm-table"` is reserved for a planned SMU power-table reader) —
**absent when the device reads none**, in which case
package power is always zero and the graph should be hidden rather than drawn
flat. The Z13 declares `"rapl"`. Its `energy_uj` counter is root-only under the
Platypus side-channel mitigation, so `voltaire setup` grants the group **read**
access to it (the one read-only grant voltaire installs — the same directory
holds the CPU's power caps). Until setup has run, the device still declares the
source but no sample carries a reading, and the graph is hidden.

`telemetry-history` returns what the daemon sampled at 1 Hz, oldest first:

```json
{"ok":true,"history":[{"at":1786000000,"temp_c":44,"rpm":[2100,2050],
                       "package_power_w":18.4,"battery_level_pct":81,"cpu_util_pct":7, "...":0},
                      {"at":1786000001,"temp_c":45,"rpm":[2150,2100], "...":0}, "..."]}
```

Each sample carries whichever of these the device measures:

| Field | Meaning |
|---|---|
| `at` | Unix seconds |
| `temp_c`, `gpu_temp_c` | CPU (Tctl) and GPU edge temperature, °C |
| `rpm` | every fan, in driver order |
| `package_power_w`, `gpu_power_w`, `npu_power_w` | power draw, watts |
| `battery_power_w`, `battery_level_pct` | battery flow (positive discharging) and charge |
| `cpu_util_pct`, `gpu_util_pct`, `npu_util_pct` | utilisation, percent |
| `cpu_clock_mhz`, `gpu_clock_mhz`, `mem_clock_mhz`, `npu_clock_mhz` | clocks, MHz |
| `mem_used_mb`, `mem_total_mb`, `vram_used_mb`, `vram_total_mb` | system memory and VRAM carveout |
| `net_rx_mbps`, `net_tx_mbps` | throughput over physical interfaces, MB/s |

Presence follows what a zero would mean. Utilisation, battery, GPU/NPU power
and network rates are sent whenever they were read, **zero included** — an idle
NPU or a resting pack genuinely reads 0 — so their absence means "not
measured". Temperatures, clocks and the remaining fields are omitted when zero,
since a zero there is never a reading.

Omit `seconds` for the whole retained window (`telemetry.history_seconds`).
**Plot against `at` — Unix seconds — never against the array index.** Samples
are not evenly spaced: the sampler stands down while the machine is suspending
and skips a failed read rather than recording a zero, so gaps are real and
meaningful. The history lives only in the daemon's memory, so a daemon restart
starts it over; an empty `history` is a daemon that has been up for less than a
sample interval, not an error.

`power.stock_profile_ppt` gives each firmware profile's PPT defaults, which is
how a client tells "the firmware's numbers" from "numbers the user chose" —
`tdp-get` and get-state's `tdp` on a stock profile report exactly that
profile's row (keyed on `platform_profile`, so a switch made by Fn+F5 shows the
profile it switched to), never the kernel's readback, which on asus-armoury is
a per-power-source cache of the last value written. Without this table they
are indistinguishable from a custom setting. Compare on
`pl1_spl`/`pl2_sppt`/`fppt`; the daemon also writes APU and Platform sPPT,
mirrored from PL2, and they are not part of the comparison.

## Lighting

| Command | Request | Response |
|---|---|---|
| Apply an effect | `{"cmd":"apply","mode":"cycle","color":"FF0000","color2":"000000","speed":"normal","brightness":3,"device":"lightbar"}` | `ok` |
| Turn off | `{"cmd":"off","device":""}` | `ok` |
| Brightness only | `{"cmd":"brightness","brightness":2,"device":""}` | `ok` |

`device` accepts a zone name from `device-get`'s `lighting.zones`
(`"keyboard"`, `"lightbar"` on the Z13), a `/dev/hidrawN` path, or `""` for all
zones. `mode` and `speed` must be ones `lighting.modes` and `lighting.speeds`
list, and `brightness` runs from 0 to `lighting.brightness_max` (3 on the Z13).
Each mode in `lighting.modes` says which inputs it uses — `color`, `color2`,
`speed` — so a client shows only those controls.

## System settings

| Command | Request | Response |
|---|---|---|
| Set profile | `{"cmd":"profile","set":"performance"}` | `ok` |
| Get profile | `{"cmd":"profile-get"}` | `ok`, `value` |
| Set battery limit | `{"cmd":"batterylimit","set":"80"}` | `ok` |
| Get battery limit | `{"cmd":"batterylimit-get"}` | `ok`, `value` |
| Charge to full once / cancel | `{"cmd":"batterylimit-full-once","set":"on"}` (`"off"` cancels) | `ok` |
| Set boot sound | `{"cmd":"bootsound","set":"1"}` | `ok` |
| Get boot sound | `{"cmd":"bootsound-get"}` | `ok`, `value` |
| Choose the button that opens voltaire | `{"cmd":"button-set","set":"armoury-crate"}` | `ok` |
| Set CPU boost | `{"cmd":"cpuboost","set":"0"}` | `ok` |
| Get CPU boost | `{"cmd":"cpuboost-get"}` | `ok`, `value` (`0`/`1`) |
| Set controller vibration | `{"cmd":"rumble","set":"2"}` | `ok` |
| Get controller vibration | `{"cmd":"rumble-get"}` | `ok`, `value` (the strength) |
| Set panel overdrive | `{"cmd":"paneloverdrive","set":"1"}` | `ok` |
| Get panel overdrive | `{"cmd":"paneloverdrive-get"}` | `ok`, `value` |
| Set firmware toggle | `{"cmd":"feature","id":"boot_sound","set":"1"}` | `ok` |
| Get firmware toggle | `{"cmd":"feature-get","id":"boot_sound"}` | `ok`, `value` |

`batterylimit-full-once` lifts the charge limit to the device's maximum until
the battery is full, then puts the saved limit back; as in G-Helper, unplugging
part-way does not end it. `get-state`'s `charge_once` is true while it runs,
and `battery_limit` stays the limit it returns to. Setting a limit with
`batterylimit` cancels it. It is refused when no limit is set (the battery
already charges to full), when the battery is already full, and in Gaming Mode
where Steam owns the limit.

`profile` accepts one of the device's firmware profiles (`device-get`'s
`profiles.names`; `quiet`, `balanced` and `performance` on the Z13) or the name
of a custom profile (including `custom`). `profile-get` and `get-state`'s
`profile` carry the device's names too: a secondary kernel handler's spelling
(`low-power` for `quiet`) is mapped back, and a read the device cannot name — the
kernel's `custom`, meaning its handlers disagree — is reported as an error
rather than passed through.

`feature`/`feature-get` are the generic form of the firmware-toggle commands:
`id` is any toggle the `device-get` document lists, so a client driven by that
document needs no per-toggle code. `bootsound` and `paneloverdrive` remain as
the named equivalents for the Z13's two toggles. `"bool"` toggles take 0 or 1;
an unknown `id` is an error naming it.

A daemon older than these commands answers `{"ok":false,"error":"unknown
command: feature"}`. Treat that as "this daemon cannot help", not as a failed
write, and go straight to sysfs — firmware toggles are BIOS settings the
daemon neither persists nor broadcasts, so a direct write is equivalent. This
is the normal state of things between a package upgrade and the daemon
restart. Go clients get the distinction from `errors.Is(err,
api.ErrUnknownCommand)`, which preserves the daemon's wording; other clients
should match the `unknown command` prefix. Probe for the error — never gate on
a version number.

::: warning Unknown profile names are now rejected
Earlier daemons forwarded any string to `platform_profile`. A name that is
neither one of this device's firmware profiles nor a saved custom profile is
now an error, so a typo cannot reach the fan-curve reset on its way to failing.
Send the names `device-get` lists: `low-power` works on a device that offers it
and is refused on one that does not.
:::

## Custom profiles

| Command | Request | Response |
|---|---|---|
| Create profile | `{"cmd":"profile-create","set":"gaming"}` | `ok` |
| Copy active profile | `{"cmd":"profile-save","set":"gaming"}` | `ok` |
| Delete profile | `{"cmd":"profile-delete","set":"gaming"}` | `ok` |
| List profiles | `{"cmd":"profile-list"}` | `ok`, `value` (JSON) |

`profile-list` returns a JSON array of `{name, fan_curve, tdp, undervolt,
active}`. Names are lowercase, 1–32 characters of `a-z`, `0-9`, `-`, `_`, and
may not be a firmware profile name or `custom`; `profile-create` and
`profile-save` validate the name as sent rather than case-folding it.

`profile-delete` refuses to remove the active profile or one referenced by
`autoswitch`.

Selecting a custom profile puts hardware into the state that profile
describes, which means a subsystem it leaves unset is **cleared**, not left
alone: no fan curve releases the fans to firmware auto, no undervolt resets
the Curve Optimizer, and no TDP hands the power limits back to the firmware
profile underneath. Without that, switching between two custom profiles would
leave the previous one's limits and offset in force, and selecting A then B
then A would not give the same machine as selecting A.

## AC/battery autoswitch

| Command | Request | Response |
|---|---|---|
| Configure | `{"cmd":"autoswitch","enabled":true,"ac":"balanced","battery":"gaming"}` | `ok` |
| Get | `{"cmd":"autoswitch-get"}` | `ok`, `value` (JSON: `enabled`, `ac`, `battery`, `on_ac`, `source_known`) |

An empty `ac` or `battery` leaves the profile alone on that source. Both
targets are validated against the firmware profiles and the saved custom
profiles.

## Fans, TDP, and undervolt

| Command | Request | Response |
|---|---|---|
| Set fan curve | `{"cmd":"fancurve","set":"48:2,55:20,..."}` | `ok` |
| Get fan curve | `{"cmd":"fancurve-get"}` | `ok`, `value` (JSON) |
| Reset fan curve | `{"cmd":"fancurve-reset"}` | `ok` |
| Set TDP | `{"cmd":"tdp","set":"60","pl1":"55","pl2":"65","pl3":"70","force":true}` | `ok` |
| Get TDP | `{"cmd":"tdp-get"}` | `ok`, `value` (JSON) |
| Reset TDP | `{"cmd":"tdp-reset"}` | `ok` |
| Reset all tuning | `{"cmd":"tuning-reset"}` | `ok` |
| Set undervolt | `{"cmd":"undervolt","set":"-20"}` | `ok` |
| Get undervolt | `{"cmd":"undervolt-get"}` | `ok`, `value` (JSON: `cpu_co`, `active`, `profile`) |
| Reset undervolt | `{"cmd":"undervolt-reset"}` | `ok` |

The `pl1`/`pl2`/`pl3` fields are optional overrides; `set` alone applies one
value to all three. `force` is required for a sustained limit (PL1) above the
device's `power.tdp_max_safe`.

On a device with `power.slider` (the OneXPlayer X2 Mini Pro), `undervolt` and
`undervolt-reset` with no `profile` field set the **undervolt setting**
(`device_undervolt`), the counterpart of the TDP slider's value: it is applied
and remembered, `"set":"0"` clears it, and made while a custom profile runs it
leaves that profile for the firmware profile, as a bare `tdp` set does. The
setting is in force on the firmware profile and under any custom profile that
carries no offset of its own; a profile with its own offset wins while it runs.
`undervolt-get` reports the setting. `tdp-reset` leaves it in force and
`tuning-reset` clears it.

On a device with `power.slider`, a `tdp` set with no `profile` is the device's
TDP setting rather than an edit to the active profile, and with a custom
profile running it switches back to the pinned firmware profile, leaving the
custom profile's saved TDP as it was. `tdp-reset` clears the setting and
writes the firmware's own preset for the current power source. See
`power.slider` above.

All six commands accept an optional `profile` field naming the custom profile
to edit. Absent or empty means the active profile — which is what every client
written before this field existed sends, so their behaviour is unchanged.
Naming a profile that is *not* active stores the setting and writes nothing to
hardware; the fan-curve floor is then checked against that profile's own TDP
rather than against the running machine, so a profile can never be stored in a
state that would be unsafe when activated.

::: danger The `profile` field is silently ignored by older daemons
It is an additive field, so a daemon from before custom profiles unmarshals
the request, drops the field, **applies the setting to the running machine**,
and answers `ok`. A client that offers profile targeting must probe first —
`profile-list` answers `unknown command` on an older daemon, which is exactly
what `voltaire` itself does before sending any `--profile` edit.
:::

::: warning Fan commands are restricted above the safe sustained TDP
While PL1 is above `power.tdp_max_safe` (75 W on the GZ302EA), the fans are
held to the device's `power.floor_curve`, measured at each point's own
temperature. `fancurve` is rejected if any point falls below that floor, and
`fancurve-reset` is rejected outright — firmware auto mode has no minimum.
`tdp-reset` is the way out: it lowers the limit before releasing the fans.

`tdp` applies the same rule in the other direction. Raising PL1 above the safe
maximum writes the fan curve **first**, and if that write fails the power limit is not
applied at all. The floor is a **per-point minimum against the built-in
high-TDP curve**: each point is raised to that curve's value where it falls
below it, and left exactly as stored where it does not, so a client sending a
curve above it gets that curve rather than a substitute. Note this is the
whole curve and not just its bottom (127 PWM on the GZ302EA) — a curve flat at
the bottom's value is still raised at higher temperatures. The built-in curve is written whole only when
the profile has no curve of its own. The same holds for the daemon's own
restore paths — startup, resume, and selecting a custom profile.
:::

## State and events

| Command | Request | Response |
|---|---|---|
| Full state | `{"cmd":"get-state"}` | `ok`, `state` |
| Subscribe | `{"cmd":"subscribe","events":["gui-toggle","gui-open-full","power-source","state-changed"]}` | `ok`, then streamed events |
| Take the controller | `{"cmd":"gamepad-intercept"}` | `ok` (`value`: controllers taken), then streamed `gamepad` events; see [Controller intercept](#controller-intercept) |

`get-state` merges persisted state with live sysfs reads — see
[State fields](#state-fields).

Each streamed event is a full response object with an `event` field:

```json
{"ok":true,"event":"gui-toggle"}
```

**Events**

| Event | Emitted when |
|---|---|
| `gui-toggle` | the button that opens voltaire is pressed (the Armoury Crate button on the Z13 by default; get-state's `drawer_button` says which). Presses of the device's other buttons emit nothing |
| `gui-open-full` | the button is pressed twice in quick succession — sent **in addition to** the second `gui-toggle` |
| `power-source` | the machine moves between mains and battery power |
| `state-changed` | the active profile, its settings, the saved profiles, the autoswitch configuration, or a firmware toggle change |

`gui-open-full` never replaces `gui-toggle`: every press toggles immediately, so
the first one opens your window with no added latency, and a client that wants
the two-press gesture subscribes to both and closes whatever the toggle opened
when the escalation arrives. Waiting to find out whether a second press was
coming would put the whole detection window onto every single press.

The window is 50–400 ms between presses. The lower bound is not padding: some
firmware reports one press twice in the same instant, and without a floor every
press would look like a double. Three presses in a row escalate once, not twice.

`power-source` fires on the transition itself, whether or not autoswitch is
configured, so a client can drive a plug/battery indicator from it alone.

`state-changed` fires whatever the cause — this client, another client, the
CLI, autoswitch, or a resume. A client displaying profile, TDP, fan curve,
undervolt or firmware-toggle values should re-read them with `get-state` when
it arrives. Lighting is deliberately excluded: a brightness slider drag would
emit a burst of events describing values the client just set itself.

All three toggle write paths emit it — `feature`, `bootsound` and
`paneloverdrive`. Only the last one used to, which nothing could observe until
a client rendered toggle rows from `features`: the same switch then updated
live or did not, depending on which command had written it.

Events carry **no payload**. The name says what happened and `get-state`
answers with current truth — a payload would describe the moment the event was
queued, which can already be stale by the time the client handles it.

The `events` list is honoured: a client that subscribes to `gui-toggle` alone
is never woken by the other two. Subscribing with an empty list receives
everything.

::: warning Switch on the event name
While `gui-toggle` was the only event, `for range ch { toggle() }` was a
reasonable client loop. It is not any more — it would toggle the window on
every power-source change. Dispatch on the name.
:::

Discriminate on the presence of `event` — a command reply never carries it.
(Events emitted by v1.2.0 and earlier carried `"ok":false`; clients that gate
on `ok` dropped them.)

::: info Timeouts
The daemon closes a connection that does not send its request line within 30
seconds, so a client cannot pin a goroutine by connecting and going silent.
This deadline is cleared once a `subscribe` is acknowledged, since
subscriptions are idle by design between button presses. In the other
direction, `api` clients bound a whole command exchange at 10 seconds so a
wedged daemon cannot hang the caller indefinitely.
:::

## Controller intercept

`gamepad-intercept` takes the machine's controller from games for as long as
the connection stays open, and streams its navigation. It is what voltaire-gui
uses while the drawer or window is up on a machine where
[InputPlumber](https://github.com/ShadowBlip/InputPlumber) manages the
controller: InputPlumber hides the real one and presents a virtual controller
to games, which on the ROG Xbox Ally X is an emulated Steam Deck controller
with no evdev node, so there is nothing an overlay can read or grab itself.

The daemon switches each InputPlumber composite device that carries a gamepad
to intercept mode. InputPlumber then stops feeding the virtual controller
(games and Steam receive an idle controller) and sends navigation over DBus,
which the daemon forwards:

```json
{"ok":true,"event":"gamepad","gamepad":{"action":"accept","pressed":true}}
```

`action` is `up`, `down`, `left`, `right`, `accept`, `back`, `l1` or `r1`, and
`pressed` is false for the release; the left stick arrives as the four
directions. Nothing else the controller sends is forwarded. Buttons InputPlumber
maps to voltaire (the button that opens it) keep working throughout.

The controller goes back when the **last** client closes its connection,
whether it released it or crashed, and when the daemon stops; each device
returns to the mode it had, unless something else has changed it since. A
daemon killed outright leaves `$XDG_RUNTIME_DIR/voltaire/intercept.json`, and
the next one restores from it at startup.

The reply is `ok: false` when InputPlumber is not running, manages no
controller, or refuses: its polkit rule lets members of `wheel` and
`inputplumber` change the mode and asks anyone else for an administrator
password. voltaire never adds anyone to `inputplumber`, which also allows
injecting keystrokes. A client should then fall back to reading the controller
itself, as voltaire-gui does everywhere InputPlumber is absent.

## Minimal client

```python
import asyncio, json, os

async def send(req):
    r, w = await asyncio.open_unix_connection(
        f"/run/user/{os.getuid()}/voltaire/voltaire.sock")
    w.write((json.dumps(req) + "\n").encode())
    await w.drain()
    resp = json.loads(await r.readline())
    w.close()
    return resp

asyncio.run(send({"cmd": "profile", "set": "quiet"}))
```

Go callers should use the [`api` module](/dev/go-api) rather than
speaking the protocol directly.


## State fields

`get-state` returns two kinds of field: the daemon's persisted state, and live
readings added on each request.

### Persisted fields

These are what the daemon saves in its
[state file](/reference/daemon#state-file), written atomically after every
successful command:

- `lighting` — mode, color, color2, speed, brightness, enabled flag
- `devices` — per-device overrides (keyboard/lightbar can have independent state).
  Turning one zone off keeps its effect with `enabled` false, so
  `voltaire brightness <level> --device <zone>` brings back that zone's own
  effect. (Daemons before 2.0 cleared the rest of the entry on a per-zone off.)
- `profile` — the active profile: a firmware profile, or a custom profile name
- `battery_limit` — last-set charge limit
- `charge_once` — a one-time charge to full is in progress (`battery_limit` is
  the limit it returns to)
- `custom_profiles` — saved custom profiles keyed by name, each holding its own
  `fan_curve`, `tdp`, and `undervolt`. This is the source of truth for custom
  settings.
- `autoswitch` — `enabled`, and the `ac` and `battery` profile targets
- `cpu_boost` — present only once boost has been switched off or on through
  voltaire; a stored `false` is replayed at every daemon start, because cpufreq
  comes up boosting on every boot
- `rumble` — the controller's vibration strength as last set through voltaire;
  replayed at startup and after a suspend. Absent until first set
- `drawer_button` — the user's choice of button (`button-set`): a button id or
  `none`; absent means the device's default
- `device_tdp` — on a device with `power.slider`, the TDP setting (one value
  for every limit); absent until a TDP has first been set, so the firmware's
  own limits stand untouched until then. Cleared by `tdp-reset`
- `device_undervolt` — on a device with `power.slider` and an undervolt
  control, the undervolt setting (`cpu_co`, and `active` when it is in hardware
  now); absent until one is set, so nothing is written to the CPU until then.
  Re-applied at startup and after a suspend. Cleared by `undervolt-reset`,
  `"set":"0"` and `tuning-reset`
- `fan_curve`, `tdp`, `undervolt` — a **projection**, written for the benefit
  of clients and daemons from before named profiles existed. They carry the
  active custom profile's settings, or `custom`'s when a firmware profile is
  active — the same values selecting `custom` would recall, which is what
  keeps a "saved undervolt, not active" display working and what stops a
  downgrade taken while on `balanced` from writing those settings away. They
  are output only; `custom_profiles` is what is read back.

A state file written before named profiles existed has no `custom_profiles`,
so its top-level `fan_curve`/`tdp`/`undervolt` are migrated into a profile
called `custom` on first load — the same settings, now addressable by name.
Nothing needs to be done by hand.

On startup the daemon reads the state file, resolves what the current power source
calls for if autoswitch is configured, and restores all saved settings before
accepting any connections. If the active profile is a custom one, its fan
curve, TDP, and undervolt are re-applied to the hardware. If it is a firmware
profile, the limits are handed back to the firmware: that profile's stock values
are written, then the fans are released to firmware auto, which makes the
firmware re-apply the profile's own limits. (The release is skipped when a fan
curve set by another tool is in force.)

### Live fields

On `get-state` requests the daemon also populates, from live sysfs reads:

| Field | Meaning |
|---|---|
| `temperature` | APU die temperature in °C |
| `rpm` | every fan the device reports, in driver order |
| `fan_rpm` | `rpm[0]`, kept for clients written before multi-fan support |
| `package_power_w` | current CPU package draw in watts; absent when unavailable |
| `battery_power_w` | battery flow in watts, **positive discharging**, negative charging |
| `features` | every firmware toggle's current value, keyed by id; a toggle that could not be read is **absent**, not zero |
| `cpu_boost` | whether CPU boost is on, read from the kernel; **absent** when unreadable or on a machine without the switch |
| `rumble` | the controller's vibration strength, read from the driver; **absent** when unreadable or on a machine without one |
| `drawer_button` | the id of the button that opens voltaire, resolved (the user's choice, else the device default), or `none`; **absent** on a device with no buttons |
| `telemetry` | the newest sample, in the same shape as a `telemetry-history` entry: the live edge of the series. Absent when the sampler has nothing recent |
| `battery_level` | the pack's current charge, percent |
| `battery_state` | `charging`, `discharging`, `full`, `not-charging` |
| `on_ac` | whether the charger is plugged in |
| `source_known` | whether `on_ac` reflects a real reading |
| `undervolt_available` | whether the `ryzen_smu` kernel module is present — and, once an undervolt has been written, whether that write worked (the check is itself a Curve Optimizer write, so it is not made just to answer this) |
| `pending_reboot` | whether a changed firmware setting needs a restart; **absent** when the device cannot say |
| `charger` | which power input is supplying the machine: `adapter`, `usb-c`, or `none`; **absent** when the device cannot say |
| `tdp_limits` | the power limits the kernel accepts: `backend` (`asus-armoury` or `asus-nb-wmi`), `pl1`/`pl2`/`pl3` as `{min,max}` watts, and `safe_max` (the highest PL1 without `force`). Introduced for z13ctl 1.4 clients, and the live copy of the [`device-get` power section](#device-capabilities)'s ranges: overlay it on each refresh to follow a change of power source or interface. **Absent** while the embedded controller is not answering, and when no power-limit interface is known — keep the last ranges then |
| `battery_health` | full-charge capacity as a percentage of design capacity |
| `battery_energy_wh` | the pack's remaining energy in watt-hours |
| `battery_energy_full_wh` | the pack's full-charge energy in watt-hours |

These are not persisted — they are real-time sensor values, the live edge of the
same series `telemetry-history` returns.

Four of them need care:

- **`source_known` false means *unknown*, not battery.** On a machine with no
  mains supply to read (a VM, a desktop) `on_ac` is false with `source_known`
  false. Claim nothing in that case.
- **`rpm` and `fan_rpm` are the same reading, not two.** Prefer `rpm`: both of
  the Z13's fans cool the same die and routinely differ by several hundred RPM,
  so quoting `fan_rpm` alone describes neither.
- **`package_power_w` is absent rather than stale.** On hardware that publishes a
  cumulative energy counter instead of instantaneous power, this is the daemon's
  1 Hz sampler's most recent figure — deriving power needs two readings and the
  interval between them, so a single request cannot compute one. A figure the
  sampler has not refreshed within a few seconds (it stands down across a
  suspend) is omitted, because a value labelled "now" describing some other
  moment is worse than no value.
- **`battery_power_w`'s zero is a reading**, which is why it is the one field
  here that is null rather than omitted when absent: a full pack on mains
  genuinely moves no energy, so zero and "this machine has no battery" have to
  stay distinguishable.
- **Read `battery_power_w` together with `battery_state`, never alone.** The
  commonest reading on a machine with a charge limit is the confusing one: a
  pack resting *above* its threshold on mains reports exactly `0`, correctly,
  because nothing is moving in either direction. Shown as a bare zero that is
  indistinguishable from a broken sensor. `battery_state` is `not-charging`
  there — deliberately distinct from `full`, since the pack is not full, it is
  being held back — and with `battery_level` (the reading) beside `battery_limit`
  (the setting) a client can say "81%, holding at your 75% limit".
- **`features` is how you render toggle rows generically.** It carries every id
  `device-get`'s `toggles[]` declares, so a client pairs each `ToggleInfo`
  (`label`, `description`, `kind`) with its current value from one `get-state`
  rather than a `feature-get` per toggle per refresh. `boot_sound` and
  `panel_overdrive` remain as top-level fields forever — they are the fixed
  vocabulary older clients read — and hold the same values. **A toggle that
  could not be read is absent from the map**, not zero: zero means "off", and a
  switch has to be able to show that it does not know.
- **`battery_level` and `battery_limit` are different things.** The first is the
  charge now; the second is the end threshold voltaire writes. They are
  routinely different numbers, and the level sitting above the limit is the
  case above.
- **The energy pair turns the flow into a time estimate.** While discharging,
  `battery_energy_wh / |battery_power_w|` is hours to empty; while charging,
  the gap between `battery_energy_wh` and the charge target over the rate is
  hours to full — where the target is `battery_energy_full_wh` scaled by
  `battery_limit` when a limit is set, since the pack genuinely stops there.
  The pair is watt-hours on every machine (the daemon converts charge-reporting
  packs through their voltage), and both are zero when the pack reports neither
  energy form. Suppress the estimate at very low rates — a resting pack wobbles
  by tenths of a watt, and dividing by that claims precision the reading does
  not have.
- `battery_health` is zero when the device does not report it — `device-get`'s
  `battery.health` says which in advance. It is not clamped to 100.
