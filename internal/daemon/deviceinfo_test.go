package daemon

// deviceinfo_test.go — the device-get capability document and the feature
// command validation paths.
//
// deviceInfoFor is pure constructor data, so unlike most handler tests these
// can drive the full Z13 assembly with no hardware risk. The feature handlers
// stay on rejection paths for the usual reason: a request that passes
// validation writes the developer's real firmware attributes.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/limits"
)

// TestDeviceGetProjectsTheAssembledDevice pins the document against the same
// embedded config the test device was assembled from: TOML → registry →
// drivers → wire, one set of numbers throughout.
func TestDeviceGetProjectsTheAssembledDevice(t *testing.T) {
	d := &Daemon{hw: testDev}
	resp := d.handleDeviceGet()
	if !resp.OK || resp.Device == nil {
		t.Fatalf("handleDeviceGet() = %+v, want ok with a device document", resp)
	}
	info := resp.Device

	c := z13TestConfig(t)
	if info.ID != c.Device.ID || info.Model != c.Device.Model {
		t.Errorf("identity = %s/%s, want %s/%s", info.ID, info.Model, c.Device.ID, c.Device.Model)
	}

	shape := c.Fans.Shape()
	if info.Fans == nil {
		t.Fatal("fans section missing")
	}
	if info.Fans.Points != shape.Points || info.Fans.TempMin != shape.TempMin ||
		info.Fans.TempMax != shape.TempMax || info.Fans.PWMMax != shape.PWMMax {
		t.Errorf("fans = %+v, want the config's shape %+v", info.Fans, shape)
	}

	env := c.Power.Envelope()
	if info.Power == nil {
		t.Fatal("power section missing")
	}
	if info.Power.TDPMin != env.TDPMin || info.Power.TDPMaxSafe != env.TDPMaxSafe ||
		info.Power.TDPMaxForced != env.TDPMaxForced {
		t.Errorf("power limits = %+v, want the envelope's %d/%d/%d",
			info.Power, env.TDPMin, env.TDPMaxSafe, env.TDPMaxForced)
	}
	if len(info.Power.FloorCurve) != len(env.FloorCurve) {
		t.Fatalf("floor curve has %d points, want %d", len(info.Power.FloorCurve), len(env.FloorCurve))
	}
	for i := range env.FloorCurve {
		if info.Power.FloorCurve[i] != env.FloorCurve[i] {
			t.Errorf("floor_curve[%d] = %+v, want %+v", i, info.Power.FloorCurve[i], env.FloorCurve[i])
		}
	}
	// The wire slice must be a copy: a client-driven mutation through a shared
	// backing array would rewrite the envelope every later floor decision uses.
	info.Power.FloorCurve[0].PWM = 1
	if testDev.Power.Envelope().FloorCurve[0].PWM == 1 {
		t.Error("mutating the document's floor curve reached the engine's envelope")
	}

	if len(info.Power.StockProfilePPT) != len(env.StockProfilePPT) {
		t.Fatalf("stock_profile_ppt has %d entries, want %d — a client cannot tell "+
			"a firmware limit from one the user chose without them",
			len(info.Power.StockProfilePPT), len(env.StockProfilePPT))
	}
	for name, want := range env.StockProfilePPT {
		if got := info.Power.StockProfilePPT[name]; got != want {
			t.Errorf("stock_profile_ppt[%q] = %+v, want %+v", name, got, want)
		}
	}
	// Copied for the same reason as the floor curve, and worth its own check:
	// a map is shared by reference even when the struct around it is copied.
	for name := range info.Power.StockProfilePPT {
		info.Power.StockProfilePPT[name] = api.TDPState{PL1SPL: 1}
	}
	for name, v := range testDev.Power.Envelope().StockProfilePPT {
		if v.PL1SPL == 1 {
			t.Errorf("mutating the document's stock table reached the engine's envelope at %q", name)
		}
	}

	if info.Profiles == nil || len(info.Profiles.Names) != len(c.Profiles.Names) {
		t.Errorf("profiles = %+v, want the config's names %v", info.Profiles, c.Profiles.Names)
	}
	if info.Lighting == nil || len(info.Lighting.Zones) != 2 {
		t.Errorf("lighting = %+v, want the two Z13 zones", info.Lighting)
	}
	if len(info.Toggles) != len(c.Toggles.Entries) {
		t.Fatalf("toggles = %+v, want %d entries", info.Toggles, len(c.Toggles.Entries))
	}
	for i, e := range c.Toggles.Entries {
		if info.Toggles[i].ID != e.ID || info.Toggles[i].Label != e.Label {
			t.Errorf("toggle[%d] = %+v, want %+v", i, info.Toggles[i], e)
		}
		if info.Toggles[i].Kind == "" {
			t.Errorf("toggle[%d] has no kind; clients need it to shape the control", i)
		}
	}
	if info.Undervolt == nil || info.Undervolt.Min != c.Undervolt.Min || info.Undervolt.Max != c.Undervolt.Max {
		t.Errorf("undervolt = %+v, want the config's %d..%d", info.Undervolt, c.Undervolt.Min, c.Undervolt.Max)
	}
	if info.Battery == nil || !info.Battery.ChargeLimit || !info.Battery.Health {
		t.Errorf("battery = %+v, want both charge_limit and health on the Z13", info.Battery)
	}
	tel := c.Telemetry.Info()
	if info.Telemetry == nil || info.Telemetry.HistorySeconds != tel.HistorySeconds ||
		info.Telemetry.PowerDraw != tel.PowerDraw {
		t.Errorf("telemetry = %+v, want the config's %+v", info.Telemetry, tel)
	}
	if !info.Buttons {
		t.Error("buttons = false, want true on the Z13")
	}
}

// TestTelemetryDeclaresNoPowerSourceYet is an honesty guard, not a statement
// about what the Z13 can do. The machine does expose powercap RAPL, but
// energy_uj is root-only (the Platypus mitigation) and asusz13's Sample()
// reads nothing for it, so PackagePowerW is always zero. Naming a source in
// the document while that is true tells a dashboard to draw a graph that is
// flat at zero, where an absent source tells it to hide the graph — capability
// absence is the whole contract clients render against.
//
// So this fails the day someone implements the read without declaring it, and
// it fails just as loudly the day someone declares it without implementing the
// read. Either way the fix is to make the two agree and delete this test.
func TestTelemetryDeclaresNoPowerSourceYet(t *testing.T) {
	info := deviceInfoFor(testDev)
	if info.Telemetry == nil {
		t.Fatal("telemetry section missing")
	}
	if info.Telemetry.PowerDraw != "" {
		t.Errorf("telemetry.power_draw = %q; if Sample() now reads package power, "+
			"delete this test — if it does not, the document is claiming a graph "+
			"that would be flat at zero", info.Telemetry.PowerDraw)
	}
	// Sample() only reads sysfs, so this is safe here in the way a write would
	// not be; it fails on a machine that is not a Z13 and that is fine to skip.
	if s, err := testDev.Telemetry.Sample(); err == nil && s.PackagePowerW != 0 {
		t.Errorf("Sample reports %.1fW of package power but the document names no "+
			"source; declare it in the device data", s.PackagePowerW)
	}
}

// TestDocumentMatchesTheDrawersFallback closes the loop the drawer now depends
// on: device TOML → registry → driver envelope → wire document → limits.Limits
// must land exactly on limits.DefaultLimits, the value voltaire-gui builds its
// widgets from when the daemon cannot be reached.
//
// The drawer fetches device-get at startup and falls back to DefaultLimits on
// any failure, so the two are alternatives for the same job and a difference
// between them is a drawer that behaves differently depending on whether the
// daemon happened to answer. That drift is not hypothetical: the fan floor
// dropped from a flat 80% to a 50%-bottomed ramp on the daemon side and the
// drawer's copy clamped at 80% for a release, with nothing to catch it.
//
// This is the only place both halves are reachable at once — internal/limits
// cannot import the daemon — so the assertion lives here despite limits being
// a GUI-side package.
func TestDocumentMatchesTheDrawersFallback(t *testing.T) {
	got := limits.FromDevice(deviceInfoFor(testDev))
	want := limits.DefaultLimits()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the Z13 device document does not convert to the drawer's fallback.\n"+
			"One of them changed without the other:\n from device-get: %+v\n DefaultLimits: %+v", got, want)
	}
}

// TestDeviceGetCapabilityByAbsence pins the wire contract the driver package
// doc promises: a capability the device does not have is an omitted key, never
// an error or a zeroed section.
func TestDeviceGetCapabilityByAbsence(t *testing.T) {
	info := deviceInfoFor(&device.Device{ID: "bare", Model: "Bare"})
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"id":"bare","model":"Bare"}`; got != want {
		t.Errorf("bare device document = %s, want %s — absent capabilities must be absent keys", got, want)
	}

	// A nil device answers an empty document rather than panicking; the daemon
	// always has hw set, but the projection must not depend on it.
	if doc := deviceInfoFor(nil); doc == nil {
		t.Error("deviceInfoFor(nil) = nil, want an empty document")
	}
}

// TestDeviceGetWireKeys pins the JSON key names of the capability sections.
// The Go field names are ours to rename; these are not — a Decky plugin or any
// other non-Go client reads them literally, and nothing else in the suite would
// notice a struct tag being edited.
func TestDeviceGetWireKeys(t *testing.T) {
	data, err := json.Marshal(deviceInfoFor(testDev))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"battery":{"charge_limit":true,"health":true}`,
		`"telemetry":{"history_seconds":300}`, // power_draw omitted: no source declared
		`"buttons":true`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("document does not contain %s\ngot: %s", want, data)
		}
	}
}

func TestFeatureRejections(t *testing.T) {
	d := &Daemon{hw: testDev}
	tests := []struct {
		name string
		req  request
		want string
	}{
		{"set missing id", request{Cmd: "feature", Set: "1"}, "missing id"},
		{"set unknown id", request{Cmd: "feature", ID: "warp-drive", Set: "1"}, `unknown feature "warp-drive"`},
		{"set non-integer value", request{Cmd: "feature", ID: "boot_sound", Set: "on"}, "must be an integer"},
		{"set bool out of range", request{Cmd: "feature", ID: "boot_sound", Set: "2"}, "must be 0 or 1"},
		{"get missing id", request{Cmd: "feature-get"}, "missing id"},
		{"get unknown id", request{Cmd: "feature-get", ID: "warp-drive"}, `unknown feature "warp-drive"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := d.dispatch(tt.req)
			if resp.OK {
				t.Fatalf("dispatch(%+v) = ok, want a rejection", tt.req)
			}
			if !strings.Contains(resp.Error, tt.want) {
				t.Errorf("error %q does not mention %q", resp.Error, tt.want)
			}
		})
	}

	// No toggles at all is a capability-absence error, not a lookup error.
	bare := &Daemon{hw: &device.Device{ID: "bare"}}
	if resp := bare.dispatch(request{Cmd: "feature-get", ID: "boot_sound"}); resp.OK ||
		!strings.Contains(resp.Error, "no firmware toggles") {
		t.Errorf("feature-get on a toggle-less device = %+v, want the no-toggles error", resp)
	}
}

// z13TestConfig returns the embedded config testDev was assembled from.
func z13TestConfig(t *testing.T) device.Config {
	t.Helper()
	configs, err := device.Configs()
	if err != nil {
		t.Fatalf("Configs: %v", err)
	}
	for _, c := range configs {
		if c.Device.ID == fallbackDeviceID {
			return c
		}
	}
	t.Fatal("Z13 device file not found")
	return device.Config{}
}
