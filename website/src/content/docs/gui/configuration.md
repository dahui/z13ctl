---
title: GUI Configuration
description: The voltaire-gui config file, theme priority, environment variables, and command-line flags.
---

## Config file

voltaire-gui stores its configuration in `~/.config/voltaire/config.toml`.
This file is updated automatically when you change themes using the in-app
theme picker.

```toml
theme = "catppuccin-mocha"
accent = "sapphire"
```

| Key | Description |
|-----|-------------|
| `theme` | Built-in theme ID (see `voltaire-gui --list-themes` or [Theming](/voltaire/gui/theming/)) |
| `accent` | Accent color variant for themes that support it; `""` uses the theme default |

If no config file exists, voltaire-gui defaults to the `rog-dark` theme.

You can edit this file by hand. Changes take effect the next time voltaire-gui
starts.

On first run after upgrading from z13gui, everything in `~/.config/z13gui/`
is **copied** to `~/.config/voltaire/` — never moved, so a downgrade still
finds its config. The copy only happens while the voltaire directory does not
exist, so it can never overwrite settings you have saved since.

## Choosing which sections appear

`~/.config/voltaire/gui.toml` controls which sections the drawer shows and in
what order. It does not exist by default, and without it you get the standard
layout.

```toml
[quickbar]
edge = "left"
controls = ["profile", "battery", "lighting"]
```

`edge` is which side of the screen the drawer slides in from — `right` (the
default) or `left`. `top` and `bottom` are not available: the drawer is a
fixed-width column, so a horizontal edge would need every section laid out along
the other axis. Asking for one logs a warning saying so and uses `right`.

| ID | Section |
|----|---------|
| `profile` | The three firmware profiles plus the Custom button |
| `autoswitch` | The AC/battery autoswitch row and its two targets |
| `battery` | The charge-limit slider |
| `lighting` | The whole RGB block — zone tabs, effect, colours, speed, brightness |

The list is both the contents *and* the order, so moving an entry moves the
section. Leaving one out hides it. The `TDP AND POWER` and `RGB` headings follow
their sections rather than staying put, so a reordered drawer still reads
correctly.

RGB is one entry rather than five because the drawer already decides which of
its parts to show from the selected effect — hiding the whole block is the
choice that makes sense.

Some notes on the edges:

- A section the machine cannot do is dropped whether or not you listed it, so
  the same file works on more than one machine.
- An unknown ID is skipped and logged; run with `-d` to see the warning naming
  it. A typo is not silently ignored.
- `controls = []` really does mean an empty drawer. Delete the key (or the file)
  to get the defaults back.
- The bottom bar — the theme button and the firmware toggles — is fixed and not
  part of the list.
- Changes take effect the next time voltaire-gui starts:
  `systemctl --user restart voltaire-gui`.

## Theme priority

voltaire-gui resolves its theme using the following priority chain (first
match wins):

1. `~/.config/voltaire/theme.toml` — custom color definitions
2. `~/.config/voltaire/theme.css` — full CSS override
3. `~/.config/voltaire/config.toml` `theme = "id"` — built-in theme selection
4. Compiled-in default (ROG Dark)

See [Theming](/voltaire/gui/theming/) for the full theming guide.

## Environment variables

| Variable | Description |
|----------|-------------|
| `VOLTAIRE_GUI_SCALE` | Override CSS scale factor in gamescope mode (default: auto from output resolution) |
| `VOLTAIRE_GUI_NO_GAMEPAD` | Set to `1` to disable gamepad input entirely |
| `VOLTAIRE_GUI_OPEN_FULL` | Set to `1` to open the full window at startup, without the double press |

The pre-rename names (`Z13GUI_SCALE`, `Z13GUI_NO_GAMEPAD`) are honoured
through the whole 2.x line when the new name is unset, and removed at 3.0.

voltaire-gui also sets `GTK_A11Y=none` internally to disable the GTK4
accessibility bridge. This prevents D-Bus timeouts when running under systemd
where the AT-SPI bus may be unavailable.

## Command-line flags

| Flag | Description |
|------|-------------|
| `--debug`, `-d` | Enable debug logging (includes GTK messages) |
| `--version` | Print version and exit |
| `--print-theme` | Print the default theme.toml to stdout |
| `--list-themes` | List all built-in theme IDs and names |

`--print-theme` is the recommended starting point for a custom theme:

```sh
mkdir -p ~/.config/voltaire
voltaire-gui --print-theme > ~/.config/voltaire/theme.toml
```
