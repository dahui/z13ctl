package api_test

// profilename_test.go — the name rules ship in this module so socket clients
// can pre-check with the same code the daemon refuses with; this suite is what
// keeps the standalone module honest about them. The daemon-side wrapper
// (internal/cli) carries its own cases against the same table.

import (
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

func TestValidateProfileName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		refused bool
	}{
		{"simple", "gaming", false},
		{"digits and separators", "profile-2_b", false},
		{"empty", "", true},
		{"uppercase", "Gaming", true},
		{"stock quiet", "quiet", true},
		{"stock balanced", "balanced", true},
		{"stock performance", "performance", true},
		{"reserved custom", "custom", true},
		{"space", "my profile", true},
		{"slash", "a/b", true},
		{"at the length limit", strings.Repeat("a", api.MaxProfileNameLen), false},
		{"over the length limit", strings.Repeat("a", api.MaxProfileNameLen+1), true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := api.ValidateProfileName(tt.in)
			if tt.refused && err == nil {
				t.Errorf("ValidateProfileName(%q) accepted, want refusal", tt.in)
			}
			if !tt.refused && err != nil {
				t.Errorf("ValidateProfileName(%q) = %v, want accepted", tt.in, err)
			}
		})
	}
}
