// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"log/slog"
	"os"
	"path/filepath"
)

// MigrateFromZ13gui copies the pre-2.0 z13gui config directory to the voltaire
// one on first run: ~/.config/z13gui/* -> ~/.config/voltaire/*.
//
// Copy, never move — the old directory staying intact is what makes a
// downgrade to a 1.x z13gui safe for the whole 2.x line. The copy happens only
// while the voltaire directory does not exist at all, so it can never
// overwrite anything the user has saved since; once the new directory exists,
// the old one is history. Only regular files at the top level are copied
// (config.toml, theme.toml, theme.css — everything the 1.x GUI ever wrote).
//
// Call it before anything reads config, including --print-theme.
func MigrateFromZ13gui() {
	newDir := filepath.Join(XDGConfigHome(), "voltaire")
	if _, err := os.Stat(newDir); err == nil || !os.IsNotExist(err) {
		return
	}
	oldDir := filepath.Join(XDGConfigHome(), "z13gui")
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return // nothing to migrate
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		slog.Warn("cannot create config dir for migration", "path", newDir, "err", err)
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(oldDir, e.Name()))
		if readErr != nil {
			slog.Warn("cannot read old config file", "file", e.Name(), "err", readErr)
			continue
		}
		if writeErr := os.WriteFile(filepath.Join(newDir, e.Name()), data, 0o644); writeErr != nil {
			slog.Warn("cannot copy config file", "file", e.Name(), "err", writeErr)
			continue
		}
	}
	slog.Info("migrated config from z13gui", "from", oldDir, "to", newDir)
}
