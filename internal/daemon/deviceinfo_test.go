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
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/asusz13"
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

// TestTelemetryDeclarationMatchesWhatIsRead is the honesty guard the device
// document's whole contract rests on: capability absence is what tells a client
// to hide a control, so a declared-but-unread source does not degrade to
// "nothing shown" — it degrades to a chart drawn flat at zero, which reads as a
// measurement.
//
// It replaced TestTelemetryDeclaresNoPowerSourceYet, which pinned the *absence*
// of power_draw while asusz13 read nothing for it. Both directions still fail
// loudly; only the expected answer changed, because Sample now reads the
// powercap energy counter.
//
// The counter, not a wattage: converting needs two readings, so a single Sample
// legitimately reports PackageEnergyUJ and no PackagePowerW. This checks the
// counter, which is what the declaration is actually about.
//
// Sample only reads sysfs, so this is safe here in the way a write would not
// be. It tolerates an unreadable counter: energy_uj is 0400 root:root until
// "voltaire setup" grants group read, and a developer machine without that
// grant must not fail the suite — the case it exists to catch is a *readable*
// counter with no declaration, or a declaration with no reader at all.
func TestTelemetryDeclarationMatchesWhatIsRead(t *testing.T) {
	info := deviceInfoFor(testDev)
	if info.Telemetry == nil {
		t.Fatal("telemetry section missing")
	}
	declared := info.Telemetry.PowerDraw != ""

	s, err := testDev.Telemetry.Sample()
	if err != nil {
		t.Skipf("no telemetry on this machine: %v", err)
	}
	reads := s.PackageEnergyUJ != 0 || s.PackagePowerW != 0

	switch {
	case reads && !declared:
		t.Error("Sample reads package power but the document names no source; " +
			"declare it in the device data or a client will hide a graph it could draw")
	case declared && !reads:
		// Distinguish "not implemented" from "not permitted". Only the first
		// is a defect; the second is a machine that has not run setup.
		if _, _, rErr := asusz13.ReadPackageEnergy(); rErr != nil {
			t.Skipf("the counter is declared but unreadable here (%v); "+
				"run 'sudo voltaire setup' to grant it", rErr)
		}
		t.Error("the document names a power source but Sample reads nothing for it; " +
			"a client would draw a chart flat at zero, which reads as a measurement")
	}

	// The expanded sources, on the same both-directions terms. Each case
	// checks one field that source alone fills, and each declared-but-unread
	// arm distinguishes a missing reader from hardware this machine simply
	// does not offer (a VM with no amdgpu card, a kernel without amdxdna) —
	// absence of the *device* is a skip, absence of the *reader* a failure.
	if gpuReads := s.GPUTempC != 0 || s.GPUBusyKnown || s.GPUClockMHz != 0; gpuReads != (info.Telemetry.GPU != "") {
		if info.Telemetry.GPU != "" {
			if _, err := asusz13.ReadGPUBusyPct(); err != nil {
				t.Skipf("gpu declared but no readable amdgpu here: %v", err)
			}
		}
		t.Errorf("gpu declaration %q does not match what Sample reads (%v)", info.Telemetry.GPU, gpuReads)
	}
	if cpuReads := s.CPUTotalJiffies != 0; cpuReads != (info.Telemetry.CPUStats != "") {
		t.Errorf("cpu_stats declaration %q does not match what Sample reads (%v)", info.Telemetry.CPUStats, cpuReads)
	}
	if npuReads := s.NPUKnown; npuReads != (info.Telemetry.NPU != "") {
		if info.Telemetry.NPU != "" {
			if _, err := asusz13.ReadNPURuntimeActive(); err != nil {
				t.Skipf("npu declared but no amdxdna device here: %v", err)
			}
		}
		t.Errorf("npu declaration %q does not match what Sample reads (%v)", info.Telemetry.NPU, npuReads)
	}
	// The counters, not the derived rate — a single Sample legitimately
	// reports bytes and no MB/s, exactly the package-power split above. A
	// physical interface has moved *some* traffic on any machine that boots,
	// so a zero pair means the read did not happen.
	if netReads := s.NetRxBytes != 0 || s.NetTxBytes != 0; netReads != (info.Telemetry.Net != "") {
		if info.Telemetry.Net != "" {
			if _, _, err := asusz13.ReadNetBytes(); err != nil {
				t.Skipf("net declared but no physical interface here: %v", err)
			}
		}
		t.Errorf("net declaration %q does not match what Sample reads (%v)", info.Telemetry.Net, netReads)
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
		`"telemetry":{"power_draw":"rapl","gpu":"amdgpu","cpu_stats":"procfs","npu":"amdxdna","net":"procfs","history_seconds":3600}`,
		`"description":"Faster pixel response for the display (may cause ghosting)"`,
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

// TestToggleDescriptionsReachTheWire is the guard for the prose half of a
// generic toggle row.
//
// The drawer's two bespoke switches carried their descriptions as literals in
// GTK code, so a settings view rendering rows from the device document would
// have shown bare labels and silently dropped the warnings — the same class of
// loss as a capability declared with nothing reading it. The path under test is
// device TOML → registry → driver spec → wire; a description dropped at any of
// those four hops fails here, and nothing else in the suite would notice.
func TestToggleDescriptionsReachTheWire(t *testing.T) {
	info := deviceInfoFor(testDev)
	if len(info.Toggles) == 0 {
		t.Fatal("no toggles in the document")
	}
	for _, tg := range info.Toggles {
		if tg.Description == "" {
			t.Errorf("toggle %q has no description; a client rendering rows generically "+
				"can only show the label, so anything the label omits is lost", tg.ID)
		}
	}
}

// TestFanPresetsReachTheWire guards the same four-hop path as the toggle
// descriptions above — device TOML → registry → driver.FanShape → wire — for
// the preset curves.
//
// A preset dropped at any hop degrades quietly: the drawer shows no preset row
// and the CLI reports "this device declares no fan curve presets", both of
// which read as a device that never had any rather than as plumbing that lost
// them. TestDocumentMatchesTheDrawersFallback would also fail, but it compares
// whole Limits values and would say only that two large structs differ.
func TestFanPresetsReachTheWire(t *testing.T) {
	info := deviceInfoFor(testDev)
	if info.Fans == nil {
		t.Fatal("no fans section in the document")
	}
	if len(info.Fans.Presets) == 0 {
		t.Fatal("no fan presets on the wire, but the Z13 device file declares three")
	}
	for _, p := range info.Fans.Presets {
		if p.Name == "" || p.Label == "" {
			t.Errorf("preset %+v is missing a name or label; a client cannot render or apply it", p)
		}
		if p.Description == "" {
			t.Errorf("preset %q has no description — what a curve does to *this* machine is "+
				"device knowledge a client cannot derive", p.Name)
		}
		if got, want := len(p.Curve), info.Fans.Points; got != want {
			t.Errorf("preset %q crossed the wire with %d points, want the shape's %d", p.Name, got, want)
		}
	}
}

// The document must not hand out the device definition's own slices: it is
// built per request but the drawer caches it for the process lifetime, and a
// client that repaired a curve in place would be editing what every later
// caller reads. The outer slice being fresh is not enough — each preset owns a
// points slice of its own.
func TestFanPresetCurvesAreCopiedNotAliased(t *testing.T) {
	first := deviceInfoFor(testDev)
	if first.Fans == nil || len(first.Fans.Presets) == 0 {
		t.Fatal("no presets to check")
	}
	first.Fans.Presets[0].Curve[0].PWM = 199

	second := deviceInfoFor(testDev)
	if got := second.Fans.Presets[0].Curve[0].PWM; got == 199 {
		t.Error("mutating a served preset changed the device definition; the curve slice is aliased")
	}
}

// TestPanelOverdriveKeepsItsGhostingWarning pins the specific consequence that
// motivated the field. It is a fact about this panel, not UI copy: a client has
// no way to derive it, so if device data stops carrying it every client stops
// warning about it.
func TestPanelOverdriveKeepsItsGhostingWarning(t *testing.T) {
	for _, tg := range deviceInfoFor(testDev).Toggles {
		if tg.ID != "panel_overdrive" {
			continue
		}
		if !strings.Contains(strings.ToLower(tg.Description), "ghost") {
			t.Errorf("panel_overdrive description = %q, want the ghosting warning", tg.Description)
		}
		return
	}
	t.Fatal("panel_overdrive toggle missing from the document")
}

// fakeToggles is a driver.Toggles that answers from a map, so the get-state
// feature path can be exercised without touching real firmware attributes.
// Reading an id absent from values reports an error, which is how an
// unreadable attribute behaves.
type fakeToggles struct {
	specs  []driver.ToggleSpec
	values map[string]int
}

func (f fakeToggles) List() []driver.ToggleSpec { return f.specs }

func (f fakeToggles) Get(id string) (int, error) {
	v, ok := f.values[id]
	if !ok {
		return 0, driver.ErrUnsupported
	}
	return v, nil
}

func (f fakeToggles) Set(string, int) error { return nil }

// TestReadFeaturesOmitsWhatItCannotRead is the honesty rule one level down from
// the telemetry series: zero means "off", which is a claim about the hardware,
// and an attribute that could not be read supports no claim at all. A client
// showing a switch has to be able to tell "this is off" from "I do not know".
func TestReadFeaturesOmitsWhatItCannotRead(t *testing.T) {
	d := &Daemon{hw: &device.Device{Toggles: fakeToggles{
		specs: []driver.ToggleSpec{
			{ID: "boot_sound", Kind: driver.ToggleBool},
			{ID: "panel_overdrive", Kind: driver.ToggleBool},
			{ID: "unreadable", Kind: driver.ToggleBool},
		},
		values: map[string]int{"boot_sound": 1, "panel_overdrive": 0},
	}}}

	got := d.readFeatures()
	want := map[string]int{"boot_sound": 1, "panel_overdrive": 0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readFeatures() = %v, want %v", got, want)
	}
	if _, present := got["unreadable"]; present {
		t.Error("an unreadable toggle appeared in Features; absent and off must stay distinguishable")
	}
	// A toggle that reads 0 is present with the value 0 — that is a reading.
	if v, present := got["panel_overdrive"]; !present || v != 0 {
		t.Error("a toggle that reads off was dropped; 0 here is a measurement, not an absence")
	}
}

// TestReadFeaturesOnADeviceWithNoToggles returns nil rather than an empty map,
// so omitempty keeps the key off the wire entirely for such a device.
func TestReadFeaturesOnADeviceWithNoToggles(t *testing.T) {
	for name, d := range map[string]*Daemon{
		"no device":  {},
		"no toggles": {hw: &device.Device{}},
		"none declared": {hw: &device.Device{Toggles: fakeToggles{
			values: map[string]int{},
		}}},
		"none readable": {hw: &device.Device{Toggles: fakeToggles{
			specs:  []driver.ToggleSpec{{ID: "boot_sound", Kind: driver.ToggleBool}},
			values: map[string]int{},
		}}},
	} {
		if got := d.readFeatures(); got != nil {
			t.Errorf("%s: readFeatures() = %v, want nil so the key is omitted", name, got)
		}
	}
}

// TestEveryToggleDeclaresASource pins the "always populated" half of the wire
// contract. Source is substituted in deviceInfoFor rather than set by each
// driver, so a driver written before plugins existed still produces a document
// a client can group by without treating absence as a third case.
func TestEveryToggleDeclaresASource(t *testing.T) {
	for _, tg := range deviceInfoFor(testDev).Toggles {
		if tg.Source != api.ToggleSourceCore {
			t.Errorf("toggle %q source = %q, want %q — every toggle on this device "+
				"comes from a compiled-in driver", tg.ID, tg.Source, api.ToggleSourceCore)
		}
	}
}

// TestToggleSourceSurvivesADriverThatSetsIt: the substitution must fill a gap,
// never overwrite. A plugin-provided toggle names itself, and a daemon that
// stamped "core" over it would erase exactly the distinction the field exists
// for — the failure would be invisible until plugins shipped.
func TestToggleSourceSurvivesADriverThatSetsIt(t *testing.T) {
	const want = api.ToggleSourcePluginPrefix + "oxpec"
	d := &device.Device{Toggles: fakeToggles{
		specs: []driver.ToggleSpec{{ID: "fan_boost", Kind: driver.ToggleBool, Source: want}},
	}}
	got := deviceInfoFor(d).Toggles
	if len(got) != 1 {
		t.Fatalf("got %d toggles, want 1", len(got))
	}
	if got[0].Source != want {
		t.Errorf("source = %q, want %q — the core substitution overwrote a declared source",
			got[0].Source, want)
	}
}
