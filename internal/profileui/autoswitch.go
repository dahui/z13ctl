// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// autoswitch.go — the autoswitch section's choices and the power-source label.

import "github.com/dahui/voltaire/api/v2"

// LeaveAlone is the wire value for "do not change the profile on this power
// source" — the empty target the daemon hands back to power-profiles-daemon.
const LeaveAlone = ""

// Autoswitch returns the autoswitch configuration as a value, nil-safe: a
// missing configuration reads as disabled with both sides unset, which is
// exactly what the daemon means by it.
func Autoswitch(s *api.State) api.AutoswitchState {
	if s == nil || s.Autoswitch == nil {
		return api.AutoswitchState{}
	}
	return *s.Autoswitch
}

// TargetRow is one row of an autoswitch target dropdown.
type TargetRow struct {
	Name  string // wire value; LeaveAlone for the no-change row
	Label string // display text, "(empty)"-suffixed for an unusable profile
	// Empty marks a custom profile with no settings. It is shown greyed out
	// rather than hidden: the daemon refuses to activate an empty profile, so
	// it would fail at every transition as a target — but silently omitting it
	// read as "my profiles aren't offered", a broken list rather than a rule
	// (Jeff, 2026-08-14). The row explains itself instead.
	Empty bool
}

// TargetRows returns the rows for one autoswitch side, in display order:
// "leave alone" first, then the stock profiles, then every custom profile —
// the empty ones present but marked, for the reason on TargetRow.Empty.
func TargetRows(s *api.State) []TargetRow {
	rows := []TargetRow{{Name: LeaveAlone, Label: TargetLabel(LeaveAlone)}}
	for _, r := range StockRows(s) {
		rows = append(rows, TargetRow{Name: r.Name, Label: TargetLabel(r.Name)})
	}
	for _, r := range CustomRows(s) {
		// "already the active profile" blocks the Activate button but says
		// nothing about whether the profile is a usable autoswitch target —
		// only an empty one is unusable, and that is the case Empty() names.
		row := TargetRow{Name: r.Name, Label: TargetLabel(r.Name)}
		if emptyProfile(s, r.Name) {
			row.Empty = true
			// The same word `profile --list` uses for the same fact.
			row.Label += " (empty)"
		}
		rows = append(rows, row)
	}
	return rows
}

// TargetOptions returns the selectable choices for one autoswitch side — the
// names of TargetRows minus the empty profiles, which are displayed but can
// never be chosen.
func TargetOptions(s *api.State) []string {
	rows := TargetRows(s)
	opts := make([]string, 0, len(rows))
	for _, r := range rows {
		if !r.Empty {
			opts = append(opts, r.Name)
		}
	}
	return opts
}

// emptyProfile reports whether the named custom profile has no settings, and
// so could never be applied.
func emptyProfile(s *api.State, name string) bool {
	if s == nil {
		return true
	}
	p, ok := s.CustomProfiles[name]
	return !ok || p.Empty()
}

// TargetLabel returns the display text for an autoswitch target.
func TargetLabel(name string) string {
	if name == LeaveAlone {
		return "(don't change)"
	}
	return Label(name)
}

// PowerLabel returns the power-source indicator text: "AC", "Battery", or ""
// when the source is unknown. Unknown means the daemon could not find a mains
// supply to read (a VM, a desktop, a pre-2.0 daemon that does not report
// source_known) — and claiming "Battery" there is exactly the mistake
// SourceKnown exists to prevent, so the indicator shows nothing instead.
func PowerLabel(s *api.State) string {
	if s == nil || !s.SourceKnown {
		return ""
	}
	if s.OnAC {
		return "AC"
	}
	return "Battery"
}
