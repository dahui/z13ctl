// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package startup

import "testing"

// TestGUIEnvPrefersNewName: when both names are set the voltaire name wins —
// a user migrating their service file must not have the stale old value
// shadow the one they just wrote.
func TestGUIEnvPrefersNewName(t *testing.T) {
	t.Setenv("VOLTAIRE_GUI_SCALE", "1.5")
	t.Setenv("Z13GUI_SCALE", "2.0")

	if got := GUIEnv("SCALE"); got != "1.5" {
		t.Errorf("GUIEnv(SCALE) = %q, want the new name's 1.5", got)
	}
}

// TestGUIEnvFallsBackToLegacyName covers the 2.0 upgrade: a session still
// exporting only Z13GUI_* keeps working unchanged.
func TestGUIEnvFallsBackToLegacyName(t *testing.T) {
	t.Setenv("VOLTAIRE_GUI_NO_GAMEPAD", "")
	t.Setenv("Z13GUI_NO_GAMEPAD", "1")

	if got := GUIEnv("NO_GAMEPAD"); got != "1" {
		t.Errorf("GUIEnv(NO_GAMEPAD) = %q, want the legacy 1", got)
	}
}

// TestGUIEnvUnsetIsEmpty: neither name set reads as empty, the inactive state
// for both variables.
func TestGUIEnvUnsetIsEmpty(t *testing.T) {
	t.Setenv("VOLTAIRE_GUI_SCALE", "")
	t.Setenv("Z13GUI_SCALE", "")

	if got := GUIEnv("SCALE"); got != "" {
		t.Errorf("GUIEnv(SCALE) = %q, want empty", got)
	}
}
