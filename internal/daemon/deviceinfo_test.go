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
	"strings"
	"testing"

	"github.com/dahui/voltaire/v2/internal/device"
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
	if !info.Battery || !info.Telemetry || !info.Buttons {
		t.Errorf("presence flags = battery:%v telemetry:%v buttons:%v, want all true on the Z13",
			info.Battery, info.Telemetry, info.Buttons)
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
