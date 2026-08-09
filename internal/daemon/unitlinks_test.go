// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// unitTree builds a per-user systemd directory and returns its root.
func unitTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "systemd", "user")
	for _, d := range []string{"graphical-session.target.wants", "sockets.target.wants"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// goneUnit is a symlink target that cannot exist, standing in for the unit file
// the upgrade removed. It must not be the real /usr/lib path: on a machine that
// still has a pre-2.0 package installed, that file is present and the link
// would not be dangling at all — which silently inverted this test's premise
// the first time it was written.
func goneUnit(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "removed-by-upgrade", name)
}

func link(t *testing.T, root, dir, name, target string) string {
	t.Helper()
	p := filepath.Join(root, dir, name)
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestStaleUnitLinksFindsDanglingOldUnits is the case the 2.0 upgrade creates:
// the user ran `systemctl --user enable z13ctl.service`, and the package
// upgrade then removed the unit file the symlink pointed at.
func TestStaleUnitLinksFindsDanglingOldUnits(t *testing.T) {
	root := unitTree(t)
	want := []string{
		link(t, root, "graphical-session.target.wants", "z13ctl.service", goneUnit(t, "z13ctl.service")),
		link(t, root, "graphical-session.target.wants", "z13gui.service", goneUnit(t, "z13gui.service")),
		link(t, root, "sockets.target.wants", "z13ctl.socket", goneUnit(t, "z13ctl.socket")),
	}

	got := staleUnitLinks(root)
	if len(got) != len(want) {
		t.Fatalf("staleUnitLinks found %d links, want %d: %v", len(got), len(want), got)
	}
	found := map[string]bool{}
	for _, g := range got {
		found[g] = true
	}
	for _, w := range want {
		if !found[w] {
			t.Errorf("missing %s from %v", w, got)
		}
	}
}

// TestStaleUnitLinksLeavesWorkingInstallAlone is the guard that matters: a
// symlink whose target still exists belongs to a 1.x install that is still
// usable, and disabling it would be a rename shim breaking a working setup.
func TestStaleUnitLinksLeavesWorkingInstallAlone(t *testing.T) {
	root := unitTree(t)
	unit := filepath.Join(t.TempDir(), "z13ctl.service")
	if err := os.WriteFile(unit, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link(t, root, "graphical-session.target.wants", "z13ctl.service", unit)

	if got := staleUnitLinks(root); len(got) != 0 {
		t.Errorf("staleUnitLinks = %v, want none — the target resolves", got)
	}
}

// TestStaleUnitLinksIgnoresRealFilesAndOtherUnits: only symlinks we recognise
// are candidates. A unit file the user wrote by hand at that path is theirs.
func TestStaleUnitLinksIgnoresRealFilesAndOtherUnits(t *testing.T) {
	root := unitTree(t)
	ownUnit := filepath.Join(root, "graphical-session.target.wants", "z13ctl.service")
	if err := os.WriteFile(ownUnit, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link(t, root, "sockets.target.wants", "someone-else.socket", goneUnit(t, "someone-else.socket"))
	link(t, root, "graphical-session.target.wants", "voltaire.service", goneUnit(t, "voltaire.service"))

	if got := staleUnitLinks(root); len(got) != 0 {
		t.Errorf("staleUnitLinks = %v, want none", got)
	}
	if _, err := os.Stat(ownUnit); err != nil {
		t.Errorf("real unit file was disturbed: %v", err)
	}
}

// TestStaleUnitLinksToleratesMissingTree: a user who never enabled anything has
// no such directory, and a daemon start must not care.
func TestStaleUnitLinksToleratesMissingTree(t *testing.T) {
	if got := staleUnitLinks(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Errorf("staleUnitLinks = %v, want nil", got)
	}
	if got := staleUnitLinks(""); got != nil {
		t.Errorf("staleUnitLinks(\"\") = %v, want nil", got)
	}
}

// TestCleanStaleUnitLinksRemovesOnlyTheDangling drives the real entry point
// through the userUnitDir seam and checks the filesystem afterwards.
func TestCleanStaleUnitLinksRemovesOnlyTheDangling(t *testing.T) {
	root := unitTree(t)
	orig := userUnitDir
	userUnitDir = func() string { return root }
	t.Cleanup(func() { userUnitDir = orig })

	dangling := link(t, root, "graphical-session.target.wants", "z13ctl.service", goneUnit(t, "z13ctl.service"))
	live := filepath.Join(t.TempDir(), "z13gui.service")
	if err := os.WriteFile(live, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := link(t, root, "graphical-session.target.wants", "z13gui.service", live)

	cleanStaleUnitLinks()

	if _, err := os.Lstat(dangling); !os.IsNotExist(err) {
		t.Errorf("dangling link survived (err=%v)", err)
	}
	if _, err := os.Lstat(kept); err != nil {
		t.Errorf("live link was removed: %v", err)
	}
}
