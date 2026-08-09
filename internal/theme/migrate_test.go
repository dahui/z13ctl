// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateCopiesOldConfigDir covers the 2.0 first run: only the z13gui
// directory exists, and every top-level file must arrive under voltaire with
// the originals untouched (copy, never move — a 1.x downgrade needs them).
func TestMigrateCopiesOldConfigDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	oldDir := filepath.Join(tmp, "z13gui")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"config.toml": "theme = \"rog-dark\"\n",
		"theme.css":   "@define-color z13-accent #ff0000;\n",
	} {
		if err := os.WriteFile(filepath.Join(oldDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	MigrateFromZ13gui()

	for _, name := range []string{"config.toml", "theme.css"} {
		if _, err := os.Stat(filepath.Join(tmp, "voltaire", name)); err != nil {
			t.Errorf("migrated %s missing: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(oldDir, name)); err != nil {
			t.Errorf("old %s was disturbed: %v", name, err)
		}
	}
	if cfg := LoadAppConfig(); cfg.Theme != "rog-dark" {
		t.Errorf("LoadAppConfig().Theme = %q after migration, want rog-dark", cfg.Theme)
	}
}

// TestMigrateNeverOverwritesExistingDir: once a voltaire directory exists —
// even empty — the old directory is history and must not clobber it.
func TestMigrateNeverOverwritesExistingDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	for dir, theme := range map[string]string{"z13gui": "old-theme", "voltaire": "new-theme"} {
		d := filepath.Join(tmp, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "config.toml"), []byte("theme = \""+theme+"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	MigrateFromZ13gui()

	if cfg := LoadAppConfig(); cfg.Theme != "new-theme" {
		t.Errorf("LoadAppConfig().Theme = %q, want the existing dir's new-theme", cfg.Theme)
	}
}

// TestMigrateNoOldDirIsANoOp: a fresh install has neither directory; the
// helper must not create anything or complain.
func TestMigrateNoOldDirIsANoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	MigrateFromZ13gui()

	if _, err := os.Stat(filepath.Join(tmp, "voltaire")); !os.IsNotExist(err) {
		t.Errorf("voltaire dir exists after no-op migration (err=%v)", err)
	}
}
