---
title: The Daemon
description: Running the voltaire daemon, what it does in the background, and its state file.
---

# The Daemon

The voltaire daemon is a small background process that runs as your user. It
remembers your settings, puts them back when the kernel or firmware drops
them, and is what the GUI talks to. Every command works without it, but
without it nothing is restored after a reboot, a sleep or a power profile
change.

## Running the daemon {#running-the-daemon}

voltaire ships three systemd **user** units:

| Unit | What it does |
|------|--------------|
| `voltaire.socket` | Creates the daemon's socket, so the daemon starts on first use |
| `voltaire.service` | The daemon itself; restarted if it fails |
| `voltaire-gui.service` | The GUI, started with your graphical session |

They start with your graphical session, so they work on a desktop (KDE, GNOME
and others) and in Steam Gaming Mode.

### Enabling it

The distribution packages install and enable all three for every user. After
installing, log out and back in, or start them now:

```sh
systemctl --user start voltaire.socket voltaire.service voltaire-gui.service
```

From a source checkout, `make install-service` installs and enables them in
`~/.config/systemd/user/`.

### Managing it

```sh
systemctl --user status voltaire.service     # is it running?
journalctl --user -u voltaire -f             # follow its log
systemctl --user restart voltaire.service    # restart it
```

To run it with a [global flag](/reference/commands#global-flags) such as
`--no-sleep-release` or `--no-button`, override the unit's command line:

```sh
systemctl --user edit voltaire.service
#   [Service]
#   ExecStart=
#   ExecStart=voltaire --no-sleep-release daemon
systemctl --user restart voltaire.service
```

### Removing it

To stop using the daemon without uninstalling voltaire:

```sh
systemctl --user disable --now voltaire.socket voltaire.service
```

From a source checkout, `make uninstall-service` stops and removes all three
units. To remove voltaire entirely, see [Uninstall](/installation#uninstall).

### Running without systemd {#running-without-systemd}

Start the daemon in a terminal, for testing or on a system without systemd:

```sh
voltaire daemon
voltaire --no-button daemon    # without watching the hardware button
```

It listens on a Unix socket at `$XDG_RUNTIME_DIR/voltaire/voltaire.sock`
(`/tmp/voltaire/voltaire.sock` when `XDG_RUNTIME_DIR` is not set). It also
answers on the old z13ctl path, `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock`, so
older clients such as the Decky plugin keep working. Client authors: see
[Socket protocol](/dev/socket-protocol).

## What the daemon does {#what-the-daemon-does}

### At startup

The daemon puts back everything you last set: lighting, the profile and its
fan curve, power limits and undervolt, the charge limit, CPU boost and the
controller's vibration strength. With [autoswitch](/guide/profiles-and-power#autoswitch)
on, it starts on the profile for the power source you are on now.

### Keeps your fan curve and power limits in force {#custom-fan-curve-reconciliation}

A power profile change drops a custom fan curve, and the firmware puts its own
power limits back on a profile change or a fan release. That happens whenever
your desktop changes power mode, when you plug in or unplug the charger, or
when another tool such as `asusctl` or a hotkey switches profile.

While a custom profile is active, the daemon notices and writes your curve and
limits back within a couple of seconds. On a firmware profile it leaves the
fans and limits to the firmware. It never changes the system power profile
itself, so your desktop stays in charge of it.

### Sleep and resume {#sleep-and-resume}

Before the machine sleeps, the daemon hands the fans back to the firmware,
because some machines keep a custom curve running while asleep and the fans
never stop. If a high power limit is in force it lowers that first, and if it
cannot, it leaves the fans alone. This only happens while a custom profile is
active, and `--no-sleep-release` turns it off. The daemon also turns the
lighting off, since some zones (the Z13's lightbar) would otherwise stay lit.

After resume it waits for the hardware to be ready, then restores the
lighting, your custom profile's fan curve, power limits and undervolt (which
the processor forgets during sleep), and the controller's vibration strength.
On a device with a TDP slider, the slider's TDP and undervolt come back too.

If your machine wakes up again right after suspending, see
[Troubleshooting](/troubleshooting).

### Keyboard reattach

The Z13's detachable keyboard comes back unlit when you reattach it. The
daemon sees it return and puts its lighting back within a few seconds.

### AC/battery autoswitch

With [autoswitch](/guide/profiles-and-power#autoswitch) configured, the daemon
switches profile about two seconds after you plug in or unplug the charger.
Only a real change of power source triggers it, so a profile you choose by
hand stays until the next plug or unplug, and it does not fight
power-profiles-daemon in between.

### Full charge once

When you ask for [a full charge once](/guide/battery#full-charge-once), the
daemon watches the battery and puts your charge limit back when it is full.

### Steam Gaming Mode

On the ROG Xbox Ally X, Steam sets the power limits and charge limit in Gaming
Mode, so voltaire offers those controls on the desktop only. When you switch
between Gaming Mode and the desktop the daemon restarts itself so its controls
follow. See [Steam Gaming Mode](/guide/gaming-mode).

### Checking what it did

The daemon logs each of these to the journal:

```sh
journalctl --user -u voltaire -b
```

## State file {#state-file}

The daemon saves your settings to `~/.local/state/voltaire/state.json` (under
`$XDG_STATE_HOME` if set) after every change, and reads it at startup. You do
not need to edit it.

- **Upgrading from z13ctl:** the old `~/.local/state/z13ctl/state.json` is
  copied here on first start. The old file is left in place.
- **A corrupt file is kept.** If the file cannot be read, the daemon renames it
  to `state.json.corrupt`, logs a warning and starts from defaults. Repair the
  copy and move it back to recover your settings.
- **Downgrading to z13ctl** keeps the active profile's settings but loses your
  other named profiles. Copy `state.json` aside first.

Other files voltaire uses are listed in [Configuration](/reference/configuration).
