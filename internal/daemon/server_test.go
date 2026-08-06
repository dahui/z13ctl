package daemon

// server_test.go — request validation and dispatch routing. These cases all
// return before any hardware access, so they need no Z13 and no sysfs.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
)

func TestDispatchUnknownCommand(t *testing.T) {
	d := &Daemon{}
	resp := d.dispatch(request{Cmd: "nope"})
	if resp.OK {
		t.Error("dispatch(nope).OK = true, want false")
	}
	if !strings.Contains(resp.Error, "unknown command") {
		t.Errorf("error = %q, want it to mention an unknown command", resp.Error)
	}
}

func TestHandleTDPRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		req     request
		wantErr string
	}{
		{"non-numeric watts", request{Cmd: "tdp", Set: "fast"}, "must be an integer"},
		{"non-numeric pl1", request{Cmd: "tdp", Set: "40", PL1: "x"}, "invalid pl1"},
		{"non-numeric pl2", request{Cmd: "tdp", Set: "40", PL2: "x"}, "invalid pl2"},
		{"non-numeric pl3", request{Cmd: "tdp", Set: "40", PL3: "x"}, "invalid pl3"},
		{"pl1 below minimum", request{Cmd: "tdp", Set: "1"}, "out of range"},
		{"pl1 above safe max without force", request{Cmd: "tdp", Set: "80"}, "use force flag"},
		{"pl1 above hardware max even with force", request{Cmd: "tdp", Set: "200", Force: true}, "out of range"},
		{"pl2 above hardware max", request{Cmd: "tdp", Set: "40", PL2: "200"}, "out of range"},
		{"pl3 below minimum", request{Cmd: "tdp", Set: "40", PL3: "1"}, "out of range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{}
			resp := d.handleTDP(tt.req)
			if resp.OK {
				t.Fatalf("handleTDP(%+v).OK = true, want a rejection", tt.req)
			}
			if !strings.Contains(resp.Error, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", resp.Error, tt.wantErr)
			}
		})
	}
}

// TestHandleTDPForceBoundaryRejections pins the asymmetry between sustained and
// burst limits: PL1 needs the force flag above TDPMaxSafe.
//
// Only rejection cases belong here. handleTDP writes straight to the real
// /sys/devices/platform/asus-nb-wmi/ppt_* nodes once validation passes, and
// internal/cli's path vars are unexported, so a case that gets past validation
// would change the developer's actual power limits as a test side effect.
// Accepted-input behaviour is covered hermetically in internal/cli/tdp_test.go.
func TestHandleTDPForceBoundaryRejections(t *testing.T) {
	d := &Daemon{}
	for _, watts := range []string{"76", "80", "93"} {
		if resp := d.handleTDP(request{Cmd: "tdp", Set: watts}); resp.OK {
			t.Errorf("PL1 %sW without the force flag was accepted, want a rejection", watts)
		}
	}
	// Exactly at the safe max is the last value that needs no force flag.
	if resp := d.handleTDP(request{Cmd: "tdp", Set: "94", Force: true}); resp.OK {
		t.Error("PL1 94W was accepted even with force, want a rejection above the hardware max")
	}
}

// TestHandleFanCurveRejectsInvalidCurve covers the only fan-curve path that is
// safe to exercise here: a malformed curve string is rejected by ParseFanCurve
// before anything reaches hwmon.
//
// The thermal-floor guards this handler now applies — cli.CheckFanCurveFloor on
// set and cli.CheckFanFloorRelease on reset — cannot be driven from this package
// without changing the machine's real fan mode, because whether they refuse
// depends on the actual ppt_* values and internal/cli's path vars are
// unexported. Both are covered hermetically in internal/cli/tdp_test.go against
// the fake sysfs tree.
func TestHandleFanCurveRejectsInvalidCurve(t *testing.T) {
	tests := []struct {
		name    string
		set     string
		wantErr string
	}{
		{"wrong point count", "40:100,50:150", "exactly 8 points"},
		{"missing pwm", "40,50,60,70,80,90,100,110", "expected temp:pwm"},
		{"non-numeric temp", "hot:100,50:150,60:160,65:170,70:180,75:190,80:200,85:210", "invalid temp"},
		{"pwm out of range", "40:300,50:300,60:300,65:300,70:300,75:300,80:300,85:300", "out of range"},
		{"temps not increasing", "40:100,40:150,60:160,65:170,70:180,75:190,80:200,85:210", "monotonically increasing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{}
			resp := d.handleFanCurve(request{Cmd: "fancurve", Set: tt.set})
			if resp.OK {
				t.Fatalf("handleFanCurve(%q).OK = true, want a rejection", tt.set)
			}
			if !strings.Contains(resp.Error, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", resp.Error, tt.wantErr)
			}
		})
	}
}

func TestHandleProfileRequiresSet(t *testing.T) {
	d := &Daemon{}
	resp := d.handleProfile(request{Cmd: "profile"})
	if resp.OK || !strings.Contains(resp.Error, "requires a set field") {
		t.Errorf("handleProfile with no set = %+v, want a validation error", resp)
	}
}

// TestHandleProfileCustomWithoutSavedSettings covers the guard that keeps
// "custom" from being selected before anything has been saved.
func TestHandleProfileCustomWithoutSavedSettings(t *testing.T) {
	d := &Daemon{}
	resp := d.handleProfile(request{Cmd: "profile", Set: "custom"})
	if resp.OK {
		t.Fatal("profile --set custom with no saved settings was accepted, want a rejection")
	}
	if !strings.Contains(resp.Error, "no settings saved") {
		t.Errorf("error = %q, want it to explain nothing is saved", resp.Error)
	}
}

func TestHandleBrightnessRejectsOutOfRange(t *testing.T) {
	d := &Daemon{}
	for _, level := range []int{-1, 4, 99} {
		resp := d.handleBrightness(request{Cmd: "brightness", Brightness: level})
		if resp.OK || !strings.Contains(resp.Error, "out of range") {
			t.Errorf("brightness %d = %+v, want a range rejection", level, resp)
		}
	}
}

func TestHandleBatteryLimitRejectsOutOfRange(t *testing.T) {
	d := &Daemon{}
	for _, v := range []string{"39", "101", "abc", ""} {
		resp := d.handleBatteryLimit(request{Cmd: "batterylimit", Set: v})
		if resp.OK {
			t.Errorf("batterylimit %q was accepted, want a rejection", v)
		}
	}
}

// TestRestoreStockPPTIgnoresUnknownProfiles guards the lookup that keeps the
// virtual "custom" profile from being handed the stock table.
func TestRestoreStockPPTIgnoresUnknownProfiles(t *testing.T) {
	for _, p := range []string{"custom", "", "turbo", "unknown"} {
		t.Run(p, func(t *testing.T) {
			if _, ok := cli.StockProfilePPT[p]; ok {
				t.Fatalf("%q is in StockProfilePPT; this test needs a name that is not", p)
			}
			// Must be a no-op: no lookup hit means no PPT write and no panic.
			restoreStockPPT(p)
		})
	}
}

// TestEffectiveProfilePrefersDaemonState is the regression guard for the report
// bug where a legitimate 5W custom TDP was displayed as the stock table:
// platform_profile is never "custom", so only daemon state can tell them apart.
func TestEffectiveProfilePrefersDaemonState(t *testing.T) {
	d := &Daemon{state: api.State{Profile: "custom"}}
	if got := d.effectiveProfile(); got != "custom" {
		t.Errorf("effectiveProfile() = %q, want \"custom\" from daemon state", got)
	}

	d = &Daemon{state: api.State{Profile: "quiet"}}
	if got := d.effectiveProfile(); got != "quiet" {
		t.Errorf("effectiveProfile() = %q, want \"quiet\"", got)
	}
}

func TestResponseMarshalsProtocolShape(t *testing.T) {
	data, err := json.Marshal(response{OK: true, Value: "52"})
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["value"] != "52" {
		t.Errorf("value = %v, want \"52\"", got["value"])
	}
	// Absent fields must stay omitted so clients can distinguish unset.
	if _, ok := got["error"]; ok {
		t.Error("error key present on a success response, want it omitted")
	}
	if _, ok := got["state"]; ok {
		t.Error("state key present when nil, want it omitted")
	}
}

func TestRequestUnmarshalsProtocolShape(t *testing.T) {
	var req request
	raw := `{"cmd":"tdp","set":"60","pl1":"55","pl2":"65","pl3":"70","force":true}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("Unmarshal() = %v", err)
	}
	want := request{Cmd: "tdp", Set: "60", PL1: "55", PL2: "65", PL3: "70", Force: true}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("request = %+v, want %+v", req, want)
	}
}

// The profile handlers below stay strictly on validation and refusal paths.
// internal/cli's sysfs path vars are unexported, so a daemon test that got past
// validation into applyProfileLocked would rewrite the developer's real power
// limits, fan mode, and Curve Optimizer offset — the same warning as
// TestHandleTDPForceBoundaryRejections.

func TestHandleProfileCreateRejectsBadNames(t *testing.T) {
	d := &Daemon{}
	for _, name := range []string{"", "quiet", "balanced", "performance", "custom", "Gaming", "my profile", "a/b"} {
		resp := d.handleProfileCreate(request{Cmd: "profile-create", Set: name})
		if resp.OK {
			t.Errorf("profile-create %q was accepted, want a rejection", name)
		}
	}
}

// TestHandleProfileRejectsUnknownNames is the guard that keeps a typo out of
// cli.SetProfile. Before named profiles the daemon forwarded any string to
// platform_profile; a mistyped name would now reach ResetAllFanCurves and drop
// the user's fan curve on the way to failing.
func TestHandleProfileRejectsUnknownNames(t *testing.T) {
	d := &Daemon{state: api.State{Profile: "balanced"}}
	for _, name := range []string{"performanc", "low-power", "gaming"} {
		resp := d.handleProfile(request{Cmd: "profile", Set: name})
		if resp.OK {
			t.Fatalf("profile --set %q was accepted, want a rejection before any hardware write", name)
		}
		if !strings.Contains(resp.Error, "unknown profile") {
			t.Errorf("error for %q = %q, want it to say the profile is unknown", name, resp.Error)
		}
	}
}

func TestHandleProfileDeleteRefusals(t *testing.T) {
	base := func() api.State {
		return api.State{
			Profile: "gaming",
			CustomProfiles: map[string]api.CustomProfile{
				"gaming":     {Name: "gaming"},
				"battery-uv": {Name: "battery-uv"},
				"spare":      {Name: "spare"},
			},
			Autoswitch: &api.AutoswitchState{Enabled: true, AC: "balanced", Battery: "battery-uv"},
		}
	}
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"unknown profile", "nope", "no saved profile"},
		{"the active profile", "gaming", "active profile"},
		{"referenced by autoswitch", "battery-uv", "autoswitch battery profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{state: base()}
			resp := d.handleProfileDelete(request{Cmd: "profile-delete", Set: tt.target})
			if resp.OK {
				t.Fatalf("deleting %q was accepted, want a rejection", tt.target)
			}
			if !strings.Contains(resp.Error, tt.want) {
				t.Errorf("error = %q, want it to mention %q", resp.Error, tt.want)
			}
		})
	}
}

func TestHandleAutoswitchRejectsUnknownTargets(t *testing.T) {
	d := &Daemon{state: api.State{
		CustomProfiles: map[string]api.CustomProfile{"battery-uv": {Name: "battery-uv"}},
	}}
	for _, req := range []request{
		{Cmd: "autoswitch", Enabled: true, AC: "turbo", Battery: "battery-uv"},
		{Cmd: "autoswitch", Enabled: true, AC: "balanced", Battery: "deleted"},
	} {
		resp := d.handleAutoswitch(req)
		if resp.OK {
			t.Errorf("autoswitch %+v was accepted, want a rejection", req)
		}
	}
}

// TestResolveEditTargetLocked covers where a setting command lands. The empty
// name must behave exactly as z13ctl did before named profiles existed: create
// and activate "custom" when a firmware profile is running.
func TestResolveEditTargetLocked(t *testing.T) {
	state := api.State{
		Profile: "balanced",
		CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming", TDP: &api.TDPState{PL1SPL: 70}},
		},
	}
	tests := []struct {
		name     string
		active   string
		arg      string
		wantName string
		wantLive bool
		wantErr  bool
	}{
		{name: "empty on a firmware profile creates and activates custom", active: "balanced", arg: "", wantName: "custom", wantLive: true},
		{name: "empty on a custom profile edits it in place", active: "gaming", arg: "", wantName: "gaming", wantLive: true},
		{name: "naming the active profile is live", active: "gaming", arg: "gaming", wantName: "gaming", wantLive: true},
		{name: "naming another profile is not live", active: "balanced", arg: "gaming", wantName: "gaming", wantLive: false},
		{name: "custom is addressable before it exists", active: "balanced", arg: "custom", wantName: "custom", wantLive: false},
		{name: "a firmware profile has nothing to edit", active: "balanced", arg: "balanced", wantErr: true},
		{name: "an unsaved name is rejected", active: "balanced", arg: "nope", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := cloneState(state)
			s.Profile = tt.active
			d := &Daemon{state: s}
			d.mu.Lock()
			got, err := d.resolveEditTargetLocked(tt.arg)
			d.mu.Unlock()
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveEditTargetLocked(%q) error = %v, wantErr %v", tt.arg, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Name != tt.wantName || got.Live != tt.wantLive {
				t.Errorf("resolveEditTargetLocked(%q) = {Name:%q Live:%v}, want {Name:%q Live:%v}",
					tt.arg, got.Name, got.Live, tt.wantName, tt.wantLive)
			}
		})
	}
}

// TestEditTargetIsACopy guards commitEditLocked's premise: the target carries a
// snapshot, so building the edited profile from it cannot mutate live state
// before the commit — and cannot alias it afterwards.
func TestEditTargetIsACopy(t *testing.T) {
	d := &Daemon{state: api.State{
		Profile: "gaming",
		CustomProfiles: map[string]api.CustomProfile{
			"gaming": {Name: "gaming", TDP: &api.TDPState{PL1SPL: 70}},
		},
	}}
	d.mu.Lock()
	target, err := d.resolveEditTargetLocked("")
	d.mu.Unlock()
	if err != nil {
		t.Fatalf("resolveEditTargetLocked: %v", err)
	}
	target.Profile.TDP.PL1SPL = 5
	if d.state.CustomProfiles["gaming"].TDP.PL1SPL != 70 {
		t.Error("editing the target mutated live daemon state before any commit")
	}
}
