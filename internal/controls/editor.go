// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package controls

// editor.go — the customization editor's model: which rows it shows, how a
// row moves, and what list the result is saved as.
//
// The editor is one list of every control this machine supports, each shown or
// hidden, in drawer order. The saved form is only the shown IDs, so a hidden
// control's position is not stored; EditorRows puts it back where the default
// order says it belongs — CPU boost after the charge limit, not after RGB — so
// switching one on lands it beside the controls it groups with rather than
// under a second copy of a heading.

import (
	"slices"

	"github.com/dahui/voltaire/api/v2"
)

// EditorRow is one line of the editor.
type EditorRow struct {
	Control Control
	Shown   bool
}

// EditorRows returns the editor's rows for a saved list (nil: the defaults),
// covering every control this machine supports. Shown rows keep the list's
// order; each hidden one is placed after the nearest control that precedes it
// in the default order, or first when none does.
//
// IDs in the list that are unknown or unsupported here produce no row; they
// are kept by SavedList instead, so editing on one machine does not drop
// another machine's choices.
func EditorRows(list *[]string, info *api.DeviceInfo, session ...Capability) []EditorRow {
	shown, _ := Resolve(Config{Quickbar: QuickbarConfig{Controls: list}}, info, session...)
	rows := make([]EditorRow, 0, len(defaultOrder))
	for _, c := range shown {
		rows = append(rows, EditorRow{Control: c, Shown: true})
	}
	all := All()
	for i, c := range all {
		if !Supports(info, c, session...) || indexOf(rows, c.ID) >= 0 {
			continue
		}
		at := 0
		for j := i - 1; j >= 0; j-- {
			if k := indexOf(rows, all[j].ID); k >= 0 {
				at = k + 1
				break
			}
		}
		rows = slices.Insert(rows, at, EditorRow{Control: c})
	}
	return rows
}

// MoveRow moves row i by delta (-1 up, +1 down). It reports false, and returns
// rows unchanged, when the move would leave the list; the caller can use that
// to grey out the button that asked.
func MoveRow(rows []EditorRow, i, delta int) ([]EditorRow, bool) {
	j := i + delta
	if i < 0 || i >= len(rows) || j < 0 || j >= len(rows) {
		return rows, false
	}
	out := slices.Clone(rows)
	out[i], out[j] = out[j], out[i]
	return out, true
}

// SavedList is what the editor stores for rows.
//
// It is nil — "no choice made" — when the result is exactly the default drawer
// for this machine and prev held nothing the editor could not show. Storing the
// defaults explicitly would freeze them: a later build that adds a default
// control would never show it to a user who had once opened the editor and
// changed nothing. Otherwise it is the shown IDs in order, then every ID from
// prev that has no row here (a control this machine lacks, or one a newer
// build knows), so they survive the round trip.
func SavedList(rows []EditorRow, prev *[]string, info *api.DeviceInfo, session ...Capability) *[]string {
	var shown []string
	for _, r := range rows {
		if r.Shown {
			shown = append(shown, r.Control.ID)
		}
	}
	var kept []string
	if prev != nil {
		for _, id := range *prev {
			if indexOf(rows, id) < 0 && !slices.Contains(kept, id) {
				kept = append(kept, id)
			}
		}
	}
	defaults, _ := Resolve(Config{}, info, session...)
	if len(kept) == 0 && slices.Equal(shown, IDsOf(defaults)) {
		return nil
	}
	out := make([]string, 0, len(shown)+len(kept)) // never nil: an empty drawer is a choice
	out = append(out, shown...)
	out = append(out, kept...)
	return &out
}

func indexOf(rows []EditorRow, id string) int {
	for i, r := range rows {
		if r.Control.ID == id {
			return i
		}
	}
	return -1
}
