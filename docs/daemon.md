# Daemon

The z13ctl daemon is a long-running background process that provides three things
ordinary one-shot CLI invocations cannot:

- **State persistence** — saves your last-applied lighting, profile, battery,
  fan curve, TDP, and undervolt settings to `~/.local/state/z13ctl/state.json`
  and restores them automatically at every boot.
- **Sleep/resume recovery** — watches for system resume events via D-Bus and
  reapplies lighting and volatile settings (fan curves, TDP, undervolt) that
  are lost during sleep.
- **Keyboard reattach recovery** — detects the detachable keyboard being removed
  and reattached, reopens the HID device, and re-applies the saved keyboard
  lighting (the firmware does not restore it on its own).
- **Custom fan curve reconciliation** — re-applies your custom fan curve (and the
  high-TDP fan floor) after the kernel driver drops it, which it does on every
  system power profile change.
- **HID device ownership** — holds the hidraw devices open continuously so that
  commands arrive instantly rather than waiting to reopen the device each time.
- **Armoury Crate button events** — captures `KEY_PROG3` (the dedicated Armoury
  Crate button) and broadcasts a `gui-toggle` event to any connected subscribers
  (see [API](api.md)).

All CLI commands (`apply`, `brightness`, `off`, `profile`, `batterylimit`,
`bootsound`, `paneloverdrive`, `fancurve`, `tdp`, `undervolt`, `status`) automatically route through the
daemon socket when it is running. If the daemon is not running they fall back to direct hardware or
sysfs access transparently — there is no user-visible difference other than
persistence.

---

## Systemd setup (recommended)

z13ctl ships two systemd user units that use socket activation:

- **`z13ctl.socket`** — systemd creates and manages the Unix socket. The daemon
  is started on first use and does not run if nothing has connected.
- **`z13ctl.service`** — `Type=notify`, `Restart=on-failure`. The daemon sends
  `sd_notify READY` when it is listening.

The units target `graphical-session.target`, so they work in both desktop
environments (KDE, GNOME) and Steam Gaming Mode (gamescope session).

Install and enable:

```sh
install -Dm644 contrib/systemd/user/z13ctl.socket \
    ~/.config/systemd/user/z13ctl.socket
install -Dm644 contrib/systemd/user/z13ctl.service \
    ~/.config/systemd/user/z13ctl.service
systemctl --user daemon-reload
systemctl --user enable --now z13ctl.socket z13ctl.service
```

Or, if you built from source:

```sh
make install-service
```

---

## Managing the service

```sh
# Check status
systemctl --user status z13ctl.socket
systemctl --user status z13ctl.service

# View live logs
journalctl --user -u z13ctl -f

# Restart the daemon (e.g., after a config change)
systemctl --user restart z13ctl.service
```

### Remove the user service

```sh
systemctl --user disable --now z13ctl.socket z13ctl.service
rm -f ~/.config/systemd/user/z13ctl.socket \
      ~/.config/systemd/user/z13ctl.service
systemctl --user daemon-reload
```

---

## Running without systemd

Start the daemon directly for testing or on systems without systemd:

```sh
z13ctl daemon
```

To disable the Armoury Crate button watcher — because another tool needs
exclusive access to the device, or because you would rather the keypress reach
only your desktop:

```sh
z13ctl --no-button daemon
```

The daemon listens on a Unix socket at:

```
$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock
```

(falls back to `/tmp/z13ctl/z13ctl.sock` if `XDG_RUNTIME_DIR` is not set).

---

## Socket protocol

The wire format is newline-delimited JSON: one request object per line, one
response object per line. Every response carries `ok` (bool) and, on failure,
`error` (string). Commands that return data use `value` (a string, JSON-encoded
where the payload is structured).

Connections are single-shot — the daemon replies once and closes — except
`subscribe`, which stays open and streams events.

### Lighting

| Command | Request | Response |
|---|---|---|
| Apply an effect | `{"cmd":"apply","mode":"cycle","color":"FF0000","color2":"000000","speed":"normal","brightness":3,"device":"lightbar"}` | `ok` |
| Turn off | `{"cmd":"off","device":""}` | `ok` |
| Brightness only | `{"cmd":"brightness","brightness":2,"device":""}` | `ok` |

`device` accepts `"keyboard"`, `"lightbar"`, a `/dev/hidrawN` path, or `""` for
all zones. `brightness` is 0–3.

### System settings

| Command | Request | Response |
|---|---|---|
| Set profile | `{"cmd":"profile","set":"performance"}` | `ok` |
| Get profile | `{"cmd":"profile-get"}` | `ok`, `value` |
| Set battery limit | `{"cmd":"batterylimit","set":"80"}` | `ok` |
| Get battery limit | `{"cmd":"batterylimit-get"}` | `ok`, `value` |
| Set boot sound | `{"cmd":"bootsound","set":"1"}` | `ok` |
| Get boot sound | `{"cmd":"bootsound-get"}` | `ok`, `value` |
| Set panel overdrive | `{"cmd":"paneloverdrive","set":"1"}` | `ok` |
| Get panel overdrive | `{"cmd":"paneloverdrive-get"}` | `ok`, `value` |

`profile` accepts `quiet`, `balanced`, `performance`, or the name of a custom
profile (including `custom`).

!!! warning "Unknown profile names are now rejected"
    Earlier daemons forwarded any string to `platform_profile`. A name that is
    neither a firmware profile nor a saved custom profile is now an error, so a
    typo cannot reach the fan-curve reset on its way to failing. A client that
    sent e.g. `low-power` will need updating.

### Custom profiles

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

Selecting a custom profile puts hardware into the state that profile describes,
which means a subsystem it leaves unset is **cleared**, not left alone: no fan
curve releases the fans to firmware auto, no undervolt resets the Curve
Optimizer, and no TDP hands the power limits back to the firmware profile
underneath. Without that, switching between two custom profiles would leave the
previous one's limits and offset in force, and selecting A then B then A would
not give the same machine as selecting A.

### AC/battery autoswitch

| Command | Request | Response |
|---|---|---|
| Configure | `{"cmd":"autoswitch","enabled":true,"ac":"balanced","battery":"gaming"}` | `ok` |
| Get | `{"cmd":"autoswitch-get"}` | `ok`, `value` (JSON: `enabled`, `ac`, `battery`, `on_ac`, `source_known`) |

An empty `ac` or `battery` leaves the profile alone on that source. Both targets
are validated against the firmware profiles and the saved custom profiles.

### Fans, TDP, and undervolt

| Command | Request | Response |
|---|---|---|
| Set fan curve | `{"cmd":"fancurve","set":"48:2,55:20,..."}` | `ok` |
| Get fan curve | `{"cmd":"fancurve-get"}` | `ok`, `value` (JSON) |
| Reset fan curve | `{"cmd":"fancurve-reset"}` | `ok` |
| Set TDP | `{"cmd":"tdp","set":"60","pl1":"55","pl2":"65","pl3":"70","force":true}` | `ok` |
| Get TDP | `{"cmd":"tdp-get"}` | `ok`, `value` (JSON) |
| Reset TDP | `{"cmd":"tdp-reset"}` | `ok` |
| Set undervolt | `{"cmd":"undervolt","set":"-20"}` | `ok` |
| Get undervolt | `{"cmd":"undervolt-get"}` | `ok`, `value` (JSON: `cpu_co`, `active`, `profile`) |
| Reset undervolt | `{"cmd":"undervolt-reset"}` | `ok` |

The `pl1`/`pl2`/`pl3` fields are optional overrides; `set` alone applies one
value to all three. `force` is required for a sustained limit (PL1) above 75 W.

All six commands accept an optional `profile` field naming the custom profile to
edit. Absent or empty means the active profile — which is what every client
written before this field existed sends, so their behaviour is unchanged. Naming
a profile that is *not* active stores the setting and writes nothing to
hardware; the fan-curve floor is then checked against that profile's own TDP
rather than against the running machine, so a profile can never be stored in a
state that would be unsafe when activated.

!!! danger "The `profile` field is silently ignored by older daemons"
    It is an additive field, so a daemon from before custom profiles unmarshals
    the request, drops the field, **applies the setting to the running machine**,
    and answers `ok`. A client that offers profile targeting must probe first —
    `profile-list` answers `unknown command` on an older daemon, which is exactly
    what `z13ctl` itself does before sending any `--profile` edit.

!!! warning "Fan commands are restricted above 75 W sustained TDP"
    While PL1 is above 75 W, both fans are held to a minimum of 127 PWM (50%).
    `fancurve` is rejected if any point falls below that floor, and
    `fancurve-reset` is rejected outright — firmware auto mode has no minimum.
    `tdp-reset` is the way out: it lowers the limit before releasing the fans.

    `tdp` applies the same rule in the other direction. Raising PL1 above 75 W
    writes the high-TDP curve **first**, and if that write fails the power limit
    is not applied at all. The same holds for the daemon's own restore paths —
    startup, resume, and selecting a custom profile.

### State and events

| Command | Request | Response |
|---|---|---|
| Full state | `{"cmd":"get-state"}` | `ok`, `state` |
| Subscribe | `{"cmd":"subscribe","events":["gui-toggle"]}` | `ok`, then streamed events |

`get-state` merges persisted state with live sysfs reads — see
[State file](#state-file).

Each streamed event is a full response object with an `event` field:

```json
{"ok":true,"event":"gui-toggle"}
```

Discriminate on the presence of `event` — a command reply never carries it.
(Events emitted by v1.2.0 and earlier carried `"ok":false`; clients that gate on
`ok` dropped them.)

!!! note "Timeouts"
    The daemon closes a connection that does not send its request line within
    30 seconds, so a client cannot pin a goroutine by connecting and going
    silent. This deadline is cleared once a `subscribe` is acknowledged, since
    subscriptions are idle by design between button presses. In the other
    direction, `api` clients bound a whole command exchange at 10 seconds so a
    wedged daemon cannot hang the caller indefinitely.

### Minimal client

```python
import asyncio, json, os

async def send(req):
    r, w = await asyncio.open_unix_connection(
        f"/run/user/{os.getuid()}/z13ctl/z13ctl.sock")
    w.write((json.dumps(req) + "\n").encode())
    await w.drain()
    resp = json.loads(await r.readline())
    w.close()
    return resp

asyncio.run(send({"cmd": "profile", "set": "quiet"}))
```

Go callers should use the [`api` module](api.md) rather than speaking the
protocol directly.

---

## State file

The daemon persists state to:

```
~/.local/state/z13ctl/state.json
```

The file is written atomically after every successful command. It stores:

- `lighting` — mode, color, color2, speed, brightness, enabled flag
- `devices` — per-device overrides (keyboard/lightbar can have independent state)
- `profile` — the active profile: a firmware profile, or a custom profile name
- `battery_limit` — last-set charge limit
- `custom_profiles` — saved custom profiles keyed by name, each holding its own
  `fan_curve`, `tdp`, and `undervolt`. This is the source of truth for custom
  settings.
- `autoswitch` — `enabled`, and the `ac` and `battery` profile targets
- `fan_curve`, `tdp`, `undervolt` — a **projection**, written for the benefit of
  clients and daemons from before named profiles existed. They carry the active
  custom profile's settings, or `custom`'s when a firmware profile is active —
  the same values selecting `custom` would recall, which is what keeps a "saved
  undervolt, not active" display working and what stops a downgrade taken while
  on `balanced` from writing those settings away. They are output only;
  `custom_profiles` is what is read back.

A state file written before named profiles existed has no `custom_profiles`, so
its top-level `fan_curve`/`tdp`/`undervolt` are migrated into a profile called
`custom` on first load — the same settings, now addressable by name. Nothing
needs to be done by hand.

On `get-state` requests the daemon also populates `temperature` (APU die
temperature in °C), `fan_rpm` (fan speed in RPM), `on_ac` (whether the charger
is plugged in), and `undervolt_available` (whether the `ryzen_smu` kernel module
is present) from live sysfs reads. These are not persisted — they are real-time
sensor values.

On startup the daemon reads this file, resolves what the current power source
calls for if autoswitch is configured, and restores all saved settings before
accepting any connections. If the active profile is a custom one, its fan curve,
TDP, and undervolt are re-applied to the hardware. If it is a firmware profile,
that profile's measured PPT values are written instead — the kernel's `ppt_*`
attributes come up holding a stale 5 W default after boot, and nothing else
restores them.

!!! warning "Downgrading loses named profiles"
    A daemon from before this release reads only the top-level fields and drops
    `custom_profiles` on its next save. The active profile's settings survive via
    the projection above; any other saved profile does not. Copy `state.json`
    aside before downgrading.

!!! note "A corrupt state file is kept, not discarded"
    If `state.json` exists but cannot be parsed, the daemon renames it to
    `state.json.corrupt`, logs a warning, and starts from defaults. Without
    that rename the next command would overwrite the damaged file, taking every
    saved setting with it and leaving nothing to inspect. Repair the `.corrupt`
    copy and move it back to recover.

!!! note "Raw hidrawN paths are not persisted"
    Commands sent with `--device /dev/hidraw2` (a raw path) are applied but
    not saved — raw device numbers are transient and may change across reboots.
    Use `keyboard` or `lightbar` by name for persistent per-zone settings.

---

## Sleep/resume recovery

Several hardware settings are volatile — they are lost when the system enters
sleep (suspend/hibernate) and must be reapplied on resume:

- **Lighting** — RGB lighting is turned off by the hardware on sleep
- **Fan curves** — custom PWM curves reset to firmware defaults on sleep (and on
  every power profile change — see [reconciliation](#custom-fan-curve-reconciliation))
- **TDP (PPT)** — power limits are lost and must be rewritten
- **Undervolt (Curve Optimizer)** — CO offsets reset to stock on every sleep cycle

The daemon monitors D-Bus for `org.freedesktop.login1.Manager.PrepareForSleep`
signals from systemd-logind. When the system resumes (`PrepareForSleep(false)`),
the daemon restores lighting (regardless of profile) and all custom-profile
volatile settings from its saved state. Fan curves, TDP, and undervolt are only
restored when the `custom` profile is active; under a stock profile the firmware
manages fan curves, and the profile's stock PPT values were already written to
hardware when that profile was selected.

This happens transparently with no user intervention. You can verify it worked
by checking the daemon logs after a resume:

```sh
journalctl --user -u z13ctl --since "5 minutes ago"
```

---

## Keyboard reattach recovery

The Z13's keyboard folio is detachable, and it is a separate USB HID device
(`0b05:1a30`) from the lightbar (`0b05:18c6`), which lives in the tablet body.
When you detach the keyboard it loses power and its RGB goes dark; when you
reattach it, the firmware brings it back **unlit** — the previously applied
effect is not restored.

The daemon opens the hidraw devices once at startup, so a reattached keyboard
appears as a brand-new device node that the original handle no longer references.
To recover, the daemon polls sysfs every couple of seconds for the keyboard
reappearing. On detecting it, the daemon reopens the HID devices and re-applies
the saved lighting state — honoring per-device overrides, so a keyboard-specific
color/mode is restored exactly as you last set it.

This requires no user intervention; the keyboard relights within a few seconds of
reattachment. If your `z13ctl setup` udev rules are in place, the reattached node
is granted access automatically. You can verify it in the daemon logs:

```sh
journalctl --user -u z13ctl -f
# On reattach: keyboard reattached; lighting restored
```

!!! note "Daemon required"
    This recovery only happens while the daemon is running. Without it, re-run
    your `apply` command (or press the Armoury Crate button in z13gui) after
    reattaching the keyboard.

---

## Custom fan curve reconciliation

The kernel's `asus-wmi` driver **disables custom fan curves on every
`platform_profile` write**. The write handler ends by clearing the driver's
internal "custom curve enabled" flag for each fan, and the curve is then never
pushed to the EC again until something re-enables it. Nothing is reported to the
process that set the curve — your fans simply return to the firmware's own curve.

This is easy to trigger without meaning to:

- GNOME power modes and `power-profiles-daemon` write `platform_profile` on every
  AC/battery transition and whenever an application requests a profile hold
- Fn+F5 (the ASUS fan-mode hotkey)
- `asusctl`, `tuned`, or any other tool that manages the platform profile

The daemon polls the fan curve device's `pwm_enable` — the driver's own
"is the custom curve live" flag, so it catches every cause — every two seconds.
When the curve has been dropped while your saved settings say it should still be
in force, it writes the curve back. The same applies to the 50% PWM floor that a
sustained TDP above 75 W requires: without this, a power profile change would
release the fans while the power limit stayed in place.

```sh
journalctl --user -u z13ctl -f
# After a profile change: reconciling custom thermal settings
#   reason="saved custom fan curve was disabled" platform_profile=balanced pwm_enable=2
```

The daemon never writes `platform_profile` itself — your desktop stays in charge
of the power profile. Reconciliation only runs while the `custom` profile is
active, so selecting `quiet`, `balanced`, or `performance` with
`z13ctl profile --set` releases the fans to firmware control and keeps them there.

!!! note "Daemon required"
    Without the daemon, a custom fan curve set with `z13ctl fancurve --set` lasts
    only until the next power profile change. The CLI warns about this when it
    applies a curve directly. Since z13ctl 1.2.2 the command also fails with an
    error, rather than reporting success, if the kernel refuses to honour the
    curve it just wrote.

---

## AC/battery autoswitch

When [`z13ctl autoswitch`](commands.md#autoswitch) is configured, the daemon
applies the profile that matches the power source on every plug and unplug.

The watcher is **edge-triggered** on the mains adapter's `online` attribute
under `/sys/class/power_supply`, and never reads or writes `platform_profile`.
That is what makes it safe to run alongside `power-profiles-daemon`: it reacts
only to a value that neither z13ctl nor PPD nor the desktop can write, so the
feedback loop that would produce a write-fight does not exist. The visible
consequence, and the intended semantics, is that a profile you choose by hand
stays in force until the power source actually changes.

Detection uses two triggers and one observation. UPower's `OnBattery` property
wakes the watcher immediately where UPower is running; a 2-second poll is the
backstop that keeps this working in Steam Gaming Mode, on a bare session, or
wherever UPower is absent. Either way the answer comes from sysfs, so there is
one behaviour to reason about.

A transition is confirmed on the following observation before anything is
applied, so a change lands about two seconds after the event. That settle window
lets the desktop's own transition write land first — a custom fan curve would
otherwise be dropped in the gap before the [reconciler](#custom-fan-curve-reconciliation)
notices — and stops a loose USB-C connector from driving a full power-limit, fan,
and SMU write per bounce.

Devices are selected by their `type` file, not by having an `online` file. On the
Z13 the detachable keyboard registers as `hid-*-battery-N` and the USB-C ports as
`ucsi-source-psy-*`, and all of them expose `online`; only `type` `Mains` is the
charger. If no `Mains` supply exists at all — a VM, a desktop, a driver not yet
bound — the source is treated as *unknown* and the watcher does nothing, rather
than concluding the machine is on battery.

There is deliberately no autoswitch hook in the resume path. Go timers do not
advance across suspend, so the watcher's armed timer fires promptly after resume
and handles an unplug-during-sleep on its own; duplicating the logic would race
it and apply twice for one event.

!!! note "GNOME's Automatic Power Saver"
    That setting triggers on low battery rather than on unplugging, so it can
    still move a firmware profile after autoswitch has acted. z13ctl yields
    between transitions by design. Give a side an empty target to hand it to your
    desktop entirely.

---

## Armoury Crate button

The daemon watches the ASUS WMI hotkeys input device for `KEY_PROG3` (key code
202) — the physical Armoury Crate button on the Z13. When pressed, it broadcasts
a `gui-toggle` event to all connected subscribers.

The device is read **non-exclusively**. It also carries `SW_TABLET_MODE`, and an
exclusive `EVIOCGRAB` would take those tablet-mode transitions away from
libinput — leaving the desktop convinced the machine is still a tablet and
suppressing the detachable cover keyboard when it is attached after login. z13ctl
grabbed the device up to v1.2.0 and did exactly that; it no longer does.

One consequence of reading shared: the Armoury Crate keypress also reaches your
desktop. No mainstream desktop binds `KEY_PROG3` by default, but if yours does,
that binding will fire alongside the `gui-toggle` event — unbind it there, or run
the daemon with `--no-button`.

!!! note "Silent failure if another process grabs the device"
    Because z13ctl no longer grabs, a different process holding an exclusive grab
    will silently receive all events instead, and the watcher will sit idle with
    nothing to report. If button presses do nothing, check what else has the
    device open (`sudo fuser -v /dev/input/eventN`).

External tools can subscribe to this event via the API:

```go
ch, cancel, err := api.Subscribe([]string{"gui-toggle"})
```

See the [API](api.md) page for details.

### InputPlumber compatibility

On gaming distributions such as Bazzite and ChimeraOS, [InputPlumber](https://github.com/ShadowBlip/InputPlumber)
ships a built-in device profile for the ROG Flow Z13 (`50-rog_flow_z13.yaml`)
that grabs `"Asus WMI hotkeys"` as a managed source device. This creates an
exclusive evdev conflict: z13ctl cannot open the device and will log:

```
button watcher stopped; retrying err="open /dev/input/eventN: permission denied"
```

**Workaround:** Create an override config that marks `"Asus WMI hotkeys"` as
`ignore: true`. This tells InputPlumber to leave that device unmanaged so z13ctl
can open it, while preserving all other InputPlumber functionality (controller
emulation, touchpad, etc.).

First, check whether the built-in config exists on your system:

```sh
cat /usr/share/inputplumber/devices/50-rog_flow_z13.yaml
```

If found, create the override directory if needed, then save the override file:

```sh
sudo mkdir -p /etc/inputplumber/devices.d
```

Create `/etc/inputplumber/devices.d/50-rog_flow_z13.yaml` with `ignore: true` added
to the keyboard source device:

```yaml
# /etc/inputplumber/devices.d/50-rog_flow_z13.yaml
version: 1
kind: CompositeDevice
name: ASUS ROG Flow Z13 (2025)
single_source: false

matches:
  - dmi_data:
      board_name: GZ302EA
      sys_vendor: ASUSTeK COMPUTER INC.

source_devices:
  - group: keyboard
    ignore: true        # leave unmanaged so z13ctl can read this device
    evdev:
      name: Asus WMI hotkeys
      handler: event*

options:
  auto_manage: true

target_devices:
  - xbox-elite
  - touchpad
  - keyboard

capability_map_id: flw1
```

!!! note
    Always place overrides under `/etc/inputplumber/devices.d/` — never edit files
    under `/usr/share/inputplumber/devices/` directly, as those are owned by the
    package and will be overwritten on upgrades.

    If your model's `board_name` differs from `GZ302EA`, verify it with:
    ```sh
    cat /sys/class/dmi/id/board_name
    ```

After saving the file, restart InputPlumber:

```sh
sudo systemctl restart inputplumber.service
```

Then restore device permissions that the previous InputPlumber instance may have
changed (InputPlumber restricts device node access while managing a device, and may
not fully restore permissions on shutdown):

```sh
sudo z13ctl setup --perms-only
```

Then confirm z13ctl can read the button device:

```sh
journalctl --user -u z13ctl.service -f
# Should show: watching Armoury Crate button (shared, non-exclusive) path=/dev/input/eventN
```

Alternatively, disable the button watcher entirely and let InputPlumber handle
the button exclusively:

```sh
z13ctl --no-button daemon
```
