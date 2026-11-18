package daemon

// slider_test.go — the one-slider TDP model (power.slider) and the profile pin
// (profiles.pin): the handler over a recording limiter, and the reconcile
// tick's two firmware-profile duties.

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"
)

// powerRecorder is a PowerLimiter that remembers every Apply.
type powerRecorder struct {
	env     driver.PowerEnvelope
	applied []api.TDPState
}

func (p *powerRecorder) Read() (api.TDPState, error) {
	if n := len(p.applied); n > 0 {
		return p.applied[n-1], nil
	}
	return p.env.StockProfilePPT["balanced"], nil
}
func (p *powerRecorder) Apply(t api.TDPState) error {
	p.applied = append(p.applied, t)
	return nil
}
func (p *powerRecorder) Envelope() driver.PowerEnvelope { return p.env }

// oxpEnv is the OXP's AC envelope, as the amd-smu limiter reports it.
func oxpEnv() driver.PowerEnvelope {
	return driver.PowerEnvelope{TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85, NoSPPTMirrors: true,
		PL2: driver.PowerRange{Min: 10, Max: 85}, PL3: driver.PowerRange{Min: 10, Max: 100},
		StockProfilePPT: map[string]api.TDPState{"balanced": {PL1SPL: 80, PL2SPPT: 85, FPPT: 100}}}
}

func sliderDaemon(t *testing.T, state api.State) (*Daemon, *powerRecorder) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	rec := &powerRecorder{env: oxpEnv()}
	d := &Daemon{
		hw:    &device.Device{Power: &safety.Engine{Power: rec}, PowerSlider: true},
		state: state,
	}
	return d, rec
}

// On a firmware profile a bare set is the device setting: applied, remembered,
// and no custom profile created or activated.
func TestSliderSetIsADeviceSetting(t *testing.T) {
	d, rec := sliderDaemon(t, api.State{Profile: "balanced"})
	if resp := d.handleTDP(request{Set: "40"}); !resp.OK {
		t.Fatalf("handleTDP: %s", resp.Error)
	}
	if len(rec.applied) != 1 || rec.applied[0].PL1SPL != 40 || rec.applied[0].PL2SPPT != 40 || rec.applied[0].FPPT != 40 {
		t.Errorf("applied %+v, want 40/40/40", rec.applied)
	}
	if d.state.DeviceTDP == nil || d.state.DeviceTDP.PL1SPL != 40 {
		t.Errorf("DeviceTDP = %+v, want 40 remembered", d.state.DeviceTDP)
	}
	if d.state.Profile != "balanced" || len(d.state.CustomProfiles) != 0 {
		t.Errorf("profile %q, custom profiles %v: a slider set must not create or activate custom",
			d.state.Profile, d.state.CustomProfiles)
	}
}

// pinnedProfiles is the OXP's profile controller: balanced, pinned.
type pinnedProfiles struct{ sets []string }

func (p *pinnedProfiles) Names() []string          { return []string{"balanced"} }
func (p *pinnedProfiles) Get() (string, error)     { return "balanced", nil }
func (p *pinnedProfiles) Set(name string) error    { p.sets = append(p.sets, name); return nil }
func (p *pinnedProfiles) Default() string          { return "balanced" }
func (p *pinnedProfiles) Label(name string) string { return name }

// With a custom profile running, a bare set leaves it (Jeff, 2026-11-17: the
// slider never edits a profile): the value becomes the TDP setting, the device
// lands on its pinned profile, and the profile keeps what it saved, so loading
// it again recalls it.
func TestSliderSetLeavesARunningCustomProfile(t *testing.T) {
	keep := api.TDPState{PL1SPL: 30, PL2SPPT: 30, FPPT: 30}
	saved := api.TDPState{PL1SPL: 50, PL2SPPT: 50, FPPT: 50}
	d, rec := sliderDaemon(t, api.State{
		Profile:        "gaming",
		DeviceTDP:      &keep,
		CustomProfiles: map[string]api.CustomProfile{"gaming": {Name: "gaming", TDP: &saved}},
	})
	prof := &pinnedProfiles{}
	d.hw.Profiles, d.hw.ProfilePin = prof, "balanced"
	if resp := d.handleTDP(request{Set: "60"}); !resp.OK {
		t.Fatalf("handleTDP: %s", resp.Error)
	}
	if d.state.Profile != "balanced" || len(prof.sets) == 0 || prof.sets[len(prof.sets)-1] != "balanced" {
		t.Errorf("profile %q (writes %v), want the pinned balanced", d.state.Profile, prof.sets)
	}
	if d.state.DeviceTDP == nil || d.state.DeviceTDP.PL1SPL != 60 {
		t.Errorf("DeviceTDP = %+v, want 60 remembered", d.state.DeviceTDP)
	}
	if got := d.state.CustomProfiles["gaming"].TDP; got == nil || *got != saved {
		t.Errorf("gaming's TDP = %+v, want its saved 50 untouched", got)
	}
	if n := len(rec.applied); n == 0 || rec.applied[n-1].PL1SPL != 60 {
		t.Errorf("applied %+v, want 60 last", rec.applied)
	}
	for _, a := range rec.applied {
		if a.PL1SPL == 30 {
			t.Errorf("applied the old setting on the way out: %+v", rec.applied)
		}
	}
}

// Editing a running profile's TDP on a slider device is explicit: --profile
// names it, and the edit applies live and stays in the profile.
func TestSliderNamedEditOfTheRunningProfile(t *testing.T) {
	keep := api.TDPState{PL1SPL: 30, PL2SPPT: 30, FPPT: 30}
	d, rec := sliderDaemon(t, api.State{
		Profile:        "gaming",
		DeviceTDP:      &keep,
		CustomProfiles: map[string]api.CustomProfile{"gaming": {Name: "gaming", TDP: &api.TDPState{PL1SPL: 50, PL2SPPT: 50, FPPT: 50}}},
	})
	if resp := d.handleTDP(request{Set: "40", Profile: "gaming"}); !resp.OK {
		t.Fatalf("handleTDP: %s", resp.Error)
	}
	if got := d.state.CustomProfiles["gaming"].TDP; got == nil || got.PL1SPL != 40 || d.state.Profile != "gaming" {
		t.Errorf("gaming's TDP = %+v, profile %q; want 40 and still running", got, d.state.Profile)
	}
	if n := len(rec.applied); n == 0 || rec.applied[n-1].PL1SPL != 40 {
		t.Errorf("applied %+v, want 40 live", rec.applied)
	}
	if *d.state.DeviceTDP != keep {
		t.Errorf("DeviceTDP = %+v, want the slider's 30 untouched", d.state.DeviceTDP)
	}
}

// The same request on a device without the slider promotes to custom, as it
// always has — the model is device data, not a global change.
func TestNoSliderStillPromotesToCustom(t *testing.T) {
	d, _ := sliderDaemon(t, api.State{Profile: "balanced"})
	d.hw.PowerSlider = false
	d.hw.Profiles = nil
	if resp := d.handleTDP(request{Set: "40"}); !resp.OK {
		t.Fatalf("handleTDP: %s", resp.Error)
	}
	if d.state.DeviceTDP != nil || d.state.Profile != "custom" {
		t.Errorf("profile %q DeviceTDP %+v: want custom created and nothing remembered", d.state.Profile, d.state.DeviceTDP)
	}
}

func TestApplyDeviceTDPWritesOnlyWhatWasSet(t *testing.T) {
	d, rec := sliderDaemon(t, api.State{Profile: "balanced"})
	if wrote, err := d.applyDeviceTDP("startup"); wrote || err != nil || len(rec.applied) != 0 {
		t.Errorf("nothing remembered: wrote %v err %v applied %v — must write nothing", wrote, err, rec.applied)
	}
	d.state.DeviceTDP = &api.TDPState{PL1SPL: 45, PL2SPPT: 45, FPPT: 45}
	if wrote, err := d.applyDeviceTDP("startup"); !wrote || err != nil || len(rec.applied) != 1 {
		t.Errorf("remembered 45: wrote %v err %v applied %v", wrote, err, rec.applied)
	}
}

func TestReconcileTickSliderAndPin(t *testing.T) {
	env := oxpEnv()
	battery := env
	battery.TDPMaxForced, battery.TDPMaxSafe, battery.PL2.Max, battery.PL3.Max = 55, 55, 55, 60
	want := &api.TDPState{PL1SPL: 70, PL2SPPT: 70, FPPT: 70}
	base := reconcileObs{CurveMode: -1, PL1: -1, RawPL1: -1}

	cases := []struct {
		desc    string
		obs     func(o *reconcileObs)
		env     driver.PowerEnvelope
		prev    reconcileState
		wantTDP bool
		wantPin string
		routine bool
	}{
		{"in force", func(o *reconcileObs) { o.DeviceTDP, o.RawPL1 = want, 70 }, env, reconcileState{}, false, "", false},
		{"charger edge: the EC's preset", func(o *reconcileObs) {
			o.DeviceTDP, o.RawPL1, o.SourceKnown, o.OnAC = want, 80, true, true
		}, env, reconcileState{lastSource: sourceBattery}, true, "", true},
		{"on battery, held at the cap", func(o *reconcileObs) { o.DeviceTDP, o.RawPL1 = want, 55 }, battery, reconcileState{}, false, "", false},
		{"unread", func(o *reconcileObs) { o.DeviceTDP = want }, env, reconcileState{}, false, "", false},
		{"nothing remembered", func(o *reconcileObs) { o.RawPL1 = 80 }, env, reconcileState{}, false, "", false},
		{"another tool selected low-power", func(o *reconcileObs) { o.Pin, o.OffPin = "balanced", true }, env, reconcileState{}, false, "balanced", false},
		{"pin held", func(o *reconcileObs) { o.Pin = "balanced" }, env, reconcileState{}, false, "", false},
		{"suspending: stand down", func(o *reconcileObs) {
			o.Suspending, o.Pin, o.OffPin, o.DeviceTDP, o.RawPL1 = true, "balanced", true, want, 80
		}, env, reconcileState{}, false, "", false},
	}
	for _, c := range cases {
		obs := base
		c.obs(&obs)
		_, act := reconcileTick(c.prev, obs, c.env)
		if (act.TDP != nil) != c.wantTDP || act.Pin != c.wantPin {
			t.Errorf("%s: TDP %v pin %q; want TDP %v pin %q", c.desc, act.TDP, act.Pin, c.wantTDP, c.wantPin)
		}
		if c.wantTDP && act.Routine != c.routine {
			t.Errorf("%s: routine %v, want %v", c.desc, act.Routine, c.routine)
		}
	}
}

// Save As with no custom profile running keeps the TDP setting, and refuses
// while the firmware's preset is all there is to copy. Activating the copy
// later is what "load it back" means.
func TestSliderSaveAsKeepsTheTDPSetting(t *testing.T) {
	d, _ := sliderDaemon(t, api.State{Profile: "balanced"})
	if resp := d.handleProfileSave(request{Set: "handheld"}); resp.OK {
		t.Fatal("Save As with nothing set: want a refusal, the firmware preset is not a setting")
	}
	set := api.TDPState{PL1SPL: 25, PL2SPPT: 25, FPPT: 25}
	d.state.DeviceTDP = &set
	if resp := d.handleProfileSave(request{Set: "handheld"}); !resp.OK {
		t.Fatalf("Save As: %s", resp.Error)
	}
	p, ok := d.state.CustomProfiles["handheld"]
	if !ok || p.TDP == nil || *p.TDP != set || p.FanCurve != nil || p.Undervolt != nil {
		t.Fatalf("saved %+v, want only the TDP setting", p)
	}
	if d.state.Profile != "balanced" {
		t.Errorf("profile %q: Save As is a bookmark, not a switch", d.state.Profile)
	}
	d.state.DeviceTDP.PL1SPL = 60
	if d.state.CustomProfiles["handheld"].TDP.PL1SPL != 25 {
		t.Error("the saved profile aliases the TDP setting")
	}
}

// Without the slider model Save As still needs a custom profile running.
func TestSaveAsWithoutTheSliderStillNeedsACustomProfile(t *testing.T) {
	d, _ := sliderDaemon(t, api.State{Profile: "balanced"})
	d.hw.PowerSlider = false
	set := api.TDPState{PL1SPL: 25, PL2SPPT: 25, FPPT: 25}
	d.state.DeviceTDP = &set
	if resp := d.handleProfileSave(request{Set: "handheld"}); resp.OK {
		t.Fatal("want the usual refusal off the slider model")
	}
}
