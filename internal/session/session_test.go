// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestIsGamingDesktop(t *testing.T) {
	for desktop, want := range map[string]bool{
		"gamescope": true, "Gamescope": true, "gamescope-session": true,
		"KDE": false, "GNOME": false, "": false, "Hyprland": false,
	} {
		if got := IsGamingDesktop(desktop); got != want {
			t.Errorf("IsGamingDesktop(%q) = %v, want %v", desktop, got, want)
		}
	}
}

func TestDisplaySession(t *testing.T) {
	if p, ok := displaySession([]any{"2", dbus.ObjectPath("/org/freedesktop/login1/session/_32")}); !ok || p == "" {
		t.Errorf("a graphical session was not found: %q %v", p, ok)
	}
	if _, ok := displaySession([]any{"", dbus.ObjectPath("/")}); ok {
		t.Error(`"/" (no graphical session) was taken as one`)
	}
	if _, ok := displaySession("nonsense"); ok {
		t.Error("a malformed property was accepted")
	}
}
