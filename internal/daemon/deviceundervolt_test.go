// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// deviceundervolt_test.go — the slider device's undervolt setting
// (deviceundervolt.go), driven through the real handlers over a recording
// Undervolter and the slider rig's limiter and pinned profiles. What is
// asserted is the SMU traffic as much as the state: which offsets were
// written, and that nothing is reset that was not applied.

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

// uvSliderDaemon is the OXP shape: slider power, the balanced pin, and a
// Curve Optimizer that answers its probe.
func uvSliderDaemon(t *testing.T, state api.State) (*Daemon, *uvRecorder, *pinnedProfiles) {
	t.Helper()
	d, _ := sliderDaemon(t, state)
	uv := &uvRecorder{available: true, probeOK: true}
	prof := &pinnedProfiles{}
	d.hw.Undervolt, d.hw.Profiles, d.hw.ProfilePin = uv, prof, "balanced"
	return d, uv, prof
}

// On the firmware profile a bare set is the device setting: written,
// remembered as applied, and no custom profile created. The same value again
// writes nothing.
func TestDeviceUndervoltSetIsADeviceSetting(t *testing.T) {
	d, uv, _ := uvSliderDaemon(t, api.State{Profile: "balanced"})
	if resp := d.handleUndervolt(request{Set: "-15"}); !resp.OK {
		t.Fatalf("handleUndervolt: %s", resp.Error)
	}
	if got := d.state.DeviceUndervolt; got == nil || got.CPUCO != -15 || !got.Active {
		t.Errorf("DeviceUndervolt = %+v, want -15 applied", got)
	}
	if len(uv.values) != 1 || uv.values[0] != -15 {
		t.Errorf("offsets written %v, want [-15]", uv.values)
	}
	if d.state.Profile != "balanced" || len(d.state.CustomProfiles) != 0 {
		t.Errorf("profile %q, profiles %v: a device setting must not create or activate custom", d.state.Profile, d.state.CustomProfiles)
	}
	if resp := d.handleUndervolt(request{Set: "-15"}); !resp.OK {
		t.Fatalf("handleUndervolt again: %s", resp.Error)
	}
	if len(uv.values) != 1 {
		t.Errorf("offsets written %v: the value already in force was written again", uv.values)
	}
}

// A reset clears what is applied and forgets the setting; with nothing
// applied it sends nothing to the SMU at all (the uvApplied rule).
func TestDeviceUndervoltReset(t *testing.T) {
	d, uv, _ := uvSliderDaemon(t, api.State{Profile: "balanced", DeviceUndervolt: &api.UndervoltState{CPUCO: -15, Active: true}})
	if resp := d.handleUndervoltReset(request{}); !resp.OK {
		t.Fatalf("handleUndervoltReset: %s", resp.Error)
	}
	if uv.resets != 1 || d.state.DeviceUndervolt != nil {
		t.Errorf("resets %d, DeviceUndervolt %+v; want one reset and the setting gone", uv.resets, d.state.DeviceUndervolt)
	}

	d, uv, _ = uvSliderDaemon(t, api.State{Profile: "balanced"})
	if resp := d.handleUndervoltReset(request{}); !resp.OK {
		t.Fatalf("handleUndervoltReset with nothing set: %s", resp.Error)
	}
	if uv.resets != 0 || uv.probes != 0 {
		t.Errorf("nothing applied: %d probes, %d resets, want none", uv.probes, uv.resets)
	}

	// Setting 0 is the same as a reset.
	d, uv, _ = uvSliderDaemon(t, api.State{Profile: "balanced", DeviceUndervolt: &api.UndervoltState{CPUCO: -15, Active: true}})
	if resp := d.handleUndervolt(request{Set: "0"}); !resp.OK {
		t.Fatalf("handleUndervolt 0: %s", resp.Error)
	}
	if uv.resets != 1 || uv.applies != 0 || d.state.DeviceUndervolt != nil {
		t.Errorf("set 0: %d resets, %d applies, setting %+v; want one reset and nothing stored", uv.resets, uv.applies, d.state.DeviceUndervolt)
	}
}

// With a custom profile running, a bare set leaves it for the pin, as the TDP
// slider does: the new setting replaces the profile's offset in one write (no
// reset first), and the profile keeps what it saved.
func TestDeviceUndervoltSetLeavesARunningProfile(t *testing.T) {
	d, uv, prof := uvSliderDaemon(t, api.State{
		Profile: "gaming",
		CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -25, Active: true}},
		},
	})
	if resp := d.handleUndervolt(request{Set: "-10"}); !resp.OK {
		t.Fatalf("handleUndervolt: %s", resp.Error)
	}
	if d.state.Profile != "balanced" || len(prof.sets) == 0 {
		t.Errorf("profile %q (writes %v), want the pinned balanced", d.state.Profile, prof.sets)
	}
	if len(uv.values) != 1 || uv.values[0] != -10 || uv.resets != 0 {
		t.Errorf("written %v with %d resets, want [-10] and no reset", uv.values, uv.resets)
	}
	g := d.state.CustomProfiles["gaming"].Undervolt
	if g == nil || g.CPUCO != -25 || g.Active {
		t.Errorf("gaming's offset = %+v, want -25 kept and no longer applied", g)
	}
	if got := d.state.DeviceUndervolt; got == nil || got.CPUCO != -10 || !got.Active {
		t.Errorf("DeviceUndervolt = %+v, want -10 applied", got)
	}
}

// The setting runs under a profile that has no offset of its own, a profile
// with one wins while it runs, and landing back on the firmware profile puts
// the setting back.
func TestDeviceUndervoltFollowsTheRunningProfile(t *testing.T) {
	d, uv, _ := uvSliderDaemon(t, api.State{
		Profile:         "balanced",
		DeviceUndervolt: &api.UndervoltState{CPUCO: -10},
		CustomProfiles: map[string]api.CustomProfile{
			"quiet-ish": {Name: "quiet-ish", TDP: &api.TDPState{PL1SPL: 20, PL2SPPT: 20, FPPT: 20}},
			"gaming":    {Name: "gaming", Undervolt: &api.UndervoltState{CPUCO: -25}},
		},
	})

	d.state.Profile = "quiet-ish"
	d.applyCustomHW(d.state.CustomProfiles["quiet-ish"])
	if len(uv.values) != 1 || uv.values[0] != -10 || !d.state.DeviceUndervolt.Active {
		t.Fatalf("under a profile without an offset: written %v, setting %+v; want -10 applied", uv.values, d.state.DeviceUndervolt)
	}

	d.state.Profile = "gaming"
	d.applyCustomHW(d.state.CustomProfiles["gaming"])
	if uv.values[len(uv.values)-1] != -25 || d.state.DeviceUndervolt.Active || !d.state.CustomProfiles["gaming"].Undervolt.Active {
		t.Fatalf("under gaming: written %v, setting %+v; want gaming's -25 applied instead", uv.values, d.state.DeviceUndervolt)
	}

	if err := d.applyStockHW("balanced"); err != nil {
		t.Fatal(err)
	}
	if uv.values[len(uv.values)-1] != -10 || !d.state.DeviceUndervolt.Active || d.state.CustomProfiles["gaming"].Undervolt.Active {
		t.Errorf("back on balanced: written %v, setting %+v; want -10 applied again", uv.values, d.state.DeviceUndervolt)
	}
	if uv.resets != 0 {
		t.Errorf("%d resets: each change should be one write of the offset that applies", uv.resets)
	}
}

// After a boot or a suspend the offset is gone whatever the state file says,
// so the restore writes it again.
func TestDeviceUndervoltIsWrittenAgainAfterItIsLost(t *testing.T) {
	d, uv, _ := uvSliderDaemon(t, api.State{Profile: "balanced", DeviceUndervolt: &api.UndervoltState{CPUCO: -10, Active: true}})
	d.deviceUndervoltLost()
	if wrote, err := d.applyDeviceUndervolt("resume"); err != nil || !wrote {
		t.Fatalf("applyDeviceUndervolt = %v, %v", wrote, err)
	}
	if len(uv.values) != 1 || uv.values[0] != -10 {
		t.Errorf("written %v, want [-10]", uv.values)
	}
}

// tdp-reset forgets the TDP setting and keeps the undervolt setting in force
// (nothing written: it already is); tuning-reset discards both.
func TestResetsAndTheDeviceUndervolt(t *testing.T) {
	tdp := api.TDPState{PL1SPL: 40, PL2SPPT: 40, FPPT: 40}
	d, uv, _ := uvSliderDaemon(t, api.State{Profile: "balanced", DeviceTDP: &tdp, DeviceUndervolt: &api.UndervoltState{CPUCO: -10, Active: true}})
	if resp := d.resetToStock(request{}, "tdp-reset", false); !resp.OK {
		t.Fatalf("tdp-reset: %s", resp.Error)
	}
	if got := d.state.DeviceUndervolt; got == nil || got.CPUCO != -10 || !got.Active || d.state.DeviceTDP != nil {
		t.Errorf("after tdp-reset: undervolt %+v, TDP %+v; want -10 still applied and the TDP forgotten", got, d.state.DeviceTDP)
	}
	if uv.resets != 0 || uv.applies != 0 {
		t.Errorf("tdp-reset sent %d resets, %d applies; want none", uv.resets, uv.applies)
	}

	if resp := d.resetToStock(request{}, "tuning-reset", true); !resp.OK {
		t.Fatalf("tuning-reset: %s", resp.Error)
	}
	if d.state.DeviceUndervolt != nil || uv.resets != 1 {
		t.Errorf("after tuning-reset: undervolt %+v, %d resets; want it cleared with one reset", d.state.DeviceUndervolt, uv.resets)
	}
}

// Save As with no custom profile running copies the undervolt setting with
// the TDP setting, and the copy does not claim to be applied.
func TestSliderSaveAsKeepsTheUndervoltSetting(t *testing.T) {
	d, _, _ := uvSliderDaemon(t, api.State{Profile: "balanced", DeviceUndervolt: &api.UndervoltState{CPUCO: -10, Active: true}})
	if resp := d.handleProfileSave(request{Set: "chilled"}); !resp.OK {
		t.Fatalf("profile-save: %s", resp.Error)
	}
	got := d.state.CustomProfiles["chilled"].Undervolt
	if got == nil || got.CPUCO != -10 || got.Active {
		t.Errorf("the copy's offset = %+v, want -10, not applied", got)
	}
}

// A lost resume signal re-applies the setting on a firmware profile, on the
// same evidence as a custom profile's offset (the expired stand-down) and on
// nothing else.
func TestReconcileTickReappliesTheDeviceUndervolt(t *testing.T) {
	co := -10
	base := reconcileObs{CurveMode: -1, PL1: -1, RawPL1: -1, WantCO: &co}
	if _, act := reconcileTick(reconcileState{}, base, oxpEnv()); act.Undervolt != nil {
		t.Errorf("an ordinary tick re-applied the offset: %v", *act.Undervolt)
	}
	stale := base
	stale.Suspending = true
	st := reconcileState{suspendedTicks: reconcileSuspendMaxTicks}
	if _, act := reconcileTick(st, stale, oxpEnv()); act.Undervolt == nil || *act.Undervolt != -10 {
		t.Errorf("expired stand-down: act.Undervolt = %v, want -10", act.Undervolt)
	}
}
