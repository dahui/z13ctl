// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

// Colors holds the 8 named color values that drive the entire GUI theme.
// Each field is a CSS hex color string like "#cc0000".
type Colors struct {
	Accent     string // @z13-accent      — active buttons, slider fill, checked states
	Background string // @z13-bg          — drawer background
	Surface    string // @z13-surface     — button/row background
	SurfaceAlt string // @z13-surface-alt — hover/alternate surface
	Text       string // @z13-text        — primary text
	TextDim    string // @z13-text-dim    — section labels, secondary text
	Border     string // @z13-border      — window border, separators
	Error      string // @z13-error       — error bar text/border, high-TDP warning
}

// DefaultColors is the ROG Dark default color set, used as the fallback when
// no theme is selected or a user theme.toml omits a color key. Parsing starts
// from this value, so a theme.toml written before a color was introduced keeps
// working — it simply inherits the default for the key it does not mention.
var DefaultColors = Colors{
	Accent:     "#cc0000",
	Background: "#1a1a1a",
	Surface:    "#2a2a2a",
	SurfaceAlt: "#333333",
	Text:       "#e0e0e0",
	TextDim:    "#888888",
	Border:     "#444444",
	Error:      "#ff4444",
}

// Accent is an alternate accent color for a theme. Catppuccin themes, for
// example, offer 14 official accent colors (Rosewater, Flamingo, etc.).
type Accent struct {
	ID   string // e.g., "blue", "sapphire"
	Name string // e.g., "Blue", "Sapphire"
	Hex  string // e.g., "#89b4fa"
}

// Builtin pairs a theme ID and display name with its color definition
// and optional accent color variants.
type Builtin struct {
	ID      string // config key, e.g. "catppuccin-mocha"
	Name    string // display name shown in the theme picker
	Colors  Colors
	Accents []Accent // optional accent variants; first = default (matches Colors.Accent)
}

// catppuccinMochaAccents lists all 14 official Catppuccin Mocha accent colors.
var catppuccinMochaAccents = []Accent{
	{ID: "rosewater", Name: "Rosewater", Hex: "#f5e0dc"},
	{ID: "flamingo", Name: "Flamingo", Hex: "#f2cdcd"},
	{ID: "pink", Name: "Pink", Hex: "#f5c2e7"},
	{ID: "mauve", Name: "Mauve", Hex: "#cba6f7"},
	{ID: "red", Name: "Red", Hex: "#f38ba8"},
	{ID: "maroon", Name: "Maroon", Hex: "#eba0ac"},
	{ID: "peach", Name: "Peach", Hex: "#fab387"},
	{ID: "yellow", Name: "Yellow", Hex: "#f9e2af"},
	{ID: "green", Name: "Green", Hex: "#a6e3a1"},
	{ID: "teal", Name: "Teal", Hex: "#94e2d5"},
	{ID: "sky", Name: "Sky", Hex: "#89dceb"},
	{ID: "sapphire", Name: "Sapphire", Hex: "#74c7ec"},
	{ID: "blue", Name: "Blue", Hex: "#89b4fa"},
	{ID: "lavender", Name: "Lavender", Hex: "#b4befe"},
}

// catppuccinLatteAccents lists all 14 official Catppuccin Latte accent colors.
var catppuccinLatteAccents = []Accent{
	{ID: "rosewater", Name: "Rosewater", Hex: "#dc8a78"},
	{ID: "flamingo", Name: "Flamingo", Hex: "#dd7878"},
	{ID: "pink", Name: "Pink", Hex: "#ea76cb"},
	{ID: "mauve", Name: "Mauve", Hex: "#8839ef"},
	{ID: "red", Name: "Red", Hex: "#d20f39"},
	{ID: "maroon", Name: "Maroon", Hex: "#e64553"},
	{ID: "peach", Name: "Peach", Hex: "#fe640b"},
	{ID: "yellow", Name: "Yellow", Hex: "#df8e1d"},
	{ID: "green", Name: "Green", Hex: "#40a02b"},
	{ID: "teal", Name: "Teal", Hex: "#179299"},
	{ID: "sky", Name: "Sky", Hex: "#04a5e5"},
	{ID: "sapphire", Name: "Sapphire", Hex: "#209fb5"},
	{ID: "blue", Name: "Blue", Hex: "#1e66f5"},
	{ID: "lavender", Name: "Lavender", Hex: "#7287fd"},
}

// catppuccinFrappeAccents lists all 14 official Catppuccin Frappe accent colors.
var catppuccinFrappeAccents = []Accent{
	{ID: "rosewater", Name: "Rosewater", Hex: "#f2d5cf"},
	{ID: "flamingo", Name: "Flamingo", Hex: "#eebebe"},
	{ID: "pink", Name: "Pink", Hex: "#f4b8e4"},
	{ID: "mauve", Name: "Mauve", Hex: "#ca9ee6"},
	{ID: "red", Name: "Red", Hex: "#e78284"},
	{ID: "maroon", Name: "Maroon", Hex: "#ea999c"},
	{ID: "peach", Name: "Peach", Hex: "#ef9f76"},
	{ID: "yellow", Name: "Yellow", Hex: "#e5c890"},
	{ID: "green", Name: "Green", Hex: "#a6d189"},
	{ID: "teal", Name: "Teal", Hex: "#81c8be"},
	{ID: "sky", Name: "Sky", Hex: "#99d1db"},
	{ID: "sapphire", Name: "Sapphire", Hex: "#85c1dc"},
	{ID: "blue", Name: "Blue", Hex: "#8caaee"},
	{ID: "lavender", Name: "Lavender", Hex: "#babbf1"},
}

// catppuccinMacchiatoAccents lists all 14 official Catppuccin Macchiato accent colors.
var catppuccinMacchiatoAccents = []Accent{
	{ID: "rosewater", Name: "Rosewater", Hex: "#f4dbd6"},
	{ID: "flamingo", Name: "Flamingo", Hex: "#f0c6c6"},
	{ID: "pink", Name: "Pink", Hex: "#f5bde6"},
	{ID: "mauve", Name: "Mauve", Hex: "#c6a0f6"},
	{ID: "red", Name: "Red", Hex: "#ed8796"},
	{ID: "maroon", Name: "Maroon", Hex: "#ee99a0"},
	{ID: "peach", Name: "Peach", Hex: "#f5a97f"},
	{ID: "yellow", Name: "Yellow", Hex: "#eed49f"},
	{ID: "green", Name: "Green", Hex: "#a6da95"},
	{ID: "teal", Name: "Teal", Hex: "#8bd5ca"},
	{ID: "sky", Name: "Sky", Hex: "#91d7e3"},
	{ID: "sapphire", Name: "Sapphire", Hex: "#7dc4e4"},
	{ID: "blue", Name: "Blue", Hex: "#8aadf4"},
	{ID: "lavender", Name: "Lavender", Hex: "#b7bdf8"},
}

// Builtins is the ordered list of built-in themes, sorted alphabetically
// by display name. This order is used directly by the theme picker popover.
var Builtins = []Builtin{
	{
		ID: "catppuccin-frappe", Name: "Catppuccin Frappe",
		Colors:  Colors{Accent: "#ca9ee6", Background: "#303446", Surface: "#414559", SurfaceAlt: "#51576d", Text: "#c6d0f5", TextDim: "#a5adce", Border: "#626880", Error: "#e78284"},
		Accents: catppuccinFrappeAccents,
	},
	{
		ID: "catppuccin-latte", Name: "Catppuccin Latte",
		Colors:  Colors{Accent: "#8839ef", Background: "#eff1f5", Surface: "#e6e9ef", SurfaceAlt: "#dce0e8", Text: "#4c4f69", TextDim: "#7c7f93", Border: "#bcc0cc", Error: "#d20f39"},
		Accents: catppuccinLatteAccents,
	},
	{
		ID: "catppuccin-macchiato", Name: "Catppuccin Macchiato",
		Colors:  Colors{Accent: "#c6a0f6", Background: "#24273a", Surface: "#363a4f", SurfaceAlt: "#494d64", Text: "#cad3f5", TextDim: "#a5adcb", Border: "#5b6078", Error: "#ed8796"},
		Accents: catppuccinMacchiatoAccents,
	},
	{
		ID: "catppuccin-mocha", Name: "Catppuccin Mocha",
		Colors:  Colors{Accent: "#cba6f7", Background: "#1e1e2e", Surface: "#313244", SurfaceAlt: "#45475a", Text: "#cdd6f4", TextDim: "#a6adc8", Border: "#585b70", Error: "#f38ba8"},
		Accents: catppuccinMochaAccents,
	},
	{
		ID: "everforest-light", Name: "Everforest Light",
		Colors: Colors{Accent: "#8da101", Background: "#fdf6e3", Surface: "#f4f0d9", SurfaceAlt: "#efebd4", Text: "#5c6a72", TextDim: "#859289", Border: "#bdc3af", Error: "#f85552"},
	},
	{
		ID: "github-light", Name: "GitHub Light",
		Colors: Colors{Accent: "#0969da", Background: "#ffffff", Surface: "#f6f8fa", SurfaceAlt: "#f3f4f6", Text: "#1f2328", TextDim: "#59636e", Border: "#d1d9e0", Error: "#cf222e"},
	},
	{
		ID: "gruvbox-dark", Name: "Gruvbox Dark",
		Colors: Colors{Accent: "#fe8019", Background: "#282828", Surface: "#3c3836", SurfaceAlt: "#504945", Text: "#ebdbb2", TextDim: "#a89984", Border: "#665c54", Error: "#fb4934"},
	},
	{
		ID: "gruvbox-light", Name: "Gruvbox Light",
		Colors: Colors{Accent: "#d65d0e", Background: "#fbf1c7", Surface: "#ebdbb2", SurfaceAlt: "#d5c4a1", Text: "#3c3836", TextDim: "#7c6f64", Border: "#bdae93", Error: "#9d0006"},
	},
	{
		ID: "nord", Name: "Nord",
		Colors: Colors{Accent: "#88c0d0", Background: "#2e3440", Surface: "#3b4252", SurfaceAlt: "#434c5e", Text: "#eceff4", TextDim: "#d8dee9", Border: "#4c566a", Error: "#bf616a"},
	},
	{
		ID: "one-light", Name: "One Light",
		Colors: Colors{Accent: "#4078f2", Background: "#fafafa", Surface: "#f0f0f0", SurfaceAlt: "#e5e5e6", Text: "#383a42", TextDim: "#696c77", Border: "#d0d0d0", Error: "#e45649"},
	},
	{
		ID: "rog-dark", Name: "ROG Dark",
		Colors: Colors{Accent: "#cc0000", Background: "#1a1a1a", Surface: "#2a2a2a", SurfaceAlt: "#333333", Text: "#e0e0e0", TextDim: "#888888", Border: "#444444", Error: "#ff4444"},
	},
	{
		ID: "rog-neon", Name: "ROG Neon",
		Colors: Colors{Accent: "#00d4ff", Background: "#0d0d14", Surface: "#1a1a2e", SurfaceAlt: "#16213e", Text: "#e0e0f0", TextDim: "#8888aa", Border: "#2a2a4a", Error: "#ff3366"},
	},
	{
		ID: "rose-pine-dawn", Name: "Rose Pine Dawn",
		Colors: Colors{Accent: "#d7827e", Background: "#faf4ed", Surface: "#f2e9e1", SurfaceAlt: "#ede3e0", Text: "#575279", TextDim: "#9893a5", Border: "#dfdad9", Error: "#b4637a"},
	},
	{
		ID: "solarized-light", Name: "Solarized Light",
		Colors: Colors{Accent: "#268bd2", Background: "#fdf6e3", Surface: "#eee8d5", SurfaceAlt: "#e8e2ce", Text: "#657b83", TextDim: "#839496", Border: "#d3c9b0", Error: "#dc322f"},
	},
	{
		ID: "tokyo-night", Name: "Tokyo Night",
		Colors: Colors{Accent: "#7aa2f7", Background: "#1a1b26", Surface: "#24283b", SurfaceAlt: "#2f3549", Text: "#c0caf5", TextDim: "#565f89", Border: "#292e42", Error: "#f7768e"},
	},
}

// BuiltinByID returns the colors for the built-in theme with the given ID.
// Returns false if the ID is not found.
func BuiltinByID(id string) (Colors, bool) {
	for _, t := range Builtins {
		if t.ID == id {
			return t.Colors, true
		}
	}
	return Colors{}, false
}

// BuiltinAccentHex returns the hex color for a named accent within a theme.
// Returns false if the theme or accent ID is not found.
func BuiltinAccentHex(themeID, accentID string) (string, bool) {
	for _, t := range Builtins {
		if t.ID != themeID {
			continue
		}
		for _, a := range t.Accents {
			if a.ID == accentID {
				return a.Hex, true
			}
		}
		return "", false
	}
	return "", false
}
