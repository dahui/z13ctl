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

// TargetOptions returns the choices for one autoswitch side, in display order:
// "leave alone" first, then the stock profiles, then every custom profile that
// could actually be applied. Empty custom profiles are deliberately absent
// even though the daemon would accept them as targets — a target that fails at
// every transition with nothing on screen is a trap, not an option.
func TargetOptions(s *api.State) []string {
	opts := []string{LeaveAlone}
	for _, r := range StockRows(s) {
		opts = append(opts, r.Name)
	}
	for _, r := range CustomRows(s) {
		// "already the active profile" blocks the Activate button but says
		// nothing about whether the profile is a usable autoswitch target —
		// only an empty one is unusable, and that is the case Empty() names.
		if !emptyProfile(s, r.Name) {
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
