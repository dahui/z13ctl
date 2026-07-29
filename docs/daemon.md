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

To disable the Armoury Crate button watcher (e.g., when another tool such as
a Steam controller mapper needs exclusive access to the button device):

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

`profile` accepts `quiet`, `balanced`, `performance`, or the virtual `custom`.

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

### State and events

| Command | Request | Response |
|---|---|---|
| Full state | `{"cmd":"get-state"}` | `ok`, `state` |
| Subscribe | `{"cmd":"subscribe","events":["gui-toggle"]}` | `ok`, then streamed events |

`get-state` merges persisted state with live sysfs reads — see
[State file](#state-file). Each streamed event is a response object carrying an
`event` field, e.g. `{"event":"gui-toggle"}`.

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
- `profile` — last-set performance profile
- `battery_limit` — last-set charge limit
- `fan_curve` — custom curve points and mode (applied to both fans)
- `tdp` — PL1, PL2, and PL3 power limits in watts
- `undervolt` — CPU Curve Optimizer offset and active flag (preserved across
  profile switches for recall; `undervolt-get` includes the current profile so
  clients can distinguish active vs saved values)

On `get-state` requests the daemon also populates `temperature` (APU die
temperature in °C), `fan_rpm` (fan speed in RPM), and `undervolt_available`
(whether the `ryzen_smu` kernel module is present) from live sysfs reads.
These are not persisted — they are real-time sensor values.

On startup the daemon reads this file and restores all saved settings before
accepting any connections. If the last profile was `custom`, saved fan curves,
TDP values, and undervolt offsets are re-applied to the hardware. If the last
profile was a stock one, that profile's measured PPT values are written instead
— the kernel's `ppt_*` attributes come up holding a stale 5 W default after
boot, and nothing else restores them.

!!! note "Raw hidrawN paths are not persisted"
    Commands sent with `--device /dev/hidraw2` (a raw path) are applied but
    not saved — raw device numbers are transient and may change across reboots.
    Use `keyboard` or `lightbar` by name for persistent per-zone settings.

---

## Sleep/resume recovery

Several hardware settings are volatile — they are lost when the system enters
sleep (suspend/hibernate) and must be reapplied on resume:

- **Lighting** — RGB lighting is turned off by the hardware on sleep
- **Fan curves** — custom PWM curves reset to firmware defaults on sleep
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

## Armoury Crate button

The daemon watches the ASUS WMI hotkeys input device for `KEY_PROG3` (key code
202) — the physical Armoury Crate button on the Z13. When pressed, it broadcasts
a `gui-toggle` event to all connected subscribers.

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
can grab it exclusively, while preserving all other InputPlumber functionality
(controller emulation, touchpad, etc.).

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
    ignore: true        # allow z13ctl to grab this device exclusively
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

Then confirm z13ctl can grab the button:

```sh
journalctl --user -u z13ctl.service -f
# Should show: watching Armoury Crate button path=/dev/input/eventN
```

Alternatively, disable the button watcher entirely and let InputPlumber handle
the button exclusively:

```sh
z13ctl --no-button daemon
```
