---
title: Themes
description: Pick a built-in theme and accent, write your own theme.toml, or replace the stylesheet entirely.
---

# Themes

voltaire-gui comes with 15 built-in themes, and you can define your own colors
in a small TOML file.

## Choose a built-in theme

Press the palette button at the bottom-left of the drawer. Pick a theme from
the list; for themes with accent variants (the four Catppuccin themes), a row
of colored dots below the theme name picks the accent. Your choice is saved
automatically.

| ID | Name | Type | Accents |
|----|------|------|---------|
| `catppuccin-frappe` | Catppuccin Frappe | dark | 14 |
| `catppuccin-latte` | Catppuccin Latte | light | 14 |
| `catppuccin-macchiato` | Catppuccin Macchiato | dark | 14 |
| `catppuccin-mocha` | Catppuccin Mocha | dark | 14 |
| `everforest-light` | Everforest Light | light | — |
| `github-light` | GitHub Light | light | — |
| `gruvbox-dark` | Gruvbox Dark | dark | — |
| `gruvbox-light` | Gruvbox Light | light | — |
| `nord` | Nord | dark | — |
| `one-light` | One Light | light | — |
| `rog-dark` | ROG Dark (default) | dark | — |
| `rog-neon` | ROG Neon | dark | — |
| `rose-pine-dawn` | Rose Pine Dawn | light | — |
| `solarized-light` | Solarized Light | light | — |
| `tokyo-night` | Tokyo Night | dark | — |

`voltaire-gui --list-themes` prints the same list. The Catppuccin accents are
the 14 official Catppuccin accent colors (`rosewater`, `flamingo`, `pink`,
`mauve`, `red`, `maroon`, `peach`, `yellow`, `green`, `teal`, `sky`,
`sapphire`, `blue`, `lavender`).

## Which theme wins

The GUI uses the first of these it finds:

1. `~/.config/voltaire/theme.toml`: your custom colors
2. `~/.config/voltaire/theme.css`: a full stylesheet
3. The built-in theme chosen in the picker (saved in `config.toml`)
4. ROG Dark

So while a `theme.toml` exists, the picker's choice has no effect. Rename or
delete the file to go back to the built-in themes.

## Create a custom theme

Start from the default colors:

```sh
mkdir -p ~/.config/voltaire
voltaire-gui --print-theme > ~/.config/voltaire/theme.toml
```

Edit the values, then restart the GUI (`systemctl --user restart
voltaire-gui`). The file has eight colors, each a `#rrggbb` hex value:

```toml
accent      = "#cc0000"  # active buttons, slider fill, checked states, active tab
background  = "#1a1a1a"  # panel background
surface     = "#2a2a2a"  # button and row backgrounds
surface_alt = "#333333"  # hover background
text        = "#e0e0e0"  # primary text
text_dim    = "#888888"  # section headings, secondary text
border      = "#444444"  # outlines and separators
error       = "#ff4444"  # error messages, high-TDP warning
```

A missing key or an invalid value falls back to the ROG Dark color, and
unknown keys and comments are ignored.

For ready-made starting points, the repository's
[examples/themes](https://github.com/dahui/voltaire/tree/main/examples/themes)
directory has a `.toml` file for every built-in theme.

### Add accent variants

A custom theme can offer accent choices as colored dots in the theme picker.
Add an `[accents]` section after the colors:

```toml
accent = "#5294e2"
# ... the other colors ...

[accents]
blue   = "#5294e2"
teal   = "#2eb398"
purple = "#9b59b6"
orange = "#e67e22"
```

Each line is `name = "#hex"`. The name is shown as the dot's tooltip and saved
in `config.toml`; the top-level `accent` is used until you pick one.

## Replace the stylesheet

For complete control, write a GTK4 stylesheet to
`~/.config/voltaire/theme.css`. It replaces the built-in styling entirely, so
it must define all eight colors:

```css
@define-color voltaire-accent      #cc0000;
@define-color voltaire-bg          #1a1a1a;
@define-color voltaire-surface     #2a2a2a;
@define-color voltaire-surface-alt #333333;
@define-color voltaire-text        #e0e0e0;
@define-color voltaire-text-dim    #888888;
@define-color voltaire-border      #444444;
@define-color voltaire-error       #ff4444;
```

Then add any GTK4 CSS rules you like. A color your stylesheet uses but does
not define makes GTK drop every rule that uses it; the GUI logs the missing
names at startup, so if part of the drawer loses its styling, check
`journalctl --user -u voltaire-gui`.

Remember that a `theme.toml` takes priority: remove it to use `theme.css`.
