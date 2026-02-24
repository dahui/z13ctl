# Daemon

The z13ctl daemon is a long-running background process that provides three things
ordinary one-shot CLI invocations cannot:

- **State persistence** — saves your last-applied lighting, profile, and battery
  settings to `~/.local/state/z13ctl/state.json` and restores them automatically
  at every boot.
- **HID device ownership** — holds the hidraw devices open continuously so that
  commands arrive instantly rather than waiting to reopen the device each time.
- **Armoury Crate button events** — captures `KEY_PROG3` (the dedicated Armoury
  Crate button) and broadcasts a `gui-toggle` event to any connected subscribers
  (see [API](api.md)).

All CLI commands (`apply`, `brightness`, `off`, `profile`, `batterylimit`,
`bootsound`, `paneloverdrive`) automatically route through the daemon socket when
it is running. If the daemon is not running they fall back to direct hardware or
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

On startup the daemon reads this file and restores all saved settings before
accepting any connections.

!!! note "Raw hidrawN paths are not persisted"
    Commands sent with `--device /dev/hidraw2` (a raw path) are applied but
    not saved — raw device numbers are transient and may change across reboots.
    Use `keyboard` or `lightbar` by name for persistent per-zone settings.

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

If another tool needs exclusive access to the button device, start the daemon
with `--no-button` to skip the button watcher entirely.
