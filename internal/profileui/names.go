// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui

// names.go — creating and copying profiles: pre-checks and the suggested name.

import (
	"fmt"

	"github.com/dahui/voltaire/api/v2"
)

// CreateNameProblem returns "" when name can be used for profile-create, else
// the reason it cannot. The name rules themselves are api.ValidateProfileName
// — the same code the daemon refuses with — plus the create-specific check
// that the name is free, so the OK button can be gated without a round trip.
// The daemon still has the final word; this only saves the user typing a whole
// name before hearing it.
func CreateNameProblem(s *api.State, name string) string {
	if err := api.ValidateProfileName(name); err != nil {
		return err.Error()
	}
	if s != nil {
		if _, exists := s.CustomProfiles[name]; exists {
			return fmt.Sprintf("a profile named %q already exists", name)
		}
	}
	return ""
}

// SaveAsNameProblem returns "" when name can be used for profile-save, else
// the reason. Unlike create, an existing name is allowed — the daemon
// overwrites it, which is the "update my bookmark" use — so only the name
// rules apply.
func SaveAsNameProblem(name string) string {
	if err := api.ValidateProfileName(name); err != nil {
		return err.Error()
	}
	return ""
}

// SaveAsBlock returns "" when Save As can work right now, else the reason the
// affordance must be insensitive. It mirrors the daemon's profile-save
// refusals: the copy source is the active custom profile, so there must be one
// and it must have settings.
func SaveAsBlock(s *api.State) string {
	if s == nil {
		return "the daemon is not connected"
	}
	p, ok := s.ActiveCustomProfile()
	if !ok {
		return "no custom profile is active — Save As copies the active one"
	}
	if p.Empty() {
		return "the active profile has no settings to copy"
	}
	return ""
}

// SuggestName returns a free, valid profile name to prefill the name entry
// with: "profile", then "profile-2", "profile-3", … A gamepad user cannot type
// into the entry at all, so the suggestion is not a convenience there — it is
// the only name they can use, and it must always be valid and free.
func SuggestName(s *api.State) string {
	taken := func(name string) bool {
		if s == nil {
			return false
		}
		_, ok := s.CustomProfiles[name]
		return ok
	}
	if !taken("profile") {
		return "profile"
	}
	for n := 2; ; n++ {
		name := fmt.Sprintf("profile-%d", n)
		if !taken(name) {
			return name
		}
	}
}
