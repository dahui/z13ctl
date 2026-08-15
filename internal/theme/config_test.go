// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppConfig_Default(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	cfg := LoadAppConfig()
	if cfg.Theme != "rog-dark" {
		t.Errorf("Theme = %q, want rog-dark", cfg.Theme)
	}
	if cfg.Accent != "" {
		t.Errorf("Accent = %q, want empty", cfg.Accent)
	}
}

func TestLoadAppConfig_WithTheme(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "voltaire")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("theme = \"nord\"\naccent = \"blue\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadAppConfig()
	if cfg.Theme != "nord" {
		t.Errorf("Theme = %q, want nord", cfg.Theme)
	}
	if cfg.Accent != "blue" {
		t.Errorf("Accent = %q, want blue", cfg.Accent)
	}
}

func TestLoadAppConfig_Comments(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "voltaire")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "# config file\ntheme = \"gruvbox-dark\" # nice theme\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadAppConfig()
	if cfg.Theme != "gruvbox-dark" {
		t.Errorf("Theme = %q, want gruvbox-dark", cfg.Theme)
	}
}

func TestLoadAppConfig_EmptyThemeKeepsDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "voltaire")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("theme = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadAppConfig()
	if cfg.Theme != "rog-dark" {
		t.Errorf("Theme = %q, want rog-dark (default for empty value)", cfg.Theme)
	}
}

func TestSaveAppConfig_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	original := AppConfig{Theme: "catppuccin-mocha", Accent: "sapphire"}
	SaveAppConfig(original)

	loaded := LoadAppConfig()
	if loaded.Theme != original.Theme {
		t.Errorf("Theme = %q, want %q", loaded.Theme, original.Theme)
	}
	if loaded.Accent != original.Accent {
		t.Errorf("Accent = %q, want %q", loaded.Accent, original.Accent)
	}
}

func TestSaveAppConfig_NoAccent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	SaveAppConfig(AppConfig{Theme: "nord"})
	loaded := LoadAppConfig()
	if loaded.Theme != "nord" {
		t.Errorf("Theme = %q, want nord", loaded.Theme)
	}
	if loaded.Accent != "" {
		t.Errorf("Accent = %q, want empty", loaded.Accent)
	}
}

func TestSaveAppConfig_CreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	SaveAppConfig(AppConfig{Theme: "nord"})
	dir := filepath.Join(tmp, "voltaire")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("config path is not a directory")
	}
}

func TestXDGConfigHome_EnvSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got := XDGConfigHome()
	if got != "/custom/config" {
		t.Errorf("XDGConfigHome() = %q, want /custom/config", got)
	}
}

func TestXDGConfigHome_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := XDGConfigHome()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config")
	if got != want {
		t.Errorf("XDGConfigHome() = %q, want %q", got, want)
	}
}

// UpdateAppConfig exists so that changing one preference cannot discard
// another — SaveAppConfig writes the whole file, so a caller that builds a
// fresh AppConfig drops every field it does not happen to set. Both GUI
// writers did exactly that once (see the function's own comment), and the
// second field added to this struct is when it stops being theoretical.
func TestUpdateAppConfigPreservesEveryOtherField(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	SaveAppConfig(AppConfig{Theme: "nord", Accent: "sapphire", ButtonPress: "window"})

	UpdateAppConfig(func(cfg *AppConfig) { cfg.Accent = "lavender" })

	loaded := LoadAppConfig()
	if loaded.Accent != "lavender" {
		t.Errorf("Accent = %q, want lavender", loaded.Accent)
	}
	if loaded.Theme != "nord" {
		t.Errorf("Theme = %q, want nord — an unrelated update discarded it", loaded.Theme)
	}
	if loaded.ButtonPress != "window" {
		t.Errorf("ButtonPress = %q, want window — an unrelated update discarded it", loaded.ButtonPress)
	}
}

// The button preference travels through the same file as the theme, and it is
// written by a control rather than by hand, so a value that does not survive a
// round trip would reset itself on the next restart.
func TestButtonPressRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	SaveAppConfig(AppConfig{Theme: "nord", ButtonPress: "window"})
	if got := LoadAppConfig().ButtonPress; got != "window" {
		t.Errorf("ButtonPress = %q, want window", got)
	}

	// Absent means "the default", which is buttonpref's to decide, not this
	// package's — so it comes back empty rather than filled in.
	SaveAppConfig(AppConfig{Theme: "nord"})
	if got := LoadAppConfig().ButtonPress; got != "" {
		t.Errorf("ButtonPress = %q, want empty when unset", got)
	}
}
