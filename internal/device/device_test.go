package device

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/cli"
	"github.com/dahui/voltaire/v2/internal/driver"
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

// The Z13 device file is the authoritative source of the Z13's numbers — the
// driver constants it once mirrored are deleted — so the data tests below are
// what stands between a typo in the TOML and hardware. They took over from the
// sanity tests that guarded the constants.

// TestZ13StockPPTSanity guards the stock table: every firmware profile must
// have a row and every value must sit inside the file's own envelope, since
// these are written to hardware verbatim on every stock-profile switch.
func TestZ13StockPPTSanity(t *testing.T) {
	c := z13Config(t)
	env := c.Power.Envelope()
	for _, profile := range c.Profiles.Names {
		stock, ok := env.StockProfilePPT[profile]
		if !ok {
			t.Errorf("stock_ppt is missing %q", profile)
			continue
		}
		for _, f := range []struct {
			name  string
			watts int
		}{
			{"PL1SPL", stock.PL1SPL},
			{"PL2SPPT", stock.PL2SPPT},
			{"FPPT", stock.FPPT},
			{"APUSPPT", stock.APUSPPT},
			{"PlatformSPPT", stock.PlatformSPPT},
		} {
			if f.watts < env.TDPMin || f.watts > env.TDPMaxForced {
				t.Errorf("%s.%s = %dW, out of range %d–%d", profile, f.name, f.watts, env.TDPMin, env.TDPMaxForced)
			}
		}
		if stock.PL2SPPT < stock.PL1SPL {
			t.Errorf("%s: PL2 %dW is below PL1 %dW; burst must not be under sustained",
				profile, stock.PL2SPPT, stock.PL1SPL)
		}
	}
}

// TestZ13StockTableNamesAreReserved pins the invariant the safety engine's
// ReadEffective relies on: any name absent from the stock table is treated as
// custom, with the stale-cache fallback disabled. Every key in the file's
// table must therefore be a name the reservation layers refuse for custom
// profiles — a new firmware profile added to the TOML without extending
// api.IsStockProfileName would misreport power limits the moment a custom
// profile took its name.
func TestZ13StockTableNamesAreReserved(t *testing.T) {
	env := z13Config(t).Power.Envelope()
	for name := range env.StockProfilePPT {
		if !api.IsStockProfileName(name) {
			t.Errorf("stock_ppt key %q is not reserved by api.IsStockProfileName", name)
		}
		if err := cli.ValidateProfileName(name); err == nil {
			t.Errorf("ValidateProfileName(%q) = nil, want an error — a custom profile may not shadow a stock table row", name)
		}
	}
}

// TestZ13FloorCurveIsWellFormed guards the floor curve the same way the old
// HighTDPFanCurve test guarded the constant: full length, respecting its own
// bottom, monotonic on both axes, and parseable by the same validator user
// curves go through — a floor the parser rejects could be written but never
// round-trip through state.
func TestZ13FloorCurveIsWellFormed(t *testing.T) {
	c := z13Config(t)
	curve := c.Power.Envelope().FloorCurve
	if len(curve) != c.Fans.Points {
		t.Fatalf("floor_curve has %d points, want the fans block's %d", len(curve), c.Fans.Points)
	}
	for i, p := range curve {
		if p.PWM < curve[0].PWM {
			t.Errorf("point %d PWM = %d, below the %d floor this curve exists to enforce", i+1, p.PWM, curve[0].PWM)
		}
		if i > 0 {
			if p.Temp <= curve[i-1].Temp {
				t.Errorf("point %d temp %d is not above point %d temp %d", i+1, p.Temp, i, curve[i-1].Temp)
			}
			if p.PWM < curve[i-1].PWM {
				t.Errorf("point %d PWM %d is below point %d PWM %d", i+1, p.PWM, i, curve[i-1].PWM)
			}
		}
	}
	if _, err := cli.ParseFanCurve(c.Fans.Shape(), formatCurve(curve)); err != nil {
		t.Errorf("floor_curve is rejected by cli.ParseFanCurve: %v", err)
	}
}

func formatCurve(points []api.FanCurvePoint) string {
	out := ""
	for i, p := range points {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%d:%d", p.Temp, p.PWM)
	}
	return out
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
	fileEnv := z13Config(t).Power.Envelope()
	if env := d.Power.Envelope(); env.TDPMaxSafe != fileEnv.TDPMaxSafe || env.TDPMaxSafe == 0 {
		t.Errorf("engine envelope TDPMaxSafe = %d, want the device file's %d (non-zero)", env.TDPMaxSafe, fileEnv.TDPMaxSafe)
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

func TestBatteryCapsDefaults(t *testing.T) {
	// charge_limit is a pointer precisely so "absent" and "false" differ: every
	// battery driver written so far exists to control the threshold, so a block
	// that says nothing means the usual thing, and a device that only reads a
	// level has to say so.
	yes, no := true, false
	for _, tc := range []struct {
		name string
		cfg  BatteryConfig
		want driver.BatteryCaps
	}{
		{"bare block", BatteryConfig{Method: "m"}, driver.BatteryCaps{ChargeLimit: true}},
		{"health declared", BatteryConfig{Method: "m", Health: true},
			driver.BatteryCaps{ChargeLimit: true, Health: true}},
		{"limit explicitly off", BatteryConfig{Method: "m", ChargeLimit: &no, Health: true},
			driver.BatteryCaps{Health: true}},
		{"limit explicitly on", BatteryConfig{Method: "m", ChargeLimit: &yes},
			driver.BatteryCaps{ChargeLimit: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Caps(); got != tc.want {
				t.Errorf("Caps() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTelemetryInfoHistoryDefault(t *testing.T) {
	// The default is applied here rather than at the ring, so the number the
	// document advertises and the number the ring is sized to are the same
	// value from the same place.
	for _, tc := range []struct {
		name string
		cfg  TelemetryConfig
		want driver.TelemetryInfo
	}{
		{"unset takes the default", TelemetryConfig{Method: "m"},
			driver.TelemetryInfo{HistorySeconds: DefaultHistorySeconds}},
		{"explicit value wins", TelemetryConfig{Method: "m", HistorySeconds: 60},
			driver.TelemetryInfo{HistorySeconds: 60}},
		// Negative would otherwise reach telemetryring.New as a capacity, and
		// "keep no history" is the only sane reading of it.
		{"negative means no history", TelemetryConfig{Method: "m", HistorySeconds: -5},
			driver.TelemetryInfo{HistorySeconds: 0}},
		{"power source is carried through", TelemetryConfig{Method: "m", PowerDraw: "rapl", HistorySeconds: 120},
			driver.TelemetryInfo{PowerDraw: "rapl", HistorySeconds: 120}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Info(); got != tc.want {
				t.Errorf("Info() = %+v, want %+v", got, tc.want)
			}
		})
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
		{"battery block offering nothing", func(c *Config) {
			no := false
			c.Battery = &BatteryConfig{Method: "m", ChargeLimit: &no}
		}, "drop the block instead"},
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
	cpuBoostFactories["cpufreq"] = func(CPUConfig) (driver.CPUBoost, error) { return fakeCPUBoost{}, nil }
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
		delete(cpuBoostFactories, "cpufreq")
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
func (fakeLighting) SetBrightness(string, int) error       { return nil }
func (fakeLighting) Present() bool                         { return true }
func (fakeLighting) Reopen() error                         { return nil }

type fakeToggles struct{}

func (fakeToggles) List() []driver.ToggleSpec { return nil }
func (fakeToggles) Get(string) (int, error)   { return 0, nil }
func (fakeToggles) Set(string, int) error     { return nil }

type fakeBattery struct{}

func (fakeBattery) Caps() driver.BatteryCaps              { return driver.BatteryCaps{ChargeLimit: true} }
func (fakeBattery) ChargeLimit() (int, error)             { return 100, nil }
func (fakeBattery) SetChargeLimit(int) error              { return nil }
func (fakeBattery) Status() (driver.BatteryStatus, error) { return driver.BatteryStatus{}, nil }

type fakeUndervolt struct{}

func (fakeUndervolt) Present() bool        { return false }
func (fakeUndervolt) ProbeAvailable() bool { return false }
func (fakeUndervolt) Range() (lo, hi int)  { return -40, 0 }
func (fakeUndervolt) Apply(int) error      { return nil }
func (fakeUndervolt) Reset() error         { return nil }

type fakeCPUBoost struct{}

func (fakeCPUBoost) Get() (bool, error) { return true, nil }
func (fakeCPUBoost) Set(bool) error     { return nil }

type fakeTelemetry struct{}

func (fakeTelemetry) Info() driver.TelemetryInfo     { return driver.TelemetryInfo{} }
func (fakeTelemetry) Sample() (driver.Sample, error) { return driver.Sample{}, nil }

type fakeButtons struct{}

func (fakeButtons) Watch(context.Context, chan<- driver.ButtonEvent) error { return nil }
