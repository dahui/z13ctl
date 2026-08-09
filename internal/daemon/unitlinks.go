// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// Cleanup of the pre-2.0 systemd user units' enable symlinks.

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// oldUnitNames are the pre-2.0 user units. Their files are removed by the
// package upgrade; only a user's own enable symlinks can be left behind.
var oldUnitNames = map[string]bool{
	"z13ctl.service": true,
	"z13ctl.socket":  true,
	"z13gui.service": true,
}

// userUnitDir is the per-user systemd unit directory, as a var so tests can
// redirect it.
var userUnitDir = func() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user")
}

// staleUnitLinks returns the pre-2.0 enable symlinks under root that can no
// longer resolve — the residue of a `systemctl --user enable z13ctl.service`
// once the upgrade has removed the unit file it pointed at.
//
// Package upgrades can only reach the system-wide enable in /etc/systemd/user;
// a postinstall running as root has no business walking every user's home to
// find the rest, so nothing else is positioned to clean these up. They are
// harmless — the daemon is started by its own units and socket activation —
// but systemd logs "Unit z13ctl.service not found" at every login for as long
// as they exist, which reads like a broken install.
//
// Three conditions, all required, keep this from touching anything live:
// the name is one of ours, the entry is a symlink (a real unit file a user
// wrote themselves is never removed), and its target does not resolve. A
// working 1.x install alongside voltaire therefore survives untouched — a
// dangling link is one that can never start anything again.
func staleUnitLinks(root string) []string {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var stale []string
	for _, e := range entries {
		// Enable symlinks live in <target>.wants/ and <target>.requires/.
		if !e.IsDir() || (!strings.HasSuffix(e.Name(), ".wants") && !strings.HasSuffix(e.Name(), ".requires")) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		links, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, l := range links {
			if !oldUnitNames[l.Name()] || l.Type()&os.ModeSymlink == 0 {
				continue
			}
			path := filepath.Join(dir, l.Name())
			if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
				stale = append(stale, path)
			}
		}
	}
	return stale
}

// cleanStaleUnitLinks removes what staleUnitLinks finds, best-effort.
//
// Deliberately no `systemctl --user daemon-reload`: the links are already dead
// to systemd, so a reload changes nothing a user can observe, and reloading
// from inside a socket-activated daemon during its own startup is a worse
// trade than waiting for the next login.
func cleanStaleUnitLinks() {
	for _, path := range staleUnitLinks(userUnitDir()) {
		if err := os.Remove(path); err != nil {
			slog.Warn("cannot remove stale pre-2.0 unit symlink", "path", path, "err", err)
			continue
		}
		slog.Info("removed stale pre-2.0 unit symlink", "path", path)
	}
}
