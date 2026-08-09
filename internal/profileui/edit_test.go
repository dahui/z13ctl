// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package profileui_test

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

func TestPlanEdit(t *testing.T) {
	gaming := api.CustomProfile{Name: "gaming", TDP: tdp(60)}
	cases := []struct {
		name     string
		state    *api.State
		target   string
		wantLive bool
		wantWire string
	}{
		// Editing what is running is live by definition.
		{"active named", stateWith("gaming", gaming), "gaming", true, ""},
		{"active custom", stateWith("custom", api.CustomProfile{Name: "custom", TDP: tdp(50)}), "custom", true, ""},
		// The drawer's historical Custom flow: bare sends create and activate
		// "custom" from a firmware profile — must stay live, or old daemons
		// (which ignore the profile field) change behaviour.
		{"custom while stock active", stateWith("balanced"), "custom", true, ""},
		{"custom with nil state", nil, "custom", true, ""},
		// Everything else is a stored edit through the ...For variants.
		{"named not active", stateWith("balanced", gaming), "gaming", false, "gaming"},
		{"named while other named active", stateWith("cool", gaming, api.CustomProfile{Name: "cool", TDP: tdp(30)}), "gaming", false, "gaming"},
		// The trap case: "custom" while a NAMED profile runs. A bare send
		// would edit the running "cool" profile, not "custom".
		{"custom while named active", stateWith("cool", api.CustomProfile{Name: "cool", TDP: tdp(30)}), "custom", false, "custom"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			plan := profileui.PlanEdit(tt.state, tt.target)
			if plan.Live != tt.wantLive {
				t.Errorf("Live = %v, want %v", plan.Live, tt.wantLive)
			}
			if got := plan.WireProfile(); got != tt.wantWire {
				t.Errorf("WireProfile() = %q, want %q", got, tt.wantWire)
			}
			if plan.Target != tt.target {
				t.Errorf("Target = %q, want %q", plan.Target, tt.target)
			}
		})
	}
}

func TestForEditorLive(t *testing.T) {
	s := stateWith("gaming", api.CustomProfile{Name: "gaming", TDP: tdp(80)})
	s.TDP = tdp(80) // the projection, as get-state fills it
	s.FanCurve = &api.FanCurveState{Mode: 1, Points: curve8(120)}
	s.Undervolt = &api.UndervoltState{CPUCO: -20, Active: true}

	es := profileui.ForEditor(s, profileui.PlanEdit(s, "gaming"))
	if es.TDP != s.TDP || es.FanCurve != s.FanCurve || es.Undervolt != s.Undervolt {
		t.Error("live edit must display the projected state")
	}
	if es.FloorPL1 != 80 {
		t.Errorf("FloorPL1 = %d, want the applied 80", es.FloorPL1)
	}
	if es.CO != -20 {
		t.Errorf("CO = %d, want -20", es.CO)
	}
	if !es.HasTDP {
		t.Error("HasTDP false while a custom profile is active")
	}
}

func TestForEditorLiveOnStockShowsNoCO(t *testing.T) {
	// On a firmware profile the projection still carries the saved offset for
	// recall, but CO is reset in hardware — the slider must show 0, as the
	// drawer always has.
	s := stateWith("balanced")
	s.TDP = tdp(45)
	s.Undervolt = &api.UndervoltState{CPUCO: -25, Active: false}

	es := profileui.ForEditor(s, profileui.PlanEdit(s, "custom"))
	if es.CO != 0 {
		t.Errorf("CO = %d on a stock profile, want 0", es.CO)
	}
	if es.HasTDP {
		t.Error("HasTDP true on a stock profile")
	}
}

func TestForEditorStored(t *testing.T) {
	p := api.CustomProfile{
		Name:      "gaming",
		TDP:       tdp(85),
		FanCurve:  &api.FanCurveState{Mode: 1, Points: curve8(140)},
		Undervolt: &api.UndervoltState{CPUCO: -15},
	}
	s := stateWith("balanced", p)
	s.TDP = tdp(45) // live hardware says 45W...

	es := profileui.ForEditor(s, profileui.PlanEdit(s, "gaming"))
	if es.TDP == nil || es.TDP.PL1SPL != 85 {
		t.Fatalf("TDP = %+v, want the profile's saved 85W", es.TDP)
	}
	// ...but the floor for a stored edit comes from the profile's own limit:
	// hardware says nothing about a profile that is not running.
	if es.FloorPL1 != 85 {
		t.Errorf("FloorPL1 = %d, want the profile's own 85", es.FloorPL1)
	}
	if es.CO != -15 {
		t.Errorf("CO = %d, want the profile's saved -15", es.CO)
	}
	if !es.HasTDP {
		t.Error("HasTDP false for a profile that saves a TDP")
	}
}

func TestForEditorStoredEmptySubsystems(t *testing.T) {
	s := stateWith("balanced", api.CustomProfile{Name: "bare", FanCurve: &api.FanCurveState{Mode: 1, Points: curve8(100)}})
	s.TDP = tdp(90) // a high live limit must NOT leak into the stored target's floor

	es := profileui.ForEditor(s, profileui.PlanEdit(s, "bare"))
	if es.TDP != nil || es.Undervolt != nil {
		t.Error("subsystems the profile does not control must be nil")
	}
	if es.FloorPL1 != 0 {
		t.Errorf("FloorPL1 = %d for a profile with no TDP, want 0 (no floor)", es.FloorPL1)
	}
	if es.CO != 0 {
		t.Errorf("CO = %d, want 0", es.CO)
	}
	if es.HasTDP {
		t.Error("HasTDP true for a profile with no TDP")
	}
}

func TestCurveToShow(t *testing.T) {
	pts := curve8(120)
	live := profileui.EditPlan{Target: "custom", Live: true}
	stored := profileui.EditPlan{Target: "gaming", Live: false}

	cases := []struct {
		name string
		plan profileui.EditPlan
		fc   *api.FanCurveState
		want bool
	}{
		{"live curve in force", live, &api.FanCurveState{Mode: 1, Points: pts}, true},
		// The registers keep the last written points after a release to
		// firmware auto; the mode is the evidence, and it says "not in force".
		{"live released curve", live, &api.FanCurveState{Mode: 2, Points: pts}, false},
		{"live nil", live, nil, false},
		// A stored curve has no hardware behind it — presence is the signal,
		// whatever the bookkeeping mode field holds.
		{"stored curve", stored, &api.FanCurveState{Mode: 0, Points: pts}, true},
		{"stored nil", stored, nil, false},
		{"stored empty points", stored, &api.FanCurveState{Mode: 1}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := profileui.CurveToShow(tt.plan, tt.fc)
			if ok != tt.want {
				t.Fatalf("ok = %v, want %v", ok, tt.want)
			}
			if ok && len(got) != len(pts) {
				t.Errorf("returned %d points, want %d", len(got), len(pts))
			}
		})
	}
}

// curve8 returns a valid 8-point curve with every PWM at the given value.
func curve8(pwm int) []api.FanCurvePoint {
	pts := make([]api.FanCurvePoint, 8)
	for i := range pts {
		pts[i] = api.FanCurvePoint{Temp: 40 + i*8, PWM: pwm}
	}
	return pts
}
