---
title: GUI Configuration
description: The voltaire-gui config file, theme priority, environment variables, and command-line flags.
---

## Config file

voltaire-gui stores its configuration in `~/.config/voltaire/config.toml`.
This is the file the app itself writes: the theme picker, the Settings tab's
preferences and its Quickbar card, and the dashboard's refresh-rate switch all
save here. A key the app has never been told about is simply absent.

```toml
theme = "catppuccin-mocha"
accent = "sapphire"
button_press = "window"
refresh_autoswitch = "true"
refresh_ac = "180"
refresh_battery = "60"
quickbar_controls = "profile,battery,lighting,refresh-rate"
```

| Key | Description |
|-----|-------------|
| `theme` | Built-in theme ID (see `voltaire-gui --list-themes` or [Theming](/voltaire/gui/theming/)) |
| `accent` | Accent color variant for themes that support it; `""` uses the theme default |
| `button_press` | Which surface a **single** press of the hardware button opens: `quickbar` (the default) or `window`. A double press opens the other one |
| `refresh_autoswitch` | `true` to change the screen's refresh rate when the power source changes; absent is off |
| `refresh_ac`, `refresh_battery` | The rate, in whole hertz, to select on each source. A rate the screen does not offer is left alone rather than rounded to the nearest one |
| `quickbar_controls` | The drawer layout saved by the Settings tab's Quickbar card: control IDs in order, comma-separated (see below). `none` means an empty drawer; absent means the default layout |

If no config file exists, voltaire-gui defaults to the `rog-dark` theme.

You can edit this file by hand. Changes take effect the next time voltaire-gui
starts.

On first run after upgrading from z13gui, everything in `~/.config/z13gui/`
is **copied** to `~/.config/voltaire/` — never moved, so a downgrade still
finds its config. The copy only happens while the voltaire directory does not
exist, so it can never overwrite settings you have saved since.

## Choosing which sections appear

The easy way is the full window's **Settings** tab: its **Quickbar** card lists
every section this machine supports, each with a switch to show or hide it and
▲/▼ to move it. Changes apply straight away, and **Reset to default** puts the
standard layout back.
The buttons work the same with a mouse, touch or a controller.

Two sections are off by default and only appear if you add them there:
**CPU boost** and **Refresh rate** (the live rate only; the per-power-source
rates stay on the Dashboard). The standard drawer is laid out to fit without
scrolling, so each one you add costs a little of that.

The card saves to `~/.config/voltaire/config.toml`. You can also write the
layout by hand in `~/.config/voltaire/gui.toml`, which does not exist by
default. A `controls` list there **takes precedence**: the Settings card then
shows that layout without letting you change it, and says so, so a file you
wrote is never overwritten.

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
| `profile` | The device's firmware profiles plus the Custom profile picker. On a device with a pinned profile (the OneXPlayer X2 Mini Pro) the firmware profiles are one **Default** button: the TDP slider, with no saved profile running |
| `tdp` | The TDP slider, on a device whose TDP is one setting rather than part of a profile (the OneXPlayer X2 Mini Pro). It sets the machine's TDP and is remembered across reboots. With a custom profile running, moving it leaves the profile (unchanged) for the TDP setting; change a profile's TDP on the Profiles page |
| `autoswitch` | The AC/battery autoswitch row and its two targets |
| `battery` | The charge-limit slider, where the device has a charge limit to set |
| `lighting` | The whole RGB block — zone tabs, effect, colours, speed, brightness |
| `cpu-boost` | The CPU boost switch (off by default) |
| `refresh-rate` | The screen's refresh rate, where the desktop can report one — not in Steam Gaming Mode, where Steam sets it (off by default) |

The list is both the contents *and* the order, so moving an entry moves the
section. Leaving one out hides it. The `TDP AND POWER`, `DISPLAY` and `RGB`
headings follow their sections rather than staying put, so a reordered drawer
still reads correctly.

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
- The bottom bar — the theme button and the expand button that opens the full
  window — is fixed and not part of the list.
- Edits to `gui.toml` take effect the next time voltaire-gui starts:
  `systemctl --user restart voltaire-gui`. Changes made in Settings apply at
  once.

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
| `VOLTAIRE_GUI_OPEN_FULL` | Set to `1` to open the full window at startup, without the double press; a tab name (`dashboard`, `profiles`, `settings`) opens on that page, and `color` opens the colour picker over the dashboard |
| `VOLTAIRE_GUI_DUMP_FOCUS` | Development aid: with `-d`, logs every controller focus grid at startup, so a change to the layout can be diffed |

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
