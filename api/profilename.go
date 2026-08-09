package api

// profilename.go — custom profile name rules.
//
// These live in the api module rather than inside the daemon because every
// client that offers a "create profile" affordance needs them for immediate
// feedback: the daemon enforces them on profile-create/profile-save either
// way, but a GUI that can only learn "q is reserved" from a round-trip refusal
// makes the user type the whole name first. One shared copy is also what keeps
// a client's pre-check and the daemon's refusal from drifting apart.

import (
	"fmt"
	"strings"
)

// MaxProfileNameLen bounds a custom profile name. It is a state file key and a
// command-line argument, not a display string; anything longer is a mistake.
const MaxProfileNameLen = 32

// ValidateProfileName checks a user-supplied custom profile name.
//
// The firmware profile names are reserved so that selecting one always reaches
// the firmware profile and can never be shadowed by a custom profile. That
// reservation is load-bearing beyond avoiding confusion: the daemon treats any
// name absent from its stock power table as custom and disables its
// stale-cache fallback, which is right for a custom profile and wrong for a
// stock one — so a custom profile called "balanced" would misreport the
// machine's power limits. "custom" is reserved separately: it is the profile
// created implicitly by the first custom setting.
//
// Validation is strict on write — "Gaming" is rejected rather than folded to
// "gaming", or the user looks for a profile under a name that is not there.
// Lookups (profile selection, edit targeting) fold case instead.
func ValidateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("profile name %q must be lowercase", name)
	}
	if IsStockProfileName(name) {
		return fmt.Errorf("%q is a firmware profile name and cannot be used for a custom profile", name)
	}
	if name == DefaultCustomProfile {
		return fmt.Errorf("%q is reserved for the profile created by the first custom setting", name)
	}
	if len(name) > MaxProfileNameLen {
		return fmt.Errorf("profile name %q is longer than %d characters", name, MaxProfileNameLen)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("profile name %q may only contain a-z, 0-9, '-' and '_'", name)
		}
	}
	return nil
}
