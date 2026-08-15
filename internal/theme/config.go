// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// AppConfig holds app-level preferences persisted to config.toml.
//
// This is the config the *UI writes*; gui.toml beside it is the one you
// hand-edit (section order, the quickbar's screen edge) and has no writer at
// all. That is the split to keep: a preference the user sets from a control
// belongs here, or setting it would mean rewriting a file full of somebody's
// comments.
//
// Values are kept as plain strings and validated by whoever consumes them —
// Theme against the built-in table, ButtonPress through buttonpref.Parse, the
// refresh trio through display.ParseEnabled/ParsePref — so this package stays a
// carrier and does not have to know what any of them mean.
type AppConfig struct {
	Theme  string // built-in theme ID; empty = use default
	Accent string // accent ID within the theme; "" = use theme default

	// ButtonPress is which surface a single press of the hardware button
	// opens; "" means the default. See internal/buttonpref.
	ButtonPress string

	// RefreshAutoswitch turns the refresh-rate switch on; "" is off, which is
	// what makes it opt-in. RefreshAC and RefreshBattery are the rate to select
	// when the machine moves onto mains and onto battery, in whole hertz.
	//
	// Three keys rather than two because the on/off state has to be its own
	// value: deriving it from "are both rates set" would mean switching off had
	// to erase them, and there would be nothing to restore on switching back on.
	// Stored as a rate rather than as the compositor's mode id, because a mode
	// id does not survive the panel's mode list changing and means nothing on a
	// second screen; internal/display has the argument and does the resolving.
	RefreshAutoswitch string
	RefreshAC         string
	RefreshBattery    string
}

// LoadAppConfig reads ~/.config/voltaire/config.toml.
// Returns a default config (theme "rog-dark") if the file doesn't exist or can't be parsed.
func LoadAppConfig() AppConfig {
	path := filepath.Join(XDGConfigHome(), "voltaire", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{Theme: "rog-dark"}
	}
	cfg := AppConfig{Theme: "rog-dark"}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, " #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "theme":
			if v != "" {
				cfg.Theme = v
			}
		case "accent":
			cfg.Accent = v
		case "button_press":
			cfg.ButtonPress = v
		case "refresh_autoswitch":
			cfg.RefreshAutoswitch = v
		case "refresh_ac":
			cfg.RefreshAC = v
		case "refresh_battery":
			cfg.RefreshBattery = v
		}
	}
	return cfg
}

// SaveAppConfig writes the app config to ~/.config/voltaire/config.toml.
func SaveAppConfig(cfg AppConfig) {
	dir := filepath.Join(XDGConfigHome(), "voltaire")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("failed to create config dir", "path", dir, "err", err)
		return
	}
	content := "# voltaire-gui app configuration\ntheme = \"" + cfg.Theme + "\"\n"
	if cfg.Accent != "" {
		content += "accent = \"" + cfg.Accent + "\"\n"
	}
	if cfg.ButtonPress != "" {
		content += "button_press = \"" + cfg.ButtonPress + "\"\n"
	}
	if cfg.RefreshAutoswitch != "" {
		content += "refresh_autoswitch = \"" + cfg.RefreshAutoswitch + "\"\n"
	}
	if cfg.RefreshAC != "" {
		content += "refresh_ac = \"" + cfg.RefreshAC + "\"\n"
	}
	if cfg.RefreshBattery != "" {
		content += "refresh_battery = \"" + cfg.RefreshBattery + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		slog.Warn("failed to write config", "err", err)
	}
}

// UpdateAppConfig reads the config, applies fn to it, and writes it back.
//
// It exists so that changing one preference cannot discard another. Both GUI
// writers used to build a fresh AppConfig from the values they happened to have
// in hand: applyCustomAccent wrote AppConfig{Accent: …} and silently reset the
// user's theme to the default (invisible while a theme.toml existed, since that
// wins on load, and a surprise the moment they removed it), and applyTheme
// wrote AppConfig{Theme, Accent} — complete only while those were the only two
// fields, and so a trap primed for the next one. SaveAppConfig writes the whole
// file, so every writer has to carry every field or drop it; this is the one
// place that carries them.
func UpdateAppConfig(fn func(*AppConfig)) {
	cfg := LoadAppConfig()
	fn(&cfg)
	SaveAppConfig(cfg)
}

// XDGConfigHome returns $XDG_CONFIG_HOME or falls back to ~/.config.
func XDGConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to get home directory", "err", err)
		return "/tmp/.config"
	}
	return filepath.Join(home, ".config")
}
