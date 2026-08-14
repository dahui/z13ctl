---
title: Theming
description: Built-in themes, custom theme.toml files, accent variants, and full CSS overrides for voltaire-gui.
---

voltaire-gui supports custom color themes through a simple TOML configuration
file. You can also select from 15 built-in themes using the in-app theme
picker.

## Theme priority

voltaire-gui resolves its theme using the following priority chain. The first
match wins:

1. `~/.config/voltaire/theme.toml` — custom color definitions (hex values)
2. `~/.config/voltaire/theme.css` — full CSS override (power users)
3. `~/.config/voltaire/config.toml` `theme = "id"` — built-in theme selection
4. Compiled-in default (ROG Dark)

If you create a `theme.toml`, it always takes priority over the built-in
theme picker selection. Delete or rename it to return to built-in themes.

## Built-in themes

| ID | Name | Type | Accents |
|----|------|------|---------|
| `catppuccin-frappe` | Catppuccin Frappe | dark | 14 variants |
| `catppuccin-latte` | Catppuccin Latte | light | 14 variants |
| `catppuccin-macchiato` | Catppuccin Macchiato | dark | 14 variants |
| `catppuccin-mocha` | Catppuccin Mocha | dark | 14 variants |
| `everforest-light` | Everforest Light | light | — |
| `github-light` | GitHub Light | light | — |
| `gruvbox-dark` | Gruvbox Dark | dark | — |
| `gruvbox-light` | Gruvbox Light | light | — |
| `nord` | Nord | dark | — |
| `one-light` | One Light | light | — |
| `rog-dark` | ROG Dark | dark | — |
| `rog-neon` | ROG Neon | dark | — |
| `rose-pine-dawn` | Rose Pine Dawn | light | — |
| `solarized-light` | Solarized Light | light | — |
| `tokyo-night` | Tokyo Night | dark | — |

List all built-in themes from the command line:

```sh
voltaire-gui --list-themes
```

## Choosing a built-in theme

Click the palette button at the bottom-left of the drawer. For themes with
accent variants (all four Catppuccin themes), a row of colored dots appears
below the theme name in the picker.

Your selection is saved automatically to `~/.config/voltaire/config.toml`.

## Creating a custom theme

A custom theme is a TOML file with 8 color keys. Each value is a CSS hex color
string (`#rrggbb`).

**Quick start:**

```sh
mkdir -p ~/.config/voltaire
voltaire-gui --print-theme > ~/.config/voltaire/theme.toml
```

This writes the default ROG Dark colors. Open the file and change the values.

**File format:**

```toml
# Accent color — active buttons, slider fill, checked states
accent = "#cc0000"

# Drawer background
background = "#1a1a1a"

# Surface color — button and row backgrounds
surface = "#2a2a2a"

# Alternate surface — hover state background
surface_alt = "#333333"

# Primary text color
text = "#e0e0e0"

# Dim text color — section labels, secondary descriptions
text_dim = "#888888"

# Border color — window outline and separators
border = "#444444"

# Error color — error bar text and border, high-TDP warning text
error = "#ff4444"
```

Comments, inline comments, unknown keys, and missing keys are all handled
gracefully. Invalid hex values are skipped; missing keys fall back to ROG Dark
defaults.

The `examples/themes/` directory in the repository contains a `.toml` file
for every built-in theme — useful starting points for custom themes.

Changes to `theme.toml` take effect the next time voltaire-gui starts.

### Accent variants

Custom themes can define accent color variants that appear as colored dots in
the theme picker. Add an `[accents]` section after the color keys:

```toml
accent = "#5294e2"
background = "#1b2838"
surface = "#253449"
surface_alt = "#2e3f55"
text = "#d3dae3"
text_dim = "#7c8fa0"
border = "#3b5068"

[accents]
blue = "#5294e2"
teal = "#2eb398"
purple = "#9b59b6"
orange = "#e67e22"
```

Each line is `id = "#hex"`. The ID is used as the tooltip and saved to
`config.toml`. The top-level `accent` key sets the default when no variant is
selected.

## Color reference

| Key | CSS variable | Controls |
|-----|-------------|----------|
| `accent` | `@voltaire-accent` | Active buttons, slider fill, checked radio buttons, active tab |
| `background` | `@voltaire-bg` | Drawer panel background |
| `surface` | `@voltaire-surface` | Button backgrounds, input row backgrounds |
| `surface_alt` | `@voltaire-surface-alt` | Hover state for buttons and rows |
| `text` | `@voltaire-text` | Primary text, labels, button text |
| `text_dim` | `@voltaire-text-dim` | Section headings (MODE, SPEED, etc.), secondary labels |
| `border` | `@voltaire-border` | Drawer border, separators, button outlines |
| `error` | `@voltaire-error` | Error bar text and border, high-TDP warning text |

Every variable is also defined under its pre-rename `@z13-*` name
(`@z13-accent` and so on). A stylesheet may reference either through the whole
2.x line; the `@voltaire-*` names are what the bundled rules use, and the
`@z13-*` aliases are removed at 3.0.

If you wrote a `theme.css` against the old names it keeps working unchanged —
it is loaded verbatim and supplies its own definitions, so it never sees ours.
The names matter only if you started from the bundled sheet and mix the two.

## Catppuccin accent colors

All four Catppuccin themes support the 14 official accent colors:

| ID | Mocha | Macchiato | Frappe | Latte |
|----|-------|-----------|--------|-------|
| `rosewater` | `#f5e0dc` | `#f4dbd6` | `#f2d5cf` | `#dc8a78` |
| `flamingo` | `#f2cdcd` | `#f0c6c6` | `#eebebe` | `#dd7878` |
| `pink` | `#f5c2e7` | `#f5bde6` | `#f4b8e4` | `#ea76cb` |
| `mauve` | `#cba6f7` | `#c6a0f6` | `#ca9ee6` | `#8839ef` |
| `red` | `#f38ba8` | `#ed8796` | `#e78284` | `#d20f39` |
| `maroon` | `#eba0ac` | `#ee99a0` | `#ea999c` | `#e64553` |
| `peach` | `#fab387` | `#f5a97f` | `#ef9f76` | `#fe640b` |
| `yellow` | `#f9e2af` | `#eed49f` | `#e5c890` | `#df8e1d` |
| `green` | `#a6e3a1` | `#a6da95` | `#a6d189` | `#40a02b` |
| `teal` | `#94e2d5` | `#8bd5ca` | `#81c8be` | `#179299` |
| `sky` | `#89dceb` | `#91d7e3` | `#99d1db` | `#04a5e5` |
| `sapphire` | `#74c7ec` | `#7dc4e4` | `#85c1dc` | `#209fb5` |
| `blue` | `#89b4fa` | `#8aadf4` | `#8caaee` | `#1e66f5` |
| `lavender` | `#b4befe` | `#b7bdf8` | `#babbf1` | `#7287fd` |

## Full CSS override

For complete control, provide a full GTK4 CSS stylesheet at
`~/.config/voltaire/theme.css`. This replaces the built-in theme CSS
entirely.

The stylesheet should define all 8 `@define-color` variables:

```css
@define-color z13-accent      #cc0000;
@define-color z13-bg          #1a1a1a;
@define-color z13-surface     #2a2a2a;
@define-color z13-surface-alt #333333;
@define-color z13-text        #e0e0e0;
@define-color z13-text-dim    #888888;
@define-color z13-border      #444444;
@define-color z13-error       #ff4444;
```

You can then add any GTK4 CSS rules. `theme.toml` takes priority over
`theme.css` — delete `theme.toml` to use a CSS override.

A `theme.css` is loaded **verbatim**: any color it references without
defining is simply undefined, and GTK silently drops every rule that uses it.
voltaire-gui checks for this at startup and logs the missing token names —
if part of the drawer loses its styling, read the log.

## Example

A complete custom dark-blue theme built from scratch:

```toml
# ~/.config/voltaire/theme.toml
accent      = "#5294e2"
background  = "#1b2838"
surface     = "#253449"
surface_alt = "#2e3f55"
text        = "#d3dae3"
text_dim    = "#7c8fa0"
border      = "#3b5068"
```

- `accent` — a medium blue for active buttons and checked states
- `background` — very dark navy for the drawer panel
- `surface` — slightly lighter than background so buttons stand out
- `surface_alt` — slightly lighter again for hover states
- `text` — light gray-blue for readability against the dark background
- `text_dim` — muted blue-gray for section labels
- `border` — sits between surface and text brightness for subtle outlines

To use this theme: save it to `~/.config/voltaire/theme.toml` and restart
voltaire-gui.
