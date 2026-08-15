package device

// config.go — the devices/*.toml schema and its validation. One file per
// device family; a capability block that is absent means the device does not
// have that capability, which is exactly what the daemon reports to clients.

import (
	"errors"
	"fmt"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// Config is one parsed devices/*.toml. Capability blocks are pointers so that
// absence is representable: nil means "this device does not have this", and
// the assembled Device carries a nil interface for it — discovery by absence,
// all the way from data file to socket client.
type Config struct {
	Device    Meta             `toml:"device"`
	Fans      *FansConfig      `toml:"fans"`
	Power     *PowerConfig     `toml:"power"`
	Profiles  *ProfilesConfig  `toml:"profiles"`
	Lighting  *LightingConfig  `toml:"lighting"`
	Toggles   *TogglesConfig   `toml:"toggles"`
	Battery   *BatteryConfig   `toml:"battery"`
	Undervolt *UndervoltConfig `toml:"undervolt"`
	CPU       *CPUConfig       `toml:"cpu"`
	Telemetry *TelemetryConfig `toml:"telemetry"`
	Button    *ButtonConfig    `toml:"button"`
}

// Meta identifies the device family and how to recognize it.
type Meta struct {
	// ID is the stable device identifier served to clients ("asus-rog-flow-
	// z13-2025"). Bug reports and logs key on it; never reuse or rename one.
	ID string `toml:"id"`

	// Model is the short hardware name for display ("GZ302").
	Model string `toml:"model"`

	// Match lists DMI patterns; any one matching claims the machine.
	Match []Match `toml:"match"`
}

// Match is one DMI pattern. Vendor must match exactly; the product matches
// either exactly (Product) or by prefix (ProductPrefix) — prefix is the common
// case, since vendors encode SKU suffixes into product_name ("ROG Flow Z13
// GZ302EA_GZ302EA").
type Match struct {
	Vendor        string `toml:"vendor"`
	Product       string `toml:"product"`
	ProductPrefix string `toml:"product_prefix"`
}

// FansConfig selects and parameterizes the fan driver.
type FansConfig struct {
	Method  string `toml:"method"`
	Points  int    `toml:"points"`
	TempMin int    `toml:"temp_min"` // curve editor axis, Celsius
	TempMax int    `toml:"temp_max"`
}

// Shape returns the driver.FanShape this config describes.
func (c FansConfig) Shape() driver.FanShape {
	return driver.FanShape{Points: c.Points, TempMin: c.TempMin, TempMax: c.TempMax, PWMMax: 255}
}

// StockRow is one profile's firmware PPT defaults, measured on hardware.
type StockRow struct {
	PL1      int `toml:"pl1"`
	PL2      int `toml:"pl2"`
	FPPT     int `toml:"fppt"`
	APU      int `toml:"apu"`
	Platform int `toml:"platform"`
}

// PowerConfig selects and parameterizes the power-limit driver.
type PowerConfig struct {
	Method       string              `toml:"method"`
	TDPMin       int                 `toml:"tdp_min"`
	TDPMaxSafe   int                 `toml:"tdp_max_safe"`
	TDPMaxForced int                 `toml:"tdp_max_forced"`
	TDPDefault   int                 `toml:"tdp_default"`
	FloorCurve   [][]int             `toml:"floor_curve"` // [[temp, pwm], ...]
	StockPPT     map[string]StockRow `toml:"stock_ppt"`
}

// Envelope returns the driver.PowerEnvelope this config describes.
func (c PowerConfig) Envelope() driver.PowerEnvelope {
	env := driver.PowerEnvelope{
		TDPMin:       c.TDPMin,
		TDPMaxSafe:   c.TDPMaxSafe,
		TDPMaxForced: c.TDPMaxForced,
	}
	if len(c.StockPPT) > 0 {
		env.StockProfilePPT = make(map[string]api.TDPState, len(c.StockPPT))
		for name, r := range c.StockPPT {
			env.StockProfilePPT[name] = api.TDPState{
				PL1SPL: r.PL1, PL2SPPT: r.PL2, FPPT: r.FPPT, APUSPPT: r.APU, PlatformSPPT: r.Platform,
			}
		}
	}
	for _, p := range c.FloorCurve {
		env.FloorCurve = append(env.FloorCurve, api.FanCurvePoint{Temp: p[0], PWM: p[1]})
	}
	return env
}

// ProfilesConfig selects the platform-profile driver and names the firmware
// profiles — which are also the reserved names no custom profile may take.
type ProfilesConfig struct {
	Method string   `toml:"method"`
	Names  []string `toml:"names"`
}

// LightingConfig selects the lighting driver.
type LightingConfig struct {
	Method string   `toml:"method"`
	Zones  []string `toml:"zones"`
}

// ToggleEntry is one firmware toggle the device offers.
//
// Description is optional prose shown beside the control, carrying anything the
// label cannot — most usefully a consequence ("may cause ghosting"). It lives in
// device data because it is a fact about the hardware, so every client gets the
// same warning without restating it.
type ToggleEntry struct {
	ID          string `toml:"id"`
	Label       string `toml:"label"`
	Description string `toml:"description"`
}

// TogglesConfig selects the firmware-toggles driver and lists its toggles.
type TogglesConfig struct {
	Method  string        `toml:"method"`
	Entries []ToggleEntry `toml:"entries"`
}

// BatteryConfig selects the battery driver and declares what it offers.
//
// ChargeLimit defaults to true when the block is present, because every
// battery driver written so far exists to control the charge threshold; a
// device that only reads a charge level says charge_limit = false.
type BatteryConfig struct {
	Method      string `toml:"method"`
	ChargeLimit *bool  `toml:"charge_limit"` // pointer: absent means the default, not false
	Health      bool   `toml:"health"`       // Status reports state of health
}

// Caps returns the battery capabilities this device declares.
func (c BatteryConfig) Caps() driver.BatteryCaps {
	limit := true
	if c.ChargeLimit != nil {
		limit = *c.ChargeLimit
	}
	return driver.BatteryCaps{ChargeLimit: limit, Health: c.Health}
}

// CPUConfig declares CPU-level controls. It is a section rather than a bool
// for the reason [battery] and [telemetry] are: its contents are independently
// absent, so a machine that offers boost control today and core parking
// tomorrow can say which without the block meaning two different things.
//
// Boost names the driver behind the boost switch ("cpufreq") and is empty when
// the device offers none — a block declaring nothing at all is a validation
// error, matching the empty-toggles rule: drop the block instead.
type CPUConfig struct {
	Boost string `toml:"boost"`
}

// UndervoltConfig selects the undervolt driver and its offset bounds.
type UndervoltConfig struct {
	Method string `toml:"method"`
	Min    int    `toml:"min"`
	Max    int    `toml:"max"`
}

// TelemetryConfig selects the telemetry driver and describes what it reports.
//
// PowerDraw names the package-power source and must be left unset until the
// driver actually reads one — see driver.TelemetryInfo. GPU, CPUStats, NPU
// and Net name the expanded sources on the same must-be-read terms.
// HistorySeconds sizes the daemon's sample ring and is what clients read as
// the largest history window worth asking for; unset means
// DefaultHistorySeconds.
type TelemetryConfig struct {
	Method         string `toml:"method"`
	PowerDraw      string `toml:"power_draw"`
	GPU            string `toml:"gpu"`
	CPUStats       string `toml:"cpu_stats"`
	NPU            string `toml:"npu"`
	Net            string `toml:"net"`
	HistorySeconds int    `toml:"history_seconds"`
}

// DefaultHistorySeconds is the sample history a device keeps when its data
// does not say: five minutes at the sampler's 1 Hz, which is the window the
// dashboard graphs were specified against.
const DefaultHistorySeconds = 300

// Info returns the telemetry description this device declares, with the
// history default applied. A negative history_seconds is treated as zero —
// "keep no history" — rather than propagating to a ring capacity, which is the
// one caller that would have to defend against it.
func (c TelemetryConfig) Info() driver.TelemetryInfo {
	secs := c.HistorySeconds
	switch {
	case secs == 0:
		secs = DefaultHistorySeconds
	case secs < 0:
		secs = 0
	}
	return driver.TelemetryInfo{
		PowerDraw:      c.PowerDraw,
		GPU:            c.GPU,
		CPUStats:       c.CPUStats,
		NPU:            c.NPU,
		Net:            c.Net,
		HistorySeconds: secs,
	}
}

// ButtonConfig selects the hardware-button driver.
type ButtonConfig struct {
	Method  string `toml:"method"`
	Device  string `toml:"device"`  // input device name to find by sysfs
	Keycode int    `toml:"keycode"` // key code to watch for
	Kind    string `toml:"kind"`    // driver.ButtonEvent.Kind delivered per press
}

// Validate reports everything wrong with a config at once, so a device data PR
// gets one round of feedback rather than one error per push. A valid config is
// one the registry can assemble without further checks.
func (c Config) Validate() error {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if c.Device.ID == "" {
		fail("device.id is required")
	}
	if c.Device.Model == "" {
		fail("device.model is required")
	}
	if len(c.Device.Match) == 0 {
		fail("device.match needs at least one pattern")
	}
	for i, m := range c.Device.Match {
		if m.Vendor == "" {
			fail("device.match[%d]: vendor is required", i)
		}
		if (m.Product == "") == (m.ProductPrefix == "") {
			fail("device.match[%d]: exactly one of product or product_prefix is required", i)
		}
	}

	if c.Fans != nil {
		if c.Fans.Method == "" {
			fail("fans.method is required")
		}
		if c.Fans.Points <= 0 {
			fail("fans.points must be positive")
		}
		// One degree per point, or a curve cannot hold strictly increasing
		// temperatures — the same bound the GUI's Sanitized enforces.
		if c.Fans.TempMax-c.Fans.TempMin < c.Fans.Points-1 {
			fail("fans temperature range %d–%d is too narrow for %d points", c.Fans.TempMin, c.Fans.TempMax, c.Fans.Points)
		}
	}

	if c.Power != nil {
		p := c.Power
		if p.Method == "" {
			fail("power.method is required")
		}
		if p.TDPMin <= 0 || p.TDPMin >= p.TDPMaxSafe || p.TDPMaxSafe > p.TDPMaxForced {
			fail("power limits must satisfy 0 < tdp_min < tdp_max_safe <= tdp_max_forced (got %d/%d/%d)",
				p.TDPMin, p.TDPMaxSafe, p.TDPMaxForced)
		}
		if p.TDPDefault != 0 && (p.TDPDefault < p.TDPMin || p.TDPDefault > p.TDPMaxSafe) {
			fail("power.tdp_default %d outside [%d, %d]", p.TDPDefault, p.TDPMin, p.TDPMaxSafe)
		}
		for i, pt := range p.FloorCurve {
			if len(pt) != 2 {
				fail("power.floor_curve[%d] must be a [temp, pwm] pair", i)
				continue
			}
			if pt[1] < 0 || pt[1] > 255 {
				fail("power.floor_curve[%d] PWM %d outside [0, 255]", i, pt[1])
			}
			if i > 0 && len(p.FloorCurve[i-1]) == 2 {
				if pt[0] <= p.FloorCurve[i-1][0] {
					fail("power.floor_curve temperatures must strictly increase (index %d)", i)
				}
				if pt[1] < p.FloorCurve[i-1][1] {
					fail("power.floor_curve PWMs must not decrease (index %d)", i)
				}
			}
		}
		// A floor with no fan control cannot be satisfied, only refused —
		// safety.Engine would deny every high-TDP request. Refuse the data
		// instead, at review time.
		if len(p.FloorCurve) > 0 && c.Fans == nil {
			fail("power.floor_curve requires a fans block: a floor without fan control cannot be enforced")
		}
		// The stock table is authoritative on write (the firmware does not
		// restore per-profile limits itself), so every selectable firmware
		// profile needs a row or switching to it leaves stale limits in force.
		if c.Profiles != nil {
			for _, name := range c.Profiles.Names {
				if _, ok := p.StockPPT[name]; !ok {
					fail("power.stock_ppt is missing profile %q", name)
				}
			}
		}
	}

	if c.Profiles != nil {
		if c.Profiles.Method == "" {
			fail("profiles.method is required")
		}
		if len(c.Profiles.Names) == 0 {
			fail("profiles.names must list the firmware profile names")
		}
	}
	if c.Lighting != nil {
		if c.Lighting.Method == "" {
			fail("lighting.method is required")
		}
		if len(c.Lighting.Zones) == 0 {
			fail("lighting.zones must name at least one zone")
		}
	}
	if c.Toggles != nil {
		if c.Toggles.Method == "" {
			fail("toggles.method is required")
		}
		if len(c.Toggles.Entries) == 0 {
			fail("toggles block with no entries; drop the block instead")
		}
		for i, e := range c.Toggles.Entries {
			if e.ID == "" || e.Label == "" {
				fail("toggles.entries[%d] needs both id and label", i)
			}
		}
	}
	if c.Battery != nil {
		if c.Battery.Method == "" {
			fail("battery.method is required")
		}
		// Same rule as an empty toggles block: a capability that offers nothing
		// still tells every client the controls exist. Say so by omission.
		if caps := c.Battery.Caps(); !caps.ChargeLimit && !caps.Health {
			fail("battery block offers neither charge_limit nor health; drop the block instead")
		}
	}
	if c.CPU != nil && c.CPU.Boost == "" {
		fail("[cpu] declares nothing; drop the block instead")
	}
	if c.Undervolt != nil {
		if c.Undervolt.Method == "" {
			fail("undervolt.method is required")
		}
		if c.Undervolt.Min > c.Undervolt.Max || c.Undervolt.Max > 0 {
			fail("undervolt bounds must satisfy min <= max <= 0 (got %d..%d)", c.Undervolt.Min, c.Undervolt.Max)
		}
	}
	if c.Telemetry != nil && c.Telemetry.Method == "" {
		fail("telemetry.method is required")
	}
	if c.Button != nil {
		if c.Button.Method == "" {
			fail("button.method is required")
		}
		if c.Button.Device == "" || c.Button.Keycode <= 0 {
			fail("button needs both device and a positive keycode")
		}
		if c.Button.Kind == "" {
			fail("button.kind is required — it names the event the daemon receives")
		}
	}

	return errors.Join(errs...)
}
