package daemon

// profile_test.go — profile-map invariants that no other test covers.
//
// Everything here stays on state-only paths. internal/cli's sysfs path vars are
// unexported, so a daemon test that reached applyProfileLocked would rewrite the
// developer's real power limits, fan mode, and Curve Optimizer offset.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

// blockedFor reports whether fn was still running after d, used to check that a
// handler really does wait on a lock. Long enough that a handler which does not
// take the lock finishes first, short enough to keep the suite fast.
func blockedFor(d time.Duration, done <-chan struct{}) bool {
	select {
	case <-done:
		return false
	case <-time.After(d):
		return true
	}
}

// TestProfileMutatorsTakeHwMu is the regression guard for a resurrect-after-
// delete race. The fancurve/tdp/undervolt handlers resolve a target under d.mu,
// release it for the hardware write, then commit the profile back under d.mu.
// A delete landing inside that window was undone by the commit, putting the
// deleted profile back. hwMu is what makes the whole span exclusive, so the
// profile mutators must take it even though they write no hardware themselves.
func TestProfileMutatorsTakeHwMu(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tests := []struct {
		name string
		call func(d *Daemon)
	}{
		{"profile-create", func(d *Daemon) { d.handleProfileCreate(request{Set: "fresh"}) }},
		{"profile-save", func(d *Daemon) { d.handleProfileSave(request{Set: "copy"}) }},
		{"profile-delete", func(d *Daemon) { d.handleProfileDelete(request{Set: "spare"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{state: api.State{
				Profile: "gaming",
				CustomProfiles: map[string]api.CustomProfile{
					"gaming": {Name: "gaming", TDP: &api.TDPState{PL1SPL: 70}},
					"spare":  {Name: "spare"},
				},
			}}
			// Hold hwMu as an edit handler would while writing hardware.
			d.hwMu.Lock()
			done := make(chan struct{})
			go func() { defer close(done); tt.call(d) }()

			blocked := blockedFor(150*time.Millisecond, done)
			d.hwMu.Unlock()
			<-done

			if !blocked {
				t.Errorf("%s ran while hwMu was held; it can interleave with an edit handler's "+
					"resolve-write-commit span and silently undo the commit", tt.name)
			}
		})
	}
}

// TestApplyCustomHWClearsWhatTheProfileDoesNotSet states the invariant that
// makes switching between two custom profiles predictable. It asserts on the
// decision inputs rather than calling applyCustomHW, which writes hardware:
// internal/cli's path vars are unexported, so invoking it here would change the
// developer's real power limits, fan mode, and Curve Optimizer offset.
//
// The bug it guards: going from a profile with a 90 W limit and a -25 offset to
// one that sets neither left both in force while the daemon reported the second
// profile, so selecting A then B then A gave a different machine than A alone.
func TestApplyCustomHWClearsWhatTheProfileDoesNotSet(t *testing.T) {
	tests := []struct {
		name           string
		p              api.CustomProfile
		wantWriteCurve bool
		wantClearTDP   bool
		wantResetCO    bool
		wantReleaseFan bool
	}{
		{
			name:           "a profile that sets nothing clears everything",
			p:              api.CustomProfile{Name: "blank"},
			wantClearTDP:   true,
			wantResetCO:    true,
			wantReleaseFan: true,
		},
		{
			name:           "a curve-only profile clears the limits and the offset",
			p:              api.CustomProfile{Name: "fans", FanCurve: &api.FanCurveState{Mode: 1, Points: curve(120)}},
			wantWriteCurve: true,
			wantClearTDP:   true,
			wantResetCO:    true,
		},
		{
			name:           "a TDP-only profile releases the fans and clears the offset",
			p:              api.CustomProfile{Name: "watts", TDP: &api.TDPState{PL1SPL: 45}},
			wantResetCO:    true,
			wantReleaseFan: true,
		},
		{
			// The floor ApplyTDPSafely just wrote must stand: releasing the fans
			// here would drop it while the limit that requires it is in force.
			name:         "a high-TDP profile with no curve keeps the high-TDP floor",
			p:            api.CustomProfile{Name: "hot", TDP: &api.TDPState{PL1SPL: cli.TDPMaxSafe + 1}},
			wantResetCO:  true,
			wantClearTDP: false,
		},
		{
			name:           "a fully-specified profile clears nothing",
			p:              api.CustomProfile{Name: "all", FanCurve: &api.FanCurveState{Mode: 1, Points: curve(140)}, TDP: &api.TDPState{PL1SPL: 45}, Undervolt: &api.UndervoltState{CPUCO: -20}},
			wantWriteCurve: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.p
			hasCurve := p.FanCurve != nil && p.FanCurve.Mode == 1 && len(p.FanCurve.Points) == 8
			highTDP := p.TDP != nil && p.TDP.PL1SPL > cli.TDPMaxSafe

			if hasCurve != tt.wantWriteCurve {
				t.Errorf("writes the profile's curve = %v, want %v", hasCurve, tt.wantWriteCurve)
			}
			if clearTDP := p.TDP == nil; clearTDP != tt.wantClearTDP {
				t.Errorf("hands the limits back to the firmware profile = %v, want %v", clearTDP, tt.wantClearTDP)
			}
			if resetCO := p.Undervolt == nil; resetCO != tt.wantResetCO {
				t.Errorf("resets the Curve Optimizer = %v, want %v", resetCO, tt.wantResetCO)
			}
			if release := !hasCurve && !highTDP; release != tt.wantReleaseFan {
				t.Errorf("releases the fans to firmware auto = %v, want %v", release, tt.wantReleaseFan)
			}
		})
	}
}

// TestProfileNamesStayLowercaseInTheMap covers the strict-on-write,
// lenient-on-lookup split: creating "Gaming" is refused, but selecting or
// editing an existing profile folds case.
func TestProfileNamesStayLowercaseInTheMap(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d := &Daemon{state: api.State{Profile: "balanced"}}

	if resp := d.handleProfileCreate(request{Set: "Gaming"}); resp.OK {
		t.Error("profile-create accepted \"Gaming\"; the map key would not match what the user typed")
	}
	if resp := d.handleProfileCreate(request{Set: "gaming"}); !resp.OK {
		t.Fatalf("profile-create gaming: %s", resp.Error)
	}

	d.mu.Lock()
	target, err := d.resolveEditTargetLocked("GAMING")
	d.mu.Unlock()
	if err != nil {
		t.Fatalf("editing \"GAMING\" should fold to \"gaming\": %v", err)
	}
	if target.Name != "gaming" {
		t.Errorf("edit target = %q, want \"gaming\"", target.Name)
	}
}

// TestProfileCreateRejectsDuplicates keeps --create from silently wiping a
// populated profile.
func TestProfileCreateRejectsDuplicates(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d := &Daemon{state: api.State{
		CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming", TDP: &api.TDPState{PL1SPL: 70}},
		},
	}}
	if resp := d.handleProfileCreate(request{Set: "gaming"}); resp.OK {
		t.Fatal("profile-create overwrote an existing profile")
	}
	if d.state.CustomProfiles["gaming"].TDP == nil {
		t.Error("the existing profile's settings were cleared by the refused create")
	}
}

// TestProfileSaveZeroesUndervoltActive covers a bug generator: Active describes
// hardware, so a copy taken while an offset was applied must not claim to be
// applied itself.
func TestProfileSaveZeroesUndervoltActive(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d := &Daemon{state: api.State{
		Profile: "gaming",
		CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -25, Active: true}},
		},
	}}
	if resp := d.handleProfileSave(request{Set: "copy"}); !resp.OK {
		t.Fatalf("profile-save: %s", resp.Error)
	}
	cp := d.state.CustomProfiles["copy"]
	if cp.Undervolt == nil || cp.Undervolt.CPUCO != -25 {
		t.Fatalf("copied undervolt = %+v, want CO -25", cp.Undervolt)
	}
	if cp.Undervolt.Active {
		t.Error("the copy claims its undervolt is applied to hardware, but only the original is")
	}
	cp.Undervolt.CPUCO = -1
	if d.state.CustomProfiles["gaming"].Undervolt.CPUCO != -25 {
		t.Error("profile-save aliased the source profile's undervolt pointer")
	}
}

// TestProfileSaveRefusesWithNothingToCopy keeps an empty profile out of the map.
func TestProfileSaveRefusesWithNothingToCopy(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, s := range []api.State{
		{Profile: "balanced"}, // a firmware profile is active
		{Profile: "custom"},   // custom exists but holds nothing
	} {
		d := &Daemon{state: s}
		if resp := d.handleProfileSave(request{Set: "copy"}); resp.OK {
			t.Errorf("profile-save with Profile=%q was accepted, want a rejection", s.Profile)
		}
	}
}

// TestSetUndervoltActiveStampsEveryProfile covers the global-hardware argument:
// Curve Optimizer is one register, so at most one profile's offset can be live.
func TestSetUndervoltActiveStampsEveryProfile(t *testing.T) {
	s := api.State{CustomProfiles: map[string]api.CustomProfile{
		"a": {Name: "a", Undervolt: &api.UndervoltState{CPUCO: -10, Active: true}},
		"b": {Name: "b", Undervolt: &api.UndervoltState{CPUCO: -20, Active: true}},
		"c": {Name: "c"},
	}}
	setUndervoltActive(s, false)
	for name, p := range s.CustomProfiles {
		if p.Undervolt != nil && p.Undervolt.Active {
			t.Errorf("profile %q still claims its undervolt is applied", name)
		}
	}
}

// TestConcurrentProfileMutationIsRaceFree exercises the map under -race through
// the paths a GUI would drive.
func TestConcurrentProfileMutationIsRaceFree(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d := &Daemon{state: api.State{
		Profile:        "balanced",
		CustomProfiles: map[string]api.CustomProfile{"keep": {Name: "keep", TDP: &api.TDPState{PL1SPL: 40}}},
	}}

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "p" + string(rune('a'+i))
			for range 50 {
				d.handleProfileCreate(request{Set: name})
				d.handleProfileList()
				d.handleAutoswitchGet()
				d.handleProfileDelete(request{Set: name})
			}
		}(i)
	}
	wg.Wait()

	if _, ok := d.state.CustomProfiles["keep"]; !ok {
		t.Error("the untouched profile disappeared")
	}
}

func TestDeleteRefusalMessages(t *testing.T) {
	d := &Daemon{state: api.State{
		Profile: "gaming",
		CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming"}, "ac-one": {Name: "ac-one"}, "bat-one": {Name: "bat-one"},
		},
		Autoswitch: &api.AutoswitchState{Enabled: true, AC: "ac-one", Battery: "bat-one"},
	}}
	for target, want := range map[string]string{
		"gaming":  "active profile",
		"ac-one":  "autoswitch AC profile",
		"bat-one": "autoswitch battery profile",
		"missing": "no saved profile",
	} {
		if got := d.deleteRefusalLocked(target); !strings.Contains(got, want) {
			t.Errorf("deleteRefusalLocked(%q) = %q, want it to mention %q", target, got, want)
		}
	}
}

// TestAutoswitchValidationOrderIsStable keeps the reported side deterministic
// when both are wrong — a map range would name a different one each run.
func TestAutoswitchValidationOrderIsStable(t *testing.T) {
	for range 20 {
		d := &Daemon{state: api.State{}}
		resp := d.handleAutoswitch(request{Enabled: true, AC: "nope-ac", Battery: "nope-bat"})
		if resp.OK {
			t.Fatal("autoswitch accepted two unknown targets")
		}
		if !strings.Contains(resp.Error, "nope-ac") {
			t.Fatalf("error = %q, want the AC side reported first every time", resp.Error)
		}
	}
}

// TestAutoswitchDisableIsAlwaysAllowed covers the escape hatch: turning
// autoswitch off must work even when a target has gone stale, or it stays
// enabled with no way back short of editing the state file.
func TestAutoswitchDisableIsAlwaysAllowed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d := &Daemon{state: api.State{}}

	if resp := d.handleAutoswitch(request{Enabled: true, Battery: "ghost"}); resp.OK {
		t.Fatal("enabling with an unknown target was accepted")
	}
	resp := d.handleAutoswitch(request{Enabled: false, AC: "ghost", Battery: "ghost"})
	if !resp.OK {
		t.Fatalf("disabling with a stale target was refused: %s", resp.Error)
	}
	if d.state.Autoswitch == nil || d.state.Autoswitch.Enabled {
		t.Errorf("autoswitch = %+v, want it disabled", d.state.Autoswitch)
	}
}
