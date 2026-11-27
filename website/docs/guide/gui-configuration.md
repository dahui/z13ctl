---
title: Customizing the GUI
description: Choose the drawer's sections and their order, move it to the left edge, and set the GUI's preferences.
---

# Customizing the GUI

Most of the GUI's preferences are set from the GUI itself and saved for you.
This page covers what you can change, and the files behind it for when you
want to edit them by hand. For colors, see [Themes](/guide/themes).

## Drawer sections {#drawer-sections}

### In the GUI

Open the full window's **Settings** tab. The **Quickbar** card lists every
drawer section your device supports, each with a switch to show or hide it and
▲/▼ buttons to move it. The drawer changes as you go, and **Reset to default**
puts your device's standard layout back. The buttons work with a mouse, touch
or a controller.

Your layout is saved in `~/.config/voltaire/config.toml` as
`quickbar_controls`.

### By hand, in gui.toml

You can also fix the layout in `~/.config/voltaire/gui.toml`, which does not
exist until you create it:

```toml
[quickbar]
controls = ["profile", "battery", "lighting"]
```

The list is both which sections appear and their order. A `controls` list
here takes precedence over the Settings card: the card then shows this layout
without letting you change it, and says so. Delete the `controls` line to
manage the layout from Settings again. Edits to `gui.toml` take effect the
next time the GUI starts:

```sh
systemctl --user restart voltaire-gui
```

| ID | Section | Default |
|----|---------|---------|
| `tdp` | The TDP slider, on a device whose TDP is one setting (the OneXPlayer X2 Mini Pro). It is remembered across reboots | on |
| `profile` | The firmware profiles and the custom profile picker. On a single-slider device, a **Saved profile** picker | on |
| `autoswitch` | The AC/battery autoswitch switch and its two targets | on |
| `battery` | The charge limit slider | on |
| `charge-once` | Charge to 100% once, then go back to the limit | off |
| `cpu-boost` | The CPU boost switch | off |
| `undervolt` | The undervolt offset, on a single-slider device | off |
| `brightness` | The screen's brightness | off |
| `refresh-rate` | The screen's refresh rate (KDE Plasma only; not in Steam Gaming Mode) | off |
| `volume` | The sound volume | off |
| `rumble` | The built-in controller's vibration strength | off |
| `lighting` | The whole RGB block: zones, effect, colors, speed and brightness | on |

The **Default** column is the built-in layout. Some devices ship their own:

| Device | Default drawer |
|--------|----------------|
| ASUS ROG Flow Z13 (2025) | `profile`, `autoswitch`, `battery`, `lighting` |
| ASUS ROG Xbox Ally X | `profile`, `brightness`, `volume`, `cpu-boost`, `refresh-rate`, `lighting` |
| OneXPlayer X2 Mini Pro | `tdp`, `cpu-boost`, `brightness`, `volume`, `rumble`, `refresh-rate`, `lighting` |

Things to know:

- **A section your machine cannot offer is left out**, whether or not you
  list it, so one file works on more than one machine.
- **An unknown ID is skipped** and logged as a warning; run `voltaire-gui -d`
  to see it.
- **`controls = []` means an empty drawer.** Delete the line (or the file) to
  get the default back.
- **Group headings follow their sections**, so a reordered drawer still reads
  correctly.
- **The bottom bar is fixed**: the theme and expand buttons are not part of
  the list.

## Move the drawer to the left edge

The drawer slides in from the right. To use the left edge instead, set `edge`
in `~/.config/voltaire/gui.toml`:

```toml
[quickbar]
edge = "left"
```

The values are `right` (the default) and `left`; the top and bottom edges are
not available. Restart the GUI to apply it.

## Preferences in config.toml

The GUI saves its preferences in `~/.config/voltaire/config.toml` as you
change them. You rarely need to edit it, but you can; changes apply the next
time the GUI starts.

```toml
theme = "catppuccin-mocha"
accent = "sapphire"
button_press = "window"
refresh_autoswitch = "true"
refresh_ac = "180"
refresh_battery = "60"
gamepad_hints = "off"
```

| Key | Set from | Meaning |
|-----|----------|---------|
| `theme` | Theme picker | The built-in theme; the default is `rog-dark` |
| `accent` | Theme picker | The accent color, for themes that have accent variants |
| `button_press` | Settings → VOLTAIRE | What a single press opens: `quickbar` (default) or `window` |
| `refresh_autoswitch` | Dashboard → Display | `true` changes the refresh rate with the power source |
| `refresh_ac`, `refresh_battery` | Dashboard → Display | The rate for each power source, in whole hertz |
| `quickbar_controls` | Settings → Quickbar | The drawer layout, as comma-separated IDs; `none` is an empty drawer |
| `gamepad_hints` | Settings → VOLTAIRE | `off` hides hints on controller focus |

[Configuration files](/reference/configuration#config-toml) has the full
reference for this file and `gui.toml`.

## Environment variables

A few environment variables change the GUI's behavior, such as the UI scale in
Steam Gaming Mode or ignoring controllers entirely.
[Configuration files](/reference/configuration#environment-variables) lists
them. Set one on the GUI's service with `systemctl --user edit voltaire-gui`:

```ini
[Service]
Environment=VOLTAIRE_GUI_SCALE=1.5
```

## Command-line options

| Option | Effect |
|--------|--------|
| `-d`, `--debug` | Log debug output, including GTK's |
| `--version` | Print the version |
| `--list-themes` | List the built-in themes |
| `--print-theme` | Print a starter custom theme; see [Themes](/guide/themes#create-a-custom-theme) |
