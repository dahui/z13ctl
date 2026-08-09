# Configuration

## Config file

z13gui stores its configuration in `~/.config/z13gui/config.toml`. This file
is updated automatically when you change themes using the in-app theme picker.

```toml
theme = "catppuccin-mocha"
accent = "sapphire"
```

| Key | Description |
|-----|-------------|
| `theme` | Built-in theme ID (see `z13gui --list-themes` or [Theming](theming.md)) |
| `accent` | Accent color variant for themes that support it; `""` uses the theme default |

If no config file exists, z13gui defaults to the `rog-dark` theme.

You can edit this file by hand. Changes take effect the next time z13gui starts.

---

## Theme priority

z13gui resolves its theme using the following priority chain (first match wins):

1. `~/.config/z13gui/theme.toml` — custom color definitions
2. `~/.config/z13gui/theme.css` — full CSS override
3. `~/.config/z13gui/config.toml` `theme = "id"` — built-in theme selection
4. Compiled-in default (ROG Dark)

See [Theming](theming.md) for the full theming guide.

---

## Environment variables

| Variable | Description |
|----------|-------------|
| `Z13GUI_SCALE` | Override CSS scale factor in gamescope mode (default: auto from output resolution) |
| `Z13GUI_NO_GAMEPAD` | Set to `1` to disable gamepad input entirely |

z13gui also sets `GTK_A11Y=none` internally to disable the GTK4 accessibility
bridge. This prevents D-Bus timeouts when running under systemd where the
AT-SPI bus may be unavailable.

---

## Command-line flags

| Flag | Description |
|------|-------------|
| `--debug`, `-d` | Enable debug logging (includes GTK messages) |
| `--version` | Print version and exit |
| `--print-theme` | Print the default theme.toml to stdout |
| `--list-themes` | List all built-in theme IDs and names |

`--print-theme` is the recommended starting point for a custom theme:

```sh
mkdir -p ~/.config/z13gui
z13gui --print-theme > ~/.config/z13gui/theme.toml
```
