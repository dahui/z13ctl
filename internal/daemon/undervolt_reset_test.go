// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// undervolt_reset_test.go — handleUndervoltReset driven end to end against a
// recording Undervolter.
//
// The handler reaches no hardware but the Undervolter, so a Device holding
// only the fake makes every branch hermetic, the live ones included — unlike
// the TDP and fan-curve resets, which write the real fan controller on a live
// target. What is asserted is the SMU traffic: how many probes and resets each
// case sends. The source guards in undervolt_gate_test.go check that the gate
// is spelled uvApplied; this checks that it holds.

import (
	"errors"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
)

// uvRecorder is an Undervolter that counts every call that could reach the
// SMU mailbox. ProbeAvailable counts as one: on the real driver its first run
// is a CO-0 write.
type uvRecorder struct {
	available bool  // what Available (and Present) answer
	probeOK   bool  // what ProbeAvailable answers
	resetErr  error // what Reset returns
	probes    int
	resets    int
	applies   int
}

func (u *uvRecorder) Present() bool   { return u.available }
func (u *uvRecorder) Available() bool { return u.available }
func (u *uvRecorder) ProbeAvailable() bool {
	u.probes++
	return u.probeOK
}
func (u *uvRecorder) Range() (lo, hi int) { return -40, 0 }
func (u *uvRecorder) Apply(int) error {
	u.applies++
	return nil
}
func (u *uvRecorder) Reset() error {
	u.resets++
	return u.resetErr
}

func TestHandleUndervoltReset(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	uv := func(co int, active bool) *api.UndervoltState {
		return &api.UndervoltState{CPUCO: co, Active: active}
	}

	cases := []struct {
		name     string
		rec      uvRecorder
		profile  string                       // state.Profile, the running one
		profiles map[string]api.CustomProfile // saved custom profiles
		req      string                       // the request's profile field
		wantOK   bool
		wantErr  string // substring of the refusal, when !wantOK
		probes   int
		resets   int
		// check inspects the profiles afterwards.
		check func(t *testing.T, got map[string]api.CustomProfile)
	}{
		{
			name:    "module absent: refused before anything is written",
			rec:     uvRecorder{available: false},
			profile: "gaming",
			profiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: uv(-20, true)},
			},
			wantErr: "not available",
			check: func(t *testing.T, got map[string]api.CustomProfile) {
				if got["gaming"].Undervolt == nil {
					t.Error("a refused reset cleared the stored offset")
				}
			},
		},
		{
			name:    "live and applied: one probe, one reset, stored offset cleared",
			rec:     uvRecorder{available: true, probeOK: true},
			profile: "gaming",
			profiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: uv(-20, true)},
			},
			wantOK: true, probes: 1, resets: 1,
			check: func(t *testing.T, got map[string]api.CustomProfile) {
				if got["gaming"].Undervolt != nil {
					t.Errorf("stored offset not cleared: %+v", got["gaming"].Undervolt)
				}
			},
		},
		{
			// The 2026-08-14 lockup: a reset for an offset that was never
			// applied. Nothing may reach the mailbox — not the reset, and not
			// the probe, whose first run is the same write.
			name:    "live, nothing applied: no SMU traffic at all, still succeeds",
			rec:     uvRecorder{available: true, probeOK: true},
			profile: "gaming",
			profiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: uv(-20, false)},
			},
			wantOK: true,
			check: func(t *testing.T, got map[string]api.CustomProfile) {
				if got["gaming"].Undervolt != nil {
					t.Error("the stored offset — the half the user can observe — was not cleared")
				}
			},
		},
		{
			// A wrong-fork machine: state says applied, the probe says the path
			// does not work. Nothing can have been applied through it.
			name:    "live, applied, probe fails: no reset",
			rec:     uvRecorder{available: true, probeOK: false},
			profile: "gaming",
			profiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: uv(-20, true)},
			},
			wantOK: true, probes: 1,
		},
		{
			name:    "live, reset fails: error, stored offset kept",
			rec:     uvRecorder{available: true, probeOK: true, resetErr: errors.New("smu said no")},
			profile: "gaming",
			profiles: map[string]api.CustomProfile{
				"gaming": {Name: "gaming", Undervolt: uv(-20, true)},
			},
			wantErr: "smu said no", probes: 1, resets: 1,
			check: func(t *testing.T, got map[string]api.CustomProfile) {
				if u := got["gaming"].Undervolt; u == nil || !u.Active {
					t.Errorf("a failed reset changed the stored offset: %+v", u)
				}
			},
		},
		{
			// --profile on a profile that is not running stores only, even
			// while another profile's offset is live in hardware: that offset
			// is not the one being reset.
			name:    "stored target while another profile's offset is applied: no write",
			rec:     uvRecorder{available: true, probeOK: true},
			profile: "work",
			profiles: map[string]api.CustomProfile{
				"work":   {Name: "work", Undervolt: uv(-10, true)},
				"gaming": {Name: "gaming", Undervolt: uv(-20, false)},
			},
			req:    "gaming",
			wantOK: true,
			check: func(t *testing.T, got map[string]api.CustomProfile) {
				if got["gaming"].Undervolt != nil {
					t.Error("--profile gaming did not clear gaming's stored offset")
				}
				if u := got["work"].Undervolt; u == nil || !u.Active || u.CPUCO != -10 {
					t.Errorf("the running profile's offset was touched: %+v", u)
				}
			},
		},
		{
			// A bare reset on a firmware profile resolves to "custom" without
			// activating it; the saved offset must survive for recall.
			name:    "bare reset on a firmware profile: saved custom offset preserved",
			rec:     uvRecorder{available: true, probeOK: true},
			profile: "balanced",
			profiles: map[string]api.CustomProfile{
				"custom": {Name: "custom", Undervolt: uv(-15, false)},
			},
			wantOK: true,
			check: func(t *testing.T, got map[string]api.CustomProfile) {
				if u := got["custom"].Undervolt; u == nil || u.CPUCO != -15 {
					t.Errorf("the saved custom offset was discarded: %+v", u)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			d := &Daemon{
				hw:    &device.Device{Undervolt: &rec},
				state: api.State{Profile: tc.profile, CustomProfiles: tc.profiles},
			}
			resp := d.handleUndervoltReset(request{Profile: tc.req})

			if tc.wantErr != "" {
				if resp.OK || !strings.Contains(resp.Error, tc.wantErr) {
					t.Fatalf("resp = %+v, want a refusal containing %q", resp, tc.wantErr)
				}
			} else if resp.OK != tc.wantOK {
				t.Fatalf("resp = %+v, want ok=%v", resp, tc.wantOK)
			}
			if rec.probes != tc.probes || rec.resets != tc.resets || rec.applies != 0 {
				t.Errorf("SMU traffic: probes=%d resets=%d applies=%d, want probes=%d resets=%d applies=0",
					rec.probes, rec.resets, rec.applies, tc.probes, tc.resets)
			}
			if d.state.Profile != tc.profile {
				t.Errorf("state.Profile = %q, want %q: a reset must not switch profiles",
					d.state.Profile, tc.profile)
			}
			if tc.check != nil {
				tc.check(t, d.state.CustomProfiles)
			}
		})
	}
}
