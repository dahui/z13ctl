---
title: Configuration files
description: Every file voltaire reads or writes, and its environment variables.
---

# Configuration files

Every file voltaire reads or writes, where it lives, and the environment
variables it honors. Most of these are written for you; the only files meant
for hand editing are `gui.toml` and a custom theme.

| File | Written by | Purpose |
|------|------------|---------|
| `~/.config/voltaire/config.toml` | the GUI | GUI preferences |
| `~/.config/voltaire/gui.toml` | you | Drawer layout and screen edge |
| `~/.config/voltaire/theme.toml`, `theme.css` | you | A custom theme |
| `~/.local/state/voltaire/state.json` | the daemon | Your saved settings and profiles |
| `/etc/udev/rules.d/99-voltaire.rules` | `voltaire setup` | Device access for your group |
| `/etc/systemd/system/voltaire-perms.service` | `voltaire setup` | Reapplies that access at boot |
| `/etc/inputplumber/devices.d/` | `voltaire setup --inputplumber-passthrough` | Opt-in InputPlumber override |

`~/.config` and `~/.local/state` follow `$XDG_CONFIG_HOME` and
`$XDG_STATE_HOME` when those are set.

## config.toml {#config-toml}

`~/.config/voltaire/config.toml` holds the GUI's preferences. The GUI writes it
whenever you change one of these settings, so you rarely need to open it; if
you do edit it by hand, restart the GUI afterwards.

```toml
theme = "catppuccin-mocha"
accent = "sapphire"
button_press = "window"
refresh_autoswitch = "true"
refresh_ac = "120"
refresh_battery = "60"
quickbar_controls = "profile,battery,lighting"
gamepad_hints = "off"
```

| Key | Meaning |
|-----|---------|
| `theme` | Built-in theme id (`voltaire-gui --list-themes`); `rog-dark` when unset |
| `accent` | Accent color within the theme; unset uses the theme's default |
| `button_press` | What one press of the hardware button opens: `quickbar` (default) or `window`. A double press opens the other |
| `refresh_autoswitch` | `true` to change the screen's refresh rate with the power source; unset is off |
| `refresh_ac` | Refresh rate in Hz to use on AC power |
| `refresh_battery` | Refresh rate in Hz to use on battery |
| `quickbar_controls` | The drawer layout saved from the Settings tab: section ids in order, comma-separated; `none` is an empty drawer |
| `gamepad_hints` | `off` hides a control's hint when the controller rests on it; unset shows them |

See [Themes](/guide/themes) and [GUI configuration](/guide/gui-configuration).

## gui.toml {#gui-toml}

`~/.config/voltaire/gui.toml` does not exist until you create it. voltaire
never writes it, so your comments and layout are safe. It has one table:

```toml
[quickbar]
edge = "left"
controls = ["profile", "battery", "lighting"]
```

| Key | Meaning |
|-----|---------|
| `edge` | The side of the screen the drawer opens from: `right` (default) or `left` |
| `controls` | The drawer's sections, in order. Takes precedence over the layout saved in `config.toml`, and the Settings tab then shows it read-only |

The section ids are listed in [Drawer sections](/guide/gui-configuration#drawer-sections).
Restart the GUI after editing: `systemctl --user restart voltaire-gui`.

## Custom themes {#custom-themes}

`~/.config/voltaire/theme.toml` defines your own colors and takes priority over
every built-in theme. `~/.config/voltaire/theme.css` replaces the GUI's
stylesheet entirely, for full control. Start from the default with
`voltaire-gui --print-theme > ~/.config/voltaire/theme.toml`. See
[Themes](/guide/themes).

## State file {#state-file}

`~/.local/state/voltaire/state.json` is where the daemon keeps everything you
set: lighting, profiles and their settings, the charge limit, autoswitch, and
the button choice. It is rewritten after every change and read at startup. Do
not edit it while the daemon runs.

If the file is damaged, the daemon keeps it as `state.json.corrupt` and starts
from defaults. More in [The Daemon](/reference/daemon#state-file).

## Daemon socket {#daemon-socket}

The daemon listens on `$XDG_RUNTIME_DIR/voltaire/voltaire.sock`, usually
`/run/user/<uid>/voltaire/voltaire.sock` (`/tmp/voltaire/voltaire.sock` if
`XDG_RUNTIME_DIR` is unset). It also answers on the old z13ctl path,
`$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock`, for older clients.

## Files setup writes {#setup-files}

[`voltaire setup`](/reference/commands#setup) writes two files, and removes
both with `--remove`:

- `/etc/udev/rules.d/99-voltaire.rules` gives your group access to this
  machine's devices as they appear.
- `/etc/systemd/system/voltaire-perms.service` runs
  `voltaire setup --perms-only` at boot, for settings that udev does not
  cover.

Both say which machine and group they were written for. See
[What setup does](/installation#what-setup-does).

With `--inputplumber-passthrough`, setup also writes an override for your
machine into `/etc/inputplumber/devices.d/`, so InputPlumber leaves the
lighting device visible. It is rebuilt from InputPlumber's own config on every
setup run, and removed by `--inputplumber-passthrough=false` or `--remove`. See
[Ally lighting](/guide/buttons-and-controllers#ally-lighting).

## Installed files {#installed-files}

The distribution packages install:

| Path | Purpose |
|------|---------|
| `/usr/bin/voltaire`, `/usr/bin/voltaire-gui` | The programs |
| `/usr/bin/z13ctl`, `/usr/bin/z13gui` | Old names, kept as links for existing scripts |
| `/usr/lib/systemd/user/voltaire.socket`, `voltaire.service`, `voltaire-gui.service` | The user units ([Running the daemon](/reference/daemon#running-the-daemon)) |
| `/usr/lib/udev/rules.d/99-voltaire-gamepad.rules` | Lets the active user read game controllers, for controller navigation in the GUI |
| `/usr/share/applications/voltaire-gui.desktop` | The GUI's menu entry |

A source install (`make install`, `make install-service`) puts the programs in
`/usr/local/bin` and the units in `~/.config/systemd/user/`.

## Environment variables {#environment-variables}

The GUI reads these. To set one for the GUI service, run
`systemctl --user edit voltaire-gui.service` and add it under `[Service]` as
`Environment=VOLTAIRE_GUI_NO_GAMEPAD=1`.

| Variable | Effect |
|----------|--------|
| `VOLTAIRE_GUI_NO_GAMEPAD` | Any value turns off controller input in the GUI |
| `VOLTAIRE_GUI_SCALE` | In Steam Gaming Mode, overrides the GUI's size (a number from 1 to 3; detected from the screen when unset) |

The old names `Z13GUI_NO_GAMEPAD` and `Z13GUI_SCALE` still work when the new
ones are unset.

The CLI, daemon and GUI also follow the standard locations:

| Variable | Used for |
|----------|----------|
| `XDG_CONFIG_HOME` | Where `voltaire/config.toml`, `gui.toml` and themes live (default `~/.config`) |
| `XDG_STATE_HOME` | Where `voltaire/state.json` lives (default `~/.local/state`) |
| `XDG_RUNTIME_DIR` | Where the daemon socket lives (default `/tmp` when unset) |
