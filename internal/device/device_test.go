package device

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/cli"
	"github.com/dahui/z13ctl/internal/driver"
)

// The DMI strings of the machine this project started on, verbatim from
// /sys/class/dmi/id — the product carries the SKU twice by design.
const (
	z13Vendor  = "ASUSTeK COMPUTER INC."
	z13Product = "ROG Flow Z13 GZ302EA_GZ302EA"
)

func TestEmbeddedConfigsParseAndValidate(t *testing.T) {
	configs, err := Configs()
	if err != nil {
		t.Fatalf("Configs: %v", err)
	}
	if len(configs) == 0 {
		t.Fatal("no embedded device files")
	}
	seen := map[string]bool{}
	for _, c := range configs {
		if seen[c.Device.ID] {
			t.Errorf("duplicate device id %q", c.Device.ID)
		}
		seen[c.Device.ID] = true
	}
}

// TestZ13FileMatchesCliConstants is the bridge guard for the driver
// extraction: while internal/cli still carries the Z13's numbers as constants,
// the device file must agree with them exactly. When the constants are deleted
// and the file becomes authoritative, this test goes with them.
func TestZ13FileMatchesCliConstants(t *testing.T) {
	c := z13Config(t)

	env := c.Power.Envelope()
	if env.TDPMin != cli.TDPMin || env.TDPMaxSafe != cli.TDPMaxSafe || env.TDPMaxForced != cli.TDPMaxForced {
		t.Errorf("power limits %d/%d/%d, cli says %d/%d/%d",
			env.TDPMin, env.TDPMaxSafe, env.TDPMaxForced, cli.TDPMin, cli.TDPMaxSafe, cli.TDPMaxForced)
	}
	if c.Power.TDPDefault != cli.TDPDefault {
		t.Errorf("tdp_default %d, cli says %d", c.Power.TDPDefault, cli.TDPDefault)
	}

	floor := cli.HighTDPFanCurve()
	if len(env.FloorCurve) != len(floor) {
		t.Fatalf("floor_curve has %d points, cli.HighTDPFanCurve has %d", len(env.FloorCurve), len(floor))
	}
	for i := range floor {
		if env.FloorCurve[i] != floor[i] {
			t.Errorf("floor_curve[%d] = %+v, cli says %+v", i, env.FloorCurve[i], floor[i])
		}
	}

	if len(env.StockProfilePPT) != len(cli.StockProfilePPT) {
		t.Fatalf("stock_ppt has %d rows, cli has %d", len(env.StockProfilePPT), len(cli.StockProfilePPT))
	}
	for name, want := range cli.StockProfilePPT {
		if got := env.StockProfilePPT[name]; got != want {
			t.Errorf("stock_ppt.%s = %+v, cli says %+v", name, got, want)
		}
	}
}

func TestZ13MatchesItsOwnDMI(t *testing.T) {
	c := z13Config(t)
	if !c.matches(z13Vendor, z13Product) {
		t.Errorf("Z13 config does not match its own DMI (%q, %q)", z13Vendor, z13Product)
	}
	// Other GZ302 SKUs share the prefix and must match too.
	if !c.matches(z13Vendor, "ROG Flow Z13 GZ302XX_GZ302XX") {
		t.Error("Z13 config does not match a sibling GZ302 SKU")
	}
	for _, tt := range []struct{ vendor, product string }{
		{"LENOVO", z13Product},                   // right product, wrong vendor
		{z13Vendor, "ROG Ally RC71L"},            // right vendor, wrong product
		{z13Vendor, "GZ302EA ROG Flow Z13"},      // prefix must anchor at the start
		{strings.ToLower(z13Vendor), z13Product}, // DMI matching is exact-case
	} {
		if c.matches(tt.vendor, tt.product) {
			t.Errorf("Z13 config wrongly matches (%q, %q)", tt.vendor, tt.product)
		}
	}
}

func TestDetectAssemblesTheZ13(t *testing.T) {
	fakeDMI(t, z13Vendor, z13Product)
	registerFakeFactories(t)

	d, err := Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if d.ID != "asus-rog-flow-z13-2025" || d.Model != "GZ302" {
		t.Errorf("identity = %q/%q", d.ID, d.Model)
	}
	// Every capability the Z13 file declares must be present…
	if d.Fans == nil || d.Power == nil || d.Profiles == nil || d.Lighting == nil ||
		d.Toggles == nil || d.Battery == nil || d.Undervolt == nil ||
		d.Telemetry == nil || d.Buttons == nil {
		t.Errorf("assembled device has nil capabilities: %+v", d)
	}
	// …and the engine must be wired to the real envelope, not a zero value.
	if env := d.Power.Envelope(); env.TDPMaxSafe != cli.TDPMaxSafe {
		t.Errorf("engine envelope TDPMaxSafe = %d, want %d", env.TDPMaxSafe, cli.TDPMaxSafe)
	}
}

func TestDetectUnknownDeviceNamesTheMachine(t *testing.T) {
	fakeDMI(t, "FRAMEWORK", "Laptop 13")
	_, err := Detect()
	if err == nil {
		t.Fatal("unknown machine detected as something")
	}
	// The error must carry what a new device file needs: the DMI strings.
	for _, want := range []string{"FRAMEWORK", "Laptop 13"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not carry DMI string %q", err, want)
		}
	}
}

func TestAssembleRefusesAnUnregisteredMethod(t *testing.T) {
	c := z13Config(t) // fakes deliberately NOT registered
	_, err := Assemble(c)
	if err == nil {
		t.Fatal("assembly without factories succeeded")
	}
	if !strings.Contains(err.Error(), "not compiled into this binary") {
		t.Errorf("error %q does not name the missing driver", err)
	}
}

func TestValidateCatchesBrokenConfigs(t *testing.T) {
	base := func() Config {
		return Config{Device: Meta{ID: "x", Model: "X", Match: []Match{{Vendor: "V", Product: "P"}}}}
	}
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"missing id", func(c *Config) { c.Device.ID = "" }, "device.id"},
		{"no match patterns", func(c *Config) { c.Device.Match = nil }, "at least one pattern"},
		{"both product forms", func(c *Config) { c.Device.Match[0].ProductPrefix = "P" }, "exactly one"},
		{"floor without fans", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40,
				FloorCurve: [][]int{{40, 100}}}
		}, "requires a fans block"},
		{"floor temps out of order", func(c *Config) {
			c.Fans = &FansConfig{Method: "m", Points: 8, TempMin: 30, TempMax: 100}
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40,
				FloorCurve: [][]int{{50, 100}, {40, 200}}}
		}, "strictly increase"},
		{"stock table missing a profile", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40,
				StockPPT: map[string]StockRow{"quiet": {}}}
			c.Profiles = &ProfilesConfig{Method: "m", Names: []string{"quiet", "balanced"}}
		}, `missing profile "balanced"`},
		{"inverted power limits", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 50, TDPMaxSafe: 30, TDPMaxForced: 40}
		}, "tdp_min < tdp_max_safe"},
		{"undervolt bounds above zero", func(c *Config) {
			c.Undervolt = &UndervoltConfig{Method: "m", Min: -10, Max: 5}
		}, "min <= max <= 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mut(&c)
			err := c.Validate()
			if err == nil {
				t.Fatal("validation passed")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}

	if err := base().Validate(); err != nil {
		t.Errorf("minimal valid config rejected: %v", err)
	}
}

// --- test scaffolding ---

// z13Config returns the embedded Z13 config.
func z13Config(t *testing.T) Config {
	t.Helper()
	configs, err := Configs()
	if err != nil {
		t.Fatalf("Configs: %v", err)
	}
	for _, c := range configs {
		if c.Device.ID == "asus-rog-flow-z13-2025" {
			return c
		}
	}
	t.Fatal("Z13 device file not found")
	return Config{}
}

// fakeDMI points the DMI path vars at a temp tree for the test's duration.
func fakeDMI(t *testing.T, vendor, product string) {
	t.Helper()
	dir := t.TempDir()
	v, p := filepath.Join(dir, "sys_vendor"), filepath.Join(dir, "product_name")
	if err := os.WriteFile(v, []byte(vendor+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(product+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldV, oldP := dmiVendorPath, dmiProductPath
	dmiVendorPath, dmiProductPath = v, p
	t.Cleanup(func() { dmiVendorPath, dmiProductPath = oldV, oldP })
}

// registerFakeFactories fills every registry entry the Z13 file names with a
// stub, removing them again at cleanup so other tests see empty registries.
func registerFakeFactories(t *testing.T) {
	t.Helper()
	var z13Power fakePower
	fansFactories["asus-nb-wmi"] = func(FansConfig) (driver.FanController, error) { return fakeFans{}, nil }
	powerFactories["asus-nb-wmi-ppt"] = func(c PowerConfig) (driver.PowerLimiter, error) {
		z13Power = fakePower{env: c.Envelope()}
		return z13Power, nil
	}
	profilesFactories["platform-profile"] = func(ProfilesConfig) (driver.ProfileController, error) { return fakeProfiles{}, nil }
	lightingFactories["aura-hid"] = func(LightingConfig) (driver.Lighting, error) { return fakeLighting{}, nil }
	togglesFactories["asus-armoury"] = func(TogglesConfig) (driver.Toggles, error) { return fakeToggles{}, nil }
	batteryFactories["power-supply"] = func(BatteryConfig) (driver.Battery, error) { return fakeBattery{}, nil }
	undervoltFactories["ryzen-smu-co"] = func(UndervoltConfig) (driver.Undervolter, error) { return fakeUndervolt{}, nil }
	telemetryFactories["hwmon-rapl"] = func(TelemetryConfig) (driver.Telemetry, error) { return fakeTelemetry{}, nil }
	buttonsFactories["evdev-key"] = func(ButtonConfig) (driver.Buttons, error) { return fakeButtons{}, nil }
	t.Cleanup(func() {
		delete(fansFactories, "asus-nb-wmi")
		delete(powerFactories, "asus-nb-wmi-ppt")
		delete(profilesFactories, "platform-profile")
		delete(lightingFactories, "aura-hid")
		delete(togglesFactories, "asus-armoury")
		delete(batteryFactories, "power-supply")
		delete(undervoltFactories, "ryzen-smu-co")
		delete(telemetryFactories, "hwmon-rapl")
		delete(buttonsFactories, "evdev-key")
	})
	_ = z13Power
}

type fakeFans struct{}

func (fakeFans) Shape() driver.FanShape                  { return driver.FanShape{} }
func (fakeFans) ReadRPM() ([]int, error)                 { return nil, nil }
func (fakeFans) ReadMode() (int, error)                  { return 2, nil }
func (fakeFans) ApplyCurve([]api.FanCurvePoint) error    { return nil }
func (fakeFans) Release() error                          { return nil }
func (fakeFans) LiveCurve() ([]api.FanCurvePoint, error) { return nil, nil }

type fakePower struct{ env driver.PowerEnvelope }

func (p fakePower) Read() (api.TDPState, error)    { return api.TDPState{}, nil }
func (p fakePower) Apply(api.TDPState) error       { return nil }
func (p fakePower) Envelope() driver.PowerEnvelope { return p.env }

type fakeProfiles struct{}

func (fakeProfiles) Names() []string      { return []string{"quiet", "balanced", "performance"} }
func (fakeProfiles) Get() (string, error) { return "balanced", nil }
func (fakeProfiles) Set(string) error     { return nil }

type fakeLighting struct{}

func (fakeLighting) Zones() []string                       { return nil }
func (fakeLighting) Apply(string, api.LightingState) error { return nil }
func (fakeLighting) Off(string) error                      { return nil }
func (fakeLighting) Present() bool                         { return true }
func (fakeLighting) Reopen() error                         { return nil }

type fakeToggles struct{}

func (fakeToggles) List() []driver.ToggleSpec { return nil }
func (fakeToggles) Get(string) (int, error)   { return 0, nil }
func (fakeToggles) Set(string, int) error     { return nil }

type fakeBattery struct{}

func (fakeBattery) ChargeLimit() (int, error)             { return 100, nil }
func (fakeBattery) SetChargeLimit(int) error              { return nil }
func (fakeBattery) Status() (driver.BatteryStatus, error) { return driver.BatteryStatus{}, nil }

type fakeUndervolt struct{}

func (fakeUndervolt) ProbeAvailable() bool { return false }
func (fakeUndervolt) Range() (lo, hi int)  { return -40, 0 }
func (fakeUndervolt) Apply(int) error      { return nil }
func (fakeUndervolt) Reset() error         { return nil }

type fakeTelemetry struct{}

func (fakeTelemetry) Sample() (driver.Sample, error) { return driver.Sample{}, nil }

type fakeButtons struct{}

func (fakeButtons) Watch(context.Context, chan<- driver.ButtonEvent) error { return nil }
