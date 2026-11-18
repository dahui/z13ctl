package device

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
// profiles. Config.Validate requires every profiles.names entry to be a kernel
// name, and the whole kernel vocabulary is reserved, so this holds for any
// device file that validates.
func TestZ13StockTableNamesAreReserved(t *testing.T) {
	env := z13Config(t).Power.Envelope()
	for name := range env.StockProfilePPT {
		if !api.IsReservedProfileName(name) {
			t.Errorf("stock_ppt key %q is not reserved by api.IsReservedProfileName", name)
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
// The hold is what stops the Z13's sleep/wake cycling (issue #24); losing it from
// the file, or from Shape, silently brings the cycling back.
// TestZ13DeclaresItsBootCache pins the measured asus-nb-wmi boot cache in the
// data, now that the driver no longer assumes it: without it a 5 W readback
// after boot would be trusted as a limit in force on the fallback interface.
func TestZ13DeclaresItsBootCache(t *testing.T) {
	c := z13Config(t)
	if got := c.Power.BootCachePL1; got != 5 {
		t.Fatalf("power.boot_cache_pl1 = %d, want the measured 5", got)
	}
	if got := c.Power.Envelope().Initial; got != (api.TDPState{PL1SPL: 5}) {
		t.Errorf("envelope Initial = %+v, want PL1 at 5 W", got)
	}
}

func TestZ13CarriesTheSleepReleaseSettle(t *testing.T) {
	if got := z13Config(t).Fans.Shape().SleepReleaseSettle; got != 3*time.Second {
		t.Errorf("SleepReleaseSettle = %v, want the measured 3s", got)
	}
}

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
		{"bare block", BatteryConfig{Method: "m"}, driver.BatteryCaps{ChargeLimit: true,
			ChargeLimitMin: DefaultChargeLimitMin, ChargeLimitMax: DefaultChargeLimitMax}},
		{"health declared", BatteryConfig{Method: "m", Health: true},
			driver.BatteryCaps{ChargeLimit: true, Health: true,
				ChargeLimitMin: DefaultChargeLimitMin, ChargeLimitMax: DefaultChargeLimitMax}},
		{"limit explicitly off", BatteryConfig{Method: "m", ChargeLimit: &no, Health: true},
			driver.BatteryCaps{Health: true}},
		{"limit explicitly on", BatteryConfig{Method: "m", ChargeLimit: &yes},
			driver.BatteryCaps{ChargeLimit: true,
				ChargeLimitMin: DefaultChargeLimitMin, ChargeLimitMax: DefaultChargeLimitMax}},
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
		{"power limit with an unknown id", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40,
				Limits: []PowerLimitConfig{{ID: "pl1"}, {ID: "pl4"}}}
		}, `id "pl4"`},
		{"power limit listed twice", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40,
				Limits: []PowerLimitConfig{{ID: "pl1"}, {ID: "pl1"}}}
		}, "twice"},
		{"power limits without pl1", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40,
				Limits: []PowerLimitConfig{{ID: "pl2"}}}
		}, "must include pl1"},
		{"boot cache above the forced maximum", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 5, TDPMaxSafe: 30, TDPMaxForced: 40, BootCachePL1: 41}
		}, "boot_cache_pl1"},
		{"inverted power limits", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 50, TDPMaxSafe: 30, TDPMaxForced: 40}
		}, "tdp_min < tdp_max_safe"},
		{"undervolt bounds above zero", func(c *Config) {
			c.Undervolt = &UndervoltConfig{Method: "m", Min: -10, Max: 5}
		}, "min <= max <= 0"},
		{"negative sleep release settle", func(c *Config) {
			c.Fans = &FansConfig{Method: "m", Points: 8, TempMin: 30, TempMax: 100, SleepReleaseSettleMs: -1}
		}, "sleep_release_settle_ms"},
		{"sleep release settle past logind's budget", func(c *Config) {
			c.Fans = &FansConfig{Method: "m", Points: 8, TempMin: 30, TempMax: 100, SleepReleaseSettleMs: 5000}
		}, "under 5000"},
		{"battery block offering nothing", func(c *Config) {
			no := false
			c.Battery = &BatteryConfig{Method: "m", ChargeLimit: &no}
		}, "drop the block instead"},
		{"charge limit range inverted", func(c *Config) {
			c.Battery = &BatteryConfig{Method: "m", ChargeLimitMin: 90, ChargeLimitMax: 60}
		}, "1 <= min < max <= 100"},
		{"charge limit max past 100", func(c *Config) {
			c.Battery = &BatteryConfig{Method: "m", ChargeLimitMax: 110}
		}, "1 <= min < max <= 100"},
		{"charge limit range with no charge limit", func(c *Config) {
			no := false
			c.Battery = &BatteryConfig{Method: "m", ChargeLimit: &no, Health: true, ChargeLimitMin: 50}
		}, "charge_limit = false"},
		{"lighting zone with no interface", func(c *Config) {
			c.Lighting = &LightingConfig{Method: "m", Zones: []LightingZoneConfig{{Name: "kbd", Label: "Keyboard", USB: "0b05:1a30"}}}
		}, "needs interface"},
		{"button with no id", func(c *Config) {
			c.Buttons = []ButtonConfig{{Label: "B", Method: "evdev-key", Device: "d", Keycode: 1}}
		}, "id is required"},
		{"button id with capitals", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "Armoury", Label: "B", Method: "evdev-key", Device: "d", Keycode: 1}}
		}, "lowercase"},
		{"button id twice", func(c *Config) {
			b := ButtonConfig{ID: "a", Label: "B", Method: "evdev-key", Device: "d", Keycode: 1}
			c.Buttons = []ButtonConfig{b, b}
		}, "duplicate id"},
		{"button with no label", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Method: "evdev-key", Device: "d", Keycode: 1}}
		}, "label is required"},
		{"evdev-key with no keycode", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-key", Device: "d"}}
		}, "positive keycode"},
		{"chord of one key", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-chord", Device: "d", Keys: []int{29}}}
		}, "at least two distinct"},
		{"chord naming a key twice", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-chord", Device: "d", Keys: []int{29, 29}}}
		}, "at least two distinct"},
		{"two default buttons", func(c *Config) {
			c.Buttons = []ButtonConfig{
				{ID: "a", Label: "A", Default: true, Method: "evdev-key", Device: "d", Keycode: 1},
				{ID: "b", Label: "B", Default: true, Method: "evdev-key", Device: "d", Keycode: 2},
			}
		}, "at most one button"},
		{"hidraw report with no match", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "hidraw-report", USB: "1a86:fe00", Interface: intp(2)}}
		}, "needs match"},
		{"hidraw match entry that is not a pair", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "hidraw-report", USB: "1a86:fe00", Interface: intp(2),
				Match: [][]int{{6, 0x24, 1}}}}
		}, "[offset, value]"},
		{"hidraw match offset past a report", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "hidraw-report", USB: "1a86:fe00", Interface: intp(2),
				Match: [][]int{{64, 1}}}}
		}, "[offset, value]"},
		{"hidraw match value past a byte", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "hidraw-report", USB: "1a86:fe00", Interface: intp(2),
				Match: [][]int{{6, 256}}}}
		}, "[offset, value]"},
		{"evdev usb with no interface", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-chord", Device: "d", Keys: []int{29, 125}, USB: "1a86:fe00"}}
		}, "needs interface too"},
		{"evdev interface with no usb", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-key", Device: "d", Keycode: 1, Interface: intp(0)}}
		}, "vvvv:pppp"},
		{"inputplumber capability that is not a button name", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-key", Device: "d", Keycode: 1, InputPlumber: "Gamepad:Button:Guide"}}
		}, "gamepad button name"},
		{"battery cap above the forced maximum", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85, TDPMaxBattery: 90}
		}, "tdp_max_battery"},
		{"battery row for a profile with no AC row", func(c *Config) {
			c.Power = &PowerConfig{Method: "m", TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85,
				StockPPT:        map[string]StockRow{"balanced": {PL1: 80}},
				StockPPTBattery: map[string]StockRow{"balanced": {PL1: 50}, "quiet": {PL1: 30}}}
		}, "stock_ppt_battery has a row"},
		{"amd-smu with two stock rows", func(c *Config) {
			c.Power = &PowerConfig{Method: "amd-smu", TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85,
				StockPPT: map[string]StockRow{"balanced": {PL1: 80}, "performance": {PL1: 85}}}
		}, "exactly one stock_ppt row"},
		{"profile pin not offered", func(c *Config) {
			c.Profiles = &ProfilesConfig{Method: "m", Names: []string{"balanced"}, Pin: "low-power"}
		}, "profiles.pin"},
		{"profile pin with no power block", func(c *Config) {
			c.Power = nil
			c.Profiles = &ProfilesConfig{Method: "m", Names: []string{"balanced"}, Pin: "balanced"}
		}, "profiles.pin needs a power block"},
		{"pinned profile with no stock row", func(c *Config) {
			c.Profiles = &ProfilesConfig{Method: "m", Names: []string{"balanced", "low-power"}, Default: "balanced", Pin: "low-power"}
			c.Power = &PowerConfig{Method: "m", TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85,
				StockPPT: map[string]StockRow{"balanced": {PL1: 80}}}
		}, `stock_ppt is missing profile "low-power"`},
		{"keyboard node on a non-evdev method", func(c *Config) {
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "hidraw-report", KeyboardNode: "why"}}
		}, "only to evdev methods"},
		{"keyboard node with a glob in the DMI match", func(c *Config) {
			c.Device.Match = []Match{{Vendor: "V", ProductPrefix: "X2*"}}
			c.Buttons = []ButtonConfig{{ID: "a", Label: "B", Method: "evdev-key", Device: "d", Keycode: 1, KeyboardNode: "why"}}
		}, "glob"},
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

func (fakeProfiles) Names() []string       { return []string{"quiet", "balanced", "performance"} }
func (fakeProfiles) Get() (string, error)  { return "balanced", nil }
func (fakeProfiles) Set(string) error      { return nil }
func (fakeProfiles) Default() string       { return "balanced" }
func (fakeProfiles) Label(n string) string { return n }

type fakeLighting struct{}

func (fakeLighting) Zones() []string                       { return nil }
func (fakeLighting) Apply(string, api.LightingState) error { return nil }
func (fakeLighting) Off(string) error                      { return nil }
func (fakeLighting) SetBrightness(string, int) error       { return nil }
func (fakeLighting) PresentZones() []string                { return nil }
func (fakeLighting) Caps() driver.LightingCaps             { return driver.LightingCaps{} }
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
func (fakeUndervolt) Available() bool      { return false }
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

// pressButton delivers one press for id, then waits for cancellation.
type pressButton string

func (p pressButton) Watch(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	ch <- driver.ButtonEvent{ID: string(p)}
	<-ctx.Done()
	return nil
}

// TestEveryDeclaredButtonIsWatched: a device with several buttons still gives
// the daemon one watcher, and a press on any of them arrives carrying its id.
func TestEveryDeclaredButtonIsWatched(t *testing.T) {
	buttonsFactories["test-press"] = func(c ButtonConfig) (driver.Buttons, error) { return pressButton(c.ID), nil }
	t.Cleanup(func() { delete(buttonsFactories, "test-press") })
	c := Config{
		Device: Meta{ID: "x", Model: "X", Match: []Match{{Vendor: "V", Product: "P"}}},
		Buttons: []ButtonConfig{
			{ID: "home", Label: "Home button", Method: "test-press"},
			{ID: "oxp", Label: "OneXPlayer button", Default: true, Method: "test-press"},
		},
	}
	d, err := Assemble(c)
	if err != nil {
		t.Fatal(err)
	}
	if d.DefaultButton != "oxp" || d.ButtonLabel != "OneXPlayer button" {
		t.Errorf("default = %q (%q), want oxp (OneXPlayer button)", d.DefaultButton, d.ButtonLabel)
	}
	if len(d.ButtonList) != 2 || d.ButtonList[0].ID != "home" || d.ButtonList[1].ID != "oxp" {
		t.Errorf("ButtonList = %+v, want home then oxp", d.ButtonList)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan driver.ButtonEvent, 2)
	done := make(chan struct{})
	go func() { _ = d.Buttons.Watch(ctx, ch); close(done) }()
	got := map[string]bool{}
	for range 2 {
		got[(<-ch).ID] = true
	}
	cancel()
	<-done
	if !got["home"] || !got["oxp"] {
		t.Errorf("presses seen = %v, want both buttons", got)
	}
}

// TestBatteryChargeLimitRange pins where the range comes from: the device data
// when it declares one, the pre-existing 40–100 when it does not, and nothing
// at all when there is no charge limit to bound.
func TestBatteryChargeLimitRange(t *testing.T) {
	no := false
	tests := []struct {
		name     string
		cfg      BatteryConfig
		min, max int
	}{
		{"undeclared takes the defaults", BatteryConfig{Method: "m"}, DefaultChargeLimitMin, DefaultChargeLimitMax},
		{"declared wins", BatteryConfig{Method: "m", ChargeLimitMin: 50, ChargeLimitMax: 90}, 50, 90},
		{"one side declared", BatteryConfig{Method: "m", ChargeLimitMin: 60}, 60, DefaultChargeLimitMax},
		{"no charge limit, no range", BatteryConfig{Method: "m", ChargeLimit: &no, Health: true}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := tt.cfg.Caps()
			if caps.ChargeLimitMin != tt.min || caps.ChargeLimitMax != tt.max {
				t.Errorf("range = %d–%d, want %d–%d", caps.ChargeLimitMin, caps.ChargeLimitMax, tt.min, tt.max)
			}
		})
	}

	z13 := z13Config(t)
	if caps := z13.Battery.Caps(); caps.ChargeLimitMin != 40 || caps.ChargeLimitMax != 100 {
		t.Errorf("Z13 range = %d–%d, want the declared 40–100", caps.ChargeLimitMin, caps.ChargeLimitMax)
	}
}

// TestParseConfigRejectsUnknownKeys pins the strictness hand-written device
// data needs: a misspelled key is an error naming the key, never a silent
// default.
func TestParseConfigRejectsUnknownKeys(t *testing.T) {
	raw := []byte(`
[device]
id = "x"
model = "X"
[[device.match]]
vendor = "V"
product = "P"
[power]
method = "m"
tdp_min = 5
tdp_max_sfae = 30
tdp_max_forced = 40
`)
	_, err := ParseConfig(raw)
	if err == nil || !strings.Contains(err.Error(), "power.tdp_max_sfae") {
		t.Fatalf("ParseConfig = %v, want an error naming power.tdp_max_sfae", err)
	}
	fixed := []byte(strings.Replace(string(raw), "tdp_max_sfae", "tdp_max_safe", 1))
	if _, err := ParseConfig(fixed); err != nil {
		t.Errorf("the corrected file: %v", err)
	}
}

// intp is a pointer to v, for the optional interface fields.
func intp(v int) *int { return &v }

// pinnedTestConfig is the OXP's shape: three firmware profiles, one pinned,
// and a power method whose factory the test supplies.
func pinnedTestConfig() Config {
	return Config{
		Device: Meta{ID: "pinned", Model: "P", Name: "Pinned", Match: []Match{{Vendor: "V", Product: "P"}}},
		Power: &PowerConfig{Method: "test-power", TDPMin: 10, TDPMaxSafe: 85, TDPMaxForced: 85,
			StockPPT: map[string]StockRow{"balanced": {PL1: 80, PL2: 85, FPPT: 100}}},
		Profiles: &ProfilesConfig{Method: "test-profiles", Names: []string{"low-power", "balanced", "performance"},
			Default: "balanced", Pin: "balanced"},
	}
}

// assemblePinned assembles pinnedTestConfig with a power factory answering
// powerErr, and returns the device and the names the profile driver was given.
func assemblePinned(t *testing.T, powerErr error) (d *Device, offered []string) {
	t.Helper()
	powerFactories["test-power"] = func(c PowerConfig) (driver.PowerLimiter, error) {
		if powerErr != nil {
			return nil, powerErr
		}
		return fakePower{env: c.Envelope()}, nil
	}
	profilesFactories["test-profiles"] = func(c ProfilesConfig) (driver.ProfileController, error) {
		offered = c.Names
		return fakeProfiles{}, nil
	}
	t.Cleanup(func() {
		delete(powerFactories, "test-power")
		delete(profilesFactories, "test-profiles")
	})
	c := pinnedTestConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("the pinned shape must validate with one stock row: %v", err)
	}
	d, err := Assemble(c)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return d, offered
}

// TestPinHoldsWhileThePowerControlExists: with the power method available,
// the pin is the only profile offered and is held.
func TestPinHoldsWhileThePowerControlExists(t *testing.T) {
	d, offered := assemblePinned(t, nil)
	if d.Power == nil || d.ProfilePin != "balanced" {
		t.Fatalf("Power = %v, ProfilePin = %q; want power and the pin", d.Power, d.ProfilePin)
	}
	if fmt.Sprint(offered) != "[balanced]" {
		t.Errorf("profile driver offered %v, want only the pin", offered)
	}
	if len(d.Unavailable) != 0 {
		t.Errorf("Unavailable = %v, want none", d.Unavailable)
	}
}

// TestUnavailablePowerDropsThePin: with ryzen_smu missing the OXP has no TDP
// control, so the firmware profiles are its only power control: the pin goes,
// every listed profile is offered, and the reason is recorded for clients.
func TestUnavailablePowerDropsThePin(t *testing.T) {
	d, offered := assemblePinned(t, &UnavailableError{Reason: "needs ryzen_smu"})
	if d.Power != nil {
		t.Fatal("an unavailable power method assembled a power capability")
	}
	if d.ProfilePin != "" || d.PowerSlider {
		t.Errorf("ProfilePin = %q, PowerSlider = %v; want neither without power", d.ProfilePin, d.PowerSlider)
	}
	if fmt.Sprint(offered) != "[low-power balanced performance]" {
		t.Errorf("profile driver offered %v, want every listed profile", offered)
	}
	if got := d.Missing("power", "none"); got != "needs ryzen_smu" {
		t.Errorf("Missing(power) = %q, want the factory's reason", got)
	}
	if got := d.Missing("fans", "no fans"); got != "no fans" {
		t.Errorf("Missing(fans) = %q, want the fallback", got)
	}
}

// TestPowerFactoryFailureStillFailsTheDevice: only UnavailableError means
// "not installed"; any other error is a broken device, as before.
func TestPowerFactoryFailureStillFailsTheDevice(t *testing.T) {
	powerFactories["test-power"] = func(PowerConfig) (driver.PowerLimiter, error) { return nil, fmt.Errorf("broken") }
	profilesFactories["test-profiles"] = func(ProfilesConfig) (driver.ProfileController, error) { return fakeProfiles{}, nil }
	t.Cleanup(func() {
		delete(powerFactories, "test-power")
		delete(profilesFactories, "test-profiles")
	})
	if _, err := Assemble(pinnedTestConfig()); err == nil {
		t.Fatal("a failing power factory assembled a device")
	}
}

// TestUnavailableUndervoltIsRecorded: the undervolt factory reports a missing
// out-of-tree module the way the power factory does, so the device carries no
// undervolt capability and says why; any other factory error still fails it.
func TestUnavailableUndervoltIsRecorded(t *testing.T) {
	c := Config{
		Device:    Meta{ID: "uv", Model: "U", Name: "UV", Match: []Match{{Vendor: "V", Product: "U"}}},
		Undervolt: &UndervoltConfig{Method: "test-uv", Min: -40, Max: 0},
	}
	t.Cleanup(func() { delete(undervoltFactories, "test-uv") })

	undervoltFactories["test-uv"] = func(UndervoltConfig) (driver.Undervolter, error) {
		return nil, &UnavailableError{Reason: "needs ryzen_smu"}
	}
	d, err := Assemble(c)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if d.Undervolt != nil {
		t.Error("an unavailable undervolt method assembled an undervolt capability")
	}
	if got := d.Missing("undervolt", "none"); got != "needs ryzen_smu" {
		t.Errorf("Missing(undervolt) = %q, want the factory's reason", got)
	}

	undervoltFactories["test-uv"] = func(UndervoltConfig) (driver.Undervolter, error) { return nil, fmt.Errorf("broken") }
	if _, err := Assemble(c); err == nil {
		t.Error("a failing undervolt factory assembled a device")
	}
}
