// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package startup

// GUI environment variable lookup with the pre-rename fallback.

import "os"

// GUIEnv returns the value of the VOLTAIRE_GUI_<suffix> environment variable,
// falling back to the pre-rename Z13GUI_<suffix> name when the new one is
// empty or unset. The old names are honoured through the whole 2.x line —
// the same compatibility contract as the z13ctl socket path and state file —
// and removed at 3.0.
//
// A non-empty new name always wins; there is no way to distinguish "set to
// empty" from "unset" through os.Getenv, and both variables use non-empty as
// their active state (SCALE as an override value, NO_GAMEPAD as a flag).
func GUIEnv(suffix string) string {
	if v := os.Getenv("VOLTAIRE_GUI_" + suffix); v != "" {
		return v
	}
	return os.Getenv("Z13GUI_" + suffix)
}
