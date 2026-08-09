// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

func TestCreateNameProblem(t *testing.T) {
	s := stateWith("balanced", api.CustomProfile{Name: "gaming", TDP: tdp(60)})

	if got := profileui.CreateNameProblem(s, "cool"); got != "" {
		t.Errorf("free valid name refused: %q", got)
	}
	if got := profileui.CreateNameProblem(s, "gaming"); got == "" {
		t.Error("existing name accepted for create")
	}
	// The name rules are api.ValidateProfileName's; one representative each of
	// the reserved and syntactic refusals pins the delegation.
	if got := profileui.CreateNameProblem(s, "balanced"); got == "" {
		t.Error("firmware profile name accepted")
	}
	if got := profileui.CreateNameProblem(s, "Gaming"); got == "" {
		t.Error("uppercase accepted — the daemon would refuse it")
	}
	if got := profileui.CreateNameProblem(nil, "cool"); got != "" {
		t.Errorf("nil state refused a valid name: %q", got)
	}
}

func TestSaveAsNameProblem(t *testing.T) {
	// Unlike create, overwriting an existing name is the daemon's documented
	// save-as semantic, so only the name rules apply.
	if got := profileui.SaveAsNameProblem("gaming"); got != "" {
		t.Errorf("valid name refused: %q", got)
	}
	if got := profileui.SaveAsNameProblem("custom"); got == "" {
		t.Error("reserved name accepted")
	}
}

func TestSaveAsBlock(t *testing.T) {
	if got := profileui.SaveAsBlock(nil); got == "" {
		t.Error("nil state must block Save As")
	}
	if got := profileui.SaveAsBlock(stateWith("balanced")); got == "" {
		t.Error("stock profile active must block Save As — there is nothing to copy")
	}
	// "custom" active but never populated: ActiveCustomProfile answers with an
	// empty profile, and the daemon refuses to copy it.
	if got := profileui.SaveAsBlock(stateWith("custom")); got == "" {
		t.Error("empty active profile must block Save As")
	}
	s := stateWith("gaming", api.CustomProfile{Name: "gaming", TDP: tdp(60)})
	if got := profileui.SaveAsBlock(s); got != "" {
		t.Errorf("active profile with settings blocked: %q", got)
	}
}

func TestSuggestName(t *testing.T) {
	if got := profileui.SuggestName(nil); got != "profile" {
		t.Errorf("SuggestName(nil) = %q, want \"profile\"", got)
	}
	s := stateWith("balanced",
		api.CustomProfile{Name: "profile", TDP: tdp(30)},
		api.CustomProfile{Name: "profile-2", TDP: tdp(30)},
	)
	if got := profileui.SuggestName(s); got != "profile-3" {
		t.Errorf("SuggestName = %q, want \"profile-3\"", got)
	}
	// The suggestion is the only name a gamepad user can use: it must always
	// pass both the shared name rules and the free-name check.
	if problem := profileui.CreateNameProblem(s, profileui.SuggestName(s)); problem != "" {
		t.Errorf("suggested name unusable: %q", problem)
	}
}
