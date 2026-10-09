package daemon

// server_test.go — request validation and dispatch routing. These cases all
// return before any hardware access, so they need no Z13 and no sysfs.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/safety"
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
		{"pl1 above safe max without force", request{Cmd: "tdp", Set: "80"}, "use force"},
		{"pl1 above hardware max even with force", request{Cmd: "tdp", Set: "200", Force: true}, "out of range"},
		{"pl2 above hardware max", request{Cmd: "tdp", Set: "40", PL2: "200"}, "out of range"},
		{"pl3 above hardware max", request{Cmd: "tdp", Set: "40", PL3: "200"}, "out of range"},
		// Every case must be refused on both interfaces' ranges — asus-armoury's
		// (PL1 28–80, PL2 32–92, PL3 45–93 on the GZ302EA) and the device file's
		// 5–93 — since testDev reads whichever the machine has. A PL2 or PL3 below
		// its minimum is raised, not refused, so it is not a rejection case at all:
		// "pl3 below minimum" stood here and would now write the real limits.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{hw: testDev}
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
// Only rejection cases belong here. handleTDP writes straight to the real PPT
// attributes (asus-armoury's, or asus-nb-wmi's ppt_*) once validation passes, and
// the Z13 driver's path vars are unexported, so a case that gets past validation
// would change the developer's actual power limits as a test side effect.
// Accepted-input behaviour is covered hermetically in internal/drivers/asusz13/tdp_test.go.
func TestHandleTDPForceBoundaryRejections(t *testing.T) {
	d := &Daemon{hw: testDev}
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
// The thermal-floor guards this handler now applies — the curve-vs-limit check
// on set and the floor-release check on reset — cannot be driven from this
// package without changing the machine's real fan mode, because whether they
// refuse depends on the actual ppt_* values and the Z13 driver's path vars are
// unexported. Both are covered hermetically in internal/drivers/asusz13 and
// internal/safety against the fake sysfs tree.
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
			// testDev, not an empty daemon: the parse is judged against the
			// device's fan shape, so the capability guard now precedes it.
			// Every case still returns from the parse, before any hardware.
			d := &Daemon{hw: testDev}
			resp := d.handleFanCurve(request{Cmd: "fancurve", Set: tt.set})
			if resp.OK {
				t.Fatalf("handleFanCurve(%q).OK = true, want a rejection", tt.set)
			}
			if !strings.Contains(resp.Error, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", resp.Error, tt.wantErr)
			}
		})
	}

	// And a device with no fan control rejects on the capability, before the
	// parse has a shape to judge against.
	d := &Daemon{}
	if resp := d.handleFanCurve(request{Cmd: "fancurve", Set: "40:100,50:150"}); resp.OK ||
		!strings.Contains(resp.Error, "no fan control") {
		t.Errorf("fanless device = %+v, want the no-fan-control rejection", resp)
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
	d := &Daemon{hw: testDev}
	for _, p := range []string{"custom", "", "turbo", "unknown"} {
		t.Run(p, func(t *testing.T) {
			if _, ok := d.env().StockProfilePPT[p]; ok {
				t.Fatalf("%q is in the stock PPT table; this test needs a name that is not", p)
			}
			// Must be a no-op: no lookup hit means no PPT write and no panic.
			d.restoreStockPPT(p)
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
// the Z13 driver's sysfs path vars are unexported, so a daemon test that got past
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

// TestDispatchRefusesECCommandsWhileWedged covers the socket half of the
// wedged-EC latch. The watchers stand down on their own; a command from the CLI
// or GUI would otherwise walk straight into the stalled EC. The refusal returns
// before any handler runs, which is also what makes this safe to test — nothing
// here reaches the driver.
func TestDispatchRefusesECCommandsWhileWedged(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		"profile", "fancurve", "fancurve-reset", "tdp", "tdp-reset", "tuning-reset",
		"batterylimit", "bootsound", "paneloverdrive", "feature",
		"bootsound-get", "paneloverdrive-get", "feature-get", "tdp-get",
	} {
		t.Run(cmd, func(t *testing.T) {
			t.Parallel()
			d := &Daemon{hw: testDev, ecWedged: true}
			resp := d.dispatch(request{Cmd: cmd, Set: "1"})
			if resp.OK || resp.Error != errECWedged {
				t.Errorf("dispatch(%s) while wedged = %+v, want the wedged-EC refusal", cmd, resp)
			}
		})
	}
}

// TestDispatchLeavesNonECCommandsAlone is the other side: lighting, state-only
// commands, cpufreq boost, the telemetry ring and undervolt (the SMU, not the
// EC) are not the EC's business.
func TestDispatchLeavesNonECCommandsAlone(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		"apply", "off", "brightness", "profile-get", "profile-create", "profile-save",
		"profile-delete", "profile-list", "autoswitch", "autoswitch-get",
		"batterylimit-get", "device-get", "telemetry-history", "cpuboost", "cpuboost-get",
		"fancurve-get", "undervolt", "undervolt-get", "undervolt-reset",
		"get-state", "subscribe",
	} {
		if ecGuarded(cmd) {
			t.Errorf("ecGuarded(%q) = true; it does not reach the EC", cmd)
		}
	}
	d := &Daemon{hw: testDev, ecWedged: true}
	if resp := d.dispatch(request{Cmd: "profile-list"}); !resp.OK {
		t.Errorf("profile-list while wedged = %+v, want OK: it touches only state", resp)
	}
}

// spyBattery reports mains power and records whether it was read at all.
type spyBattery struct{ read *bool }

func (b spyBattery) Caps() driver.BatteryCaps   { return driver.BatteryCaps{} }
func (b spyBattery) ChargeLimit() (int, error)  { return 0, nil }
func (b spyBattery) SetChargeLimit(_ int) error { return nil }
func (b spyBattery) Status() (driver.BatteryStatus, error) {
	*b.read = true
	return driver.BatteryStatus{OnAC: true, ACKnown: true}, nil
}

// TestAutoswitchGetReportsUnknownSourceWhileWedged covers the one EC read the
// latch missed (z13ctl v1.3.4). autoswitch-get changes nothing, so it was never
// in ecGuarded, but its live source comes from the battery driver's AC read, and
// the AC driver answers that by evaluating _PSR on the EC. While wedged it must
// report the source as unknown without reading it. The control case proves the
// spy is reachable, so the wedged case cannot pass merely because nothing was
// wired up.
func TestAutoswitchGetReportsUnknownSourceWhileWedged(t *testing.T) {
	t.Parallel()
	for _, wedged := range []bool{false, true} {
		read := false
		d := &Daemon{ecWedged: wedged, hw: &device.Device{Battery: spyBattery{&read}}}
		resp := d.dispatch(request{Cmd: "autoswitch-get"})
		if !resp.OK {
			t.Fatalf("wedged=%v: autoswitch-get = %+v, want OK: it is a read, not refused", wedged, resp)
		}
		var got struct {
			OnAC  bool `json:"on_ac"`
			Known bool `json:"source_known"`
		}
		if err := json.Unmarshal([]byte(resp.Value), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", resp.Value, err)
		}
		if wedged && (read || got.Known || got.OnAC) {
			t.Errorf("while wedged: read=%v source=%+v, want no read and unknown: the AC read is an EC call", read, got)
		}
		if !wedged && (!read || !got.Known || !got.OnAC) {
			t.Errorf("healthy: read=%v source=%+v, want the battery driver's answer", read, got)
		}
	}
}

// TestGetStateSkipsArmouryPPTWhileWedged: on asus-armoury every PPT read —
// current_value and the bounds alike — goes through get_current_tunables(),
// which evaluates the AC adapter's _PSR to choose the AC or battery table. That
// is a live ACPI call into the EC, not the cache asus-nb-wmi's ppt_* were, so
// while the latch is set get-state must not read the limits.
//
// Hermetic with the gate in place: nothing is read. Without it the test reads
// the machine's real PPT interface and fails on any machine that has one.
func TestGetStateSkipsArmouryPPTWhileWedged(t *testing.T) {
	d := &Daemon{hw: testDev, ecWedged: true}
	resp := d.dispatch(request{Cmd: "get-state"})
	if !resp.OK || resp.State == nil {
		t.Fatalf("get-state while wedged = %+v, want OK: it is a read, not refused", resp)
	}
	if resp.State.TDP != nil {
		t.Errorf("tdp while wedged = %+v, want the state projection (none here), not a readback", resp.State.TDP)
	}
}

// countingPower is a power limiter that counts how often its envelope is read,
// and (when limitReads is set) how often its limits are.
type countingPower struct {
	reads      *int
	limitReads *int
	live       driver.PowerEnvelope
	data       driver.PowerEnvelope
}

func (p countingPower) Read() (api.TDPState, error) {
	if p.limitReads != nil {
		*p.limitReads++
	}
	return api.TDPState{PL1SPL: 40, PL2SPPT: 40, FPPT: 45}, nil
}
func (countingPower) Apply(api.TDPState) error { return nil }
func (p countingPower) Envelope() driver.PowerEnvelope {
	*p.reads++
	return p.live
}
func (p countingPower) DeviceEnvelope() driver.PowerEnvelope { return p.data }

// TestEnvDoesNotReadTheKernelWhileWedged: the envelope the daemon validates and
// serves comes from the driver, which on asus-armoury reads the kernel's bounds
// (a live _PSR evaluation each). While the EC is not answering it must come
// from the last good read, or from device data when there is none — never the
// driver. device-get goes through the same path.
func TestEnvDoesNotReadTheKernelWhileWedged(t *testing.T) {
	reads := 0
	live := driver.PowerEnvelope{TDPMin: 28, TDPMaxSafe: 75, TDPMaxForced: 80, Interface: "asus-armoury"}
	data := driver.PowerEnvelope{TDPMin: 5, TDPMaxSafe: 75, TDPMaxForced: 93}
	hw := &device.Device{ID: "z13", Power: &safety.Engine{Power: countingPower{reads: &reads, live: live, data: data}}}

	d := &Daemon{hw: hw, ecWedged: true}
	if got := d.env(); got.TDPMin != data.TDPMin || got.Interface != "" {
		t.Errorf("env() wedged with nothing cached = %+v, want the device data", got)
	}
	if p := d.handleDeviceGet().Device.Power; p == nil || p.TDPMin != data.TDPMin {
		t.Errorf("device-get wedged with nothing cached = %+v, want the device data", p)
	}
	if reads != 0 {
		t.Fatalf("the driver's envelope was read %d times while wedged, want none", reads)
	}

	d.setECWedged(false)
	if got := d.env(); got.Interface != "asus-armoury" || reads != 1 {
		t.Fatalf("env() answering = %+v after %d reads, want the driver's, read once", got, reads)
	}
	d.setECWedged(true)
	if got := d.env(); got.Interface != "asus-armoury" || got.TDPMin != 28 {
		t.Errorf("env() wedged after a good read = %+v, want that read", got)
	}
	if p := d.handleDeviceGet().Device.Power; p == nil || p.Interface != "asus-armoury" {
		t.Errorf("device-get wedged after a good read = %+v, want that read", p)
	}
	if reads != 1 {
		t.Errorf("the driver's envelope was read %d times, want 1: none while wedged", reads)
	}
}

// TestEnvReusesARecentRead: the reconcile watcher asks for the envelope every
// two seconds, and on asus-armoury a fresh one is nine live ACPI evaluations.
func TestEnvReusesARecentRead(t *testing.T) {
	reads := 0
	hw := &device.Device{ID: "z13", Power: &safety.Engine{Power: countingPower{reads: &reads}}}
	d := &Daemon{hw: hw}
	d.env()
	d.env()
	if reads != 1 {
		t.Errorf("two env() calls within the TTL read the driver %d times, want 1", reads)
	}
	orig := envCacheTTL
	envCacheTTL = 0
	t.Cleanup(func() { envCacheTTL = orig })
	d.env()
	if reads != 2 {
		t.Errorf("env() past the TTL read the driver %d times in all, want 2", reads)
	}
}

// TestReconcileReadsPowerOnlyWhenItCanAct: on asus-armoury every limit read is
// a live _PSR evaluation, so the watcher reads them only for a custom profile it
// is not standing down for. A stock profile only ever fed a log line, and
// while suspending — which covers the post-resume wait for the EC, before the
// wedge latch is set — the tick stands down regardless.
func TestReconcileReadsPowerOnlyWhenItCanAct(t *testing.T) {
	tdp := &api.TDPState{PL1SPL: 40, PL2SPPT: 40, FPPT: 45}
	tests := []struct {
		name       string
		profile    string
		suspending bool
		wantReads  bool
	}{
		{"stock profile", "balanced", false, false},
		{"custom profile while suspending", "custom", true, false},
		{"custom profile", "custom", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envReads, limitReads := 0, 0
			hw := &device.Device{ID: "z13", Power: &safety.Engine{Power: countingPower{reads: &envReads, limitReads: &limitReads}}}
			d := &Daemon{hw: hw, suspending: tt.suspending}
			d.state.Profile = tt.profile
			d.state.CustomProfiles = map[string]api.CustomProfile{"custom": {Name: "custom", TDP: tdp}}
			d.reconcileOnce(reconcileState{lastHW: "balanced"})
			if got := limitReads > 0 || envReads > 0; got != tt.wantReads {
				t.Errorf("power reads = %d limits / %d envelope, want any = %v", limitReads, envReads, tt.wantReads)
			}
		})
	}
}
