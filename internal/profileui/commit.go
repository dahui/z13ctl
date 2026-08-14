// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// commit.go — label rules for the full window's one-commit model. The window's
// Profiles page has a single commit button instead of the drawer's per-domain
// saves (Save TDP / Save Fans / Save UV): beside the profile operations those
// read as a staging step that does not exist — the daemon has no working slot,
// a send *is* the save into the profile. The widget mechanics live in the cgo
// island (internal/gui/customcommit.go); everything decidable lives here,
// where make test reaches it.

import "strings"

// CommitLabel is the commit button's label for an edit plan. A live target is
// applied to hardware the moment it is committed; a stored one is only saved,
// and calling both "Apply" would promise a hardware change a stored target
// never makes until it is activated.
func CommitLabel(live bool) string {
	if live {
		return "Apply Changes"
	}
	return "Save Changes"
}

// UnsavedSummary names the domains whose widgets differ from the last-synced
// state, for the indicator beside the commit button. Empty when nothing is
// dirty; callers disable the button and hide the label on "".
func UnsavedSummary(tdp, fans, uv bool) string {
	var parts []string
	if tdp {
		parts = append(parts, "TDP")
	}
	if fans {
		parts = append(parts, "fan curve")
	}
	if uv {
		parts = append(parts, "undervolt")
	}
	if len(parts) == 0 {
		return ""
	}
	return "Unsaved: " + strings.Join(parts, " · ")
}
