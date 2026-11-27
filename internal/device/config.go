package device

// config.go — the devices/*.toml schema and its validation. One file per
// device family; a capability block that is absent means the device does not
// have that capability, which is exactly what the daemon reports to clients.

import (
	"errors"
	"fmt"
	"github.com/dahui/voltaire/v2/internal/safety"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

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
	Buttons   []ButtonConfig   `toml:"buttons"`
	Quickbar  *QuickbarConfig  `toml:"quickbar"`
	Rumble    *RumbleConfig    `toml:"rumble"`
}

// RumbleConfig selects the game controller's vibration-strength driver.
// Method "hid-oxp" is the kernel's OneXPlayer controller driver, whose HID
// device carries rumble_intensity (0–5 on the X2, from rumble_intensity_range);
// USB names the controller ("vvvv:pppp"), so a second hid-oxp device is never
// the one written.
type RumbleConfig struct {
	Method string `toml:"method"`
	USB    string `toml:"usb"`
}

// QuickbarConfig is the drawer's default contents on this device: control IDs
// (the GUI's internal/controls registry; the same names gui.toml's [quickbar]
// controls takes), in order. Absent: the GUI's built-in default. A user's own
// choice (the Settings page's Quickbar card, or gui.toml) still wins; this only
// says what a drawer nobody has customized holds. A handheld wants different
// quick controls from a laptop (the OXP: CPU boost and the refresh rate are
// worth a place where the Z13's drawer has the charge limit and RGB).
type QuickbarConfig struct {
	Controls []string `toml:"controls"`
}

// Meta identifies the device family and how to recognize it.
type Meta struct {
	// ID is the stable device identifier served to clients ("asus-rog-flow-
	// z13-2025"). Bug reports and logs key on it; never reuse or rename one.
	ID string `toml:"id"`

	// Model is the short hardware name for display ("GZ302").
	Model string `toml:"model"`

	// Name is the machine's display name ("ASUS ROG Flow Z13 (2025)"), for
	// help text and the capability API. Optional; clients fall back to Model.
	Name string `toml:"name"`

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

	// SleepReleaseSettleMs holds a suspend this long after the daemon's pre-sleep
	// fan release; see driver.FanShape.SleepReleaseSettle. 0, the default, is no
	// hold: only a machine where the early wake was measured carries one.
	SleepReleaseSettleMs int `toml:"sleep_release_settle_ms"`

	// Labels names the fans that report a speed, in the kernel's channel
	// order. Optional: a fan with no entry takes the kernel's own label, else
	// "Fan N". Worth setting where the kernel's label is wrong for the machine.
	Labels []string `toml:"labels"`

	// RPMMax is the fans' measured top speed, a chart hint. Optional: hwmon
	// has no fan*_max here, so only a measurement can supply it.
	RPMMax int `toml:"rpm_max"`

	Presets []FanPresetConfig `toml:"presets"`
}

// FanPresetConfig is one named starting-point curve, as [[fans.presets]].
//
// Curve is [[temp, pwm], ...] with exactly the fans block's Points entries, and
// must satisfy the same rules a user's curve does — strictly increasing
// temperatures, non-decreasing PWMs — because applying a preset *is* an ordinary
// curve write. Validate enforces that here, at load, since device data is
// compiled in: a malformed preset is a build defect, not a runtime error.
type FanPresetConfig struct {
	Name        string  `toml:"name"`
	Label       string  `toml:"label"`
	Description string  `toml:"description"`
	Curve       [][]int `toml:"curve"`
}

// Shape returns the driver.FanShape this config describes.
func (c FansConfig) Shape() driver.FanShape {
	s := driver.FanShape{Points: c.Points, TempMin: c.TempMin, TempMax: c.TempMax, PWMMax: 255,
		SleepReleaseSettle: time.Duration(c.SleepReleaseSettleMs) * time.Millisecond,
		Labels:             slices.Clone(c.Labels),
		RPMMax:             c.RPMMax}
	for _, p := range c.Presets {
		preset := api.FanPreset{Name: p.Name, Label: p.Label, Description: p.Description}
		for _, pt := range p.Curve {
			if len(pt) == 2 {
				preset.Curve = append(preset.Curve, api.FanCurvePoint{Temp: pt[0], PWM: pt[1]})
			}
		}
		s.Presets = append(s.Presets, preset)
	}
	return s
}

// FanPreset returns the preset with the given name, matched case-insensitively
// as profile names are, and whether one exists.
func (c FansConfig) FanPreset(name string) (api.FanPreset, bool) {
	for _, p := range c.Shape().Presets {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return api.FanPreset{}, false
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
	Method       string  `toml:"method"`
	TDPMin       int     `toml:"tdp_min"`
	TDPMaxSafe   int     `toml:"tdp_max_safe"`
	TDPMaxForced int     `toml:"tdp_max_forced"`
	TDPDefault   int     `toml:"tdp_default"`
	FloorCurve   [][]int `toml:"floor_curve"` // [[temp, pwm], ...]
	// BootCachePL1 is the PL1 value the power-limit attributes hold after boot,
	// before anything writes them (5 on the Z13's asus-nb-wmi attributes) — a
	// readback still equal to it says nothing about the limits in force.
	// Optional: 0 leaves safety.CacheStale's own fallback (PL1 at tdp_min), and
	// an interface that reports its defaults (asus-armoury) supplies its own.
	BootCachePL1 int `toml:"boot_cache_pl1"`

	// YieldInGamingMode leaves the power limits to Steam while a gamescope
	// session runs: on a handheld whose SteamOS configuration has
	// steamos-manager write the same limits (Steam's TDP slider), both
	// writing is a fight. The capability is then unavailable with that
	// reason, as for a missing module; the desktop session keeps it.
	YieldInGamingMode bool `toml:"yield_in_gaming_mode"`
	// Limits ([[power.limits]]) narrows and names the settable limits: which of
	// pl1/pl2/pl3 to offer, in order, and their wording. Optional — without it
	// the power driver offers every limit the kernel exposes, in its own words;
	// with it, a listed limit the kernel lacks is still dropped, and an entry
	// with no label takes the driver's.
	Limits   []PowerLimitConfig  `toml:"limits"`
	StockPPT map[string]StockRow `toml:"stock_ppt"`

	// TDPMaxBattery caps the sustained limit while on battery (0: no separate
	// cap). StockPPTBattery is the firmware's own rows on battery, where they
	// differ from StockPPT (the OXP's EC presets: 80/100/85 on AC, 50/60/50 on
	// battery). Together they make EnvelopeBattery.
	TDPMaxBattery   int                 `toml:"tdp_max_battery"`
	StockPPTBattery map[string]StockRow `toml:"stock_ppt_battery"`

	// Slider selects the one-slider model (OneXConsole's): setting a TDP while
	// no custom profile is active applies and remembers it as a device setting
	// rather than creating the custom profile, and the daemon re-applies the
	// remembered value at boot, on resume and after a charger edge. Named
	// custom profiles still hold a TDP of their own.
	Slider bool `toml:"slider"`
}

// PowerLimitConfig is one [[power.limits]] entry.
type PowerLimitConfig struct {
	ID          string `toml:"id"` // "pl1", "pl2" or "pl3"
	Label       string `toml:"label"`
	Description string `toml:"description"`
}

// PowerLimitIDs are the limits a power envelope can describe, in their
// natural order: the tdp command's pl1/pl2/pl3 fields.
var PowerLimitIDs = []string{"pl1", "pl2", "pl3"}

// Envelope returns the driver.PowerEnvelope this config describes.
func (c PowerConfig) Envelope() driver.PowerEnvelope {
	env := driver.PowerEnvelope{
		TDPMin:       c.TDPMin,
		TDPMaxSafe:   c.TDPMaxSafe,
		TDPMaxForced: c.TDPMaxForced,
	}
	if c.BootCachePL1 > 0 {
		env.Initial = api.TDPState{PL1SPL: c.BootCachePL1}
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
	for _, l := range c.Limits {
		env.Limits = append(env.Limits, driver.PowerLimitSpec{ID: l.ID, Label: l.Label, Description: l.Description})
	}
	return env
}

// EnvelopeBattery is the envelope while on battery: the sustained limits
// capped at TDPMaxBattery, and StockPPTBattery's rows where it has them.
func (c PowerConfig) EnvelopeBattery() driver.PowerEnvelope {
	env := c.Envelope()
	if c.TDPMaxBattery > 0 {
		env.TDPMaxForced = min(env.TDPMaxForced, c.TDPMaxBattery)
		env.TDPMaxSafe = min(env.TDPMaxSafe, c.TDPMaxBattery)
	}
	if len(c.StockPPTBattery) > 0 {
		env.StockProfilePPT = make(map[string]api.TDPState, len(c.StockPPTBattery))
		for name, r := range c.StockPPTBattery {
			env.StockProfilePPT[name] = api.TDPState{
				PL1SPL: r.PL1, PL2SPPT: r.PL2, FPPT: r.FPPT, APUSPPT: r.APU, PlatformSPPT: r.Platform,
			}
		}
	}
	return env
}

// ProfilesConfig selects the platform-profile driver and describes the
// firmware profiles.
//
// The kernel is the first source: the driver offers what the handler's
// `choices` lists, and Names filters and orders that (kernel ∩ Names) — so a
// name here the kernel does not offer is dropped with a warning, and one the
// kernel offers that is not here is not offered. Names is also the fallback
// when sysfs cannot be read. Every entry must be a kernel profile name
// (api.KernelProfileNames).
//
// Handler names the platform-profile class device that owns the profile
// (its `name` file, "asus-wmi" on the Z13); empty picks one heuristically.
// Default is the profile a reset lands on, and must have a stock power row
// within the safe sustained maximum. Labels overrides display labels per name.
type ProfilesConfig struct {
	Method  string            `toml:"method"`
	Names   []string          `toml:"names"`
	Handler string            `toml:"handler"`
	Default string            `toml:"default"`
	Labels  map[string]string `toml:"labels"`

	// Pin is the one firmware profile the daemon holds: when anything else
	// selects another (power-profiles-daemon on low battery, a stray tool),
	// the daemon puts this one back and logs it. For a device whose firmware
	// profiles cap power below what its TDP control offers (the OXP's amd-pmf
	// low-power holds ~33 W whatever the SMU limits say). While pinned, the
	// pin is the only profile offered; when the power method is unavailable
	// on this machine (Device.Unavailable), the pin is dropped and every name
	// in Names is offered, since the firmware profiles are then the only
	// power control there is.
	Pin string `toml:"pin"`
}

// LightingConfig selects the lighting driver and describes its zones.
//
// Modes and Speeds are optional subsets of what the driver's protocol can
// send, in the order to offer them; empty means all of it. Which inputs each
// mode takes is the protocol's to say, not the device's, so it is not here.
//
// Report, DynamicLightingInit and PowerFlags say how this device speaks the
// Aura protocol, which differs between machines: the Z13 takes output reports,
// its 0xC0 init packet and the 0xBD power message (the defaults); the ROG Xbox
// Ally X takes feature reports and neither packet (G-Helper's Ally path).
type LightingConfig struct {
	Method string               `toml:"method"`
	Zones  []LightingZoneConfig `toml:"zones"`
	Modes  []string             `toml:"modes"`
	Speeds []string             `toml:"speeds"`

	// Report is how Aura packets are sent: "output" reports (the default)
	// or "feature" reports.
	Report string `toml:"report"`
	// DynamicLightingInit sends the Z13's 0xC0 0x03 0x01 init packet;
	// absent means true.
	DynamicLightingInit *bool `toml:"dynamic_lighting_init"`
	// PowerFlags sends the 0xBD power-state message on every apply and off;
	// absent means true. Without it, off is brightness 0.
	PowerFlags *bool `toml:"power_flags"`
}

// FeatureReports reports whether Aura packets go out as feature reports.
func (c LightingConfig) FeatureReports() bool { return c.Report == "feature" }

// SendsDynamicLightingInit reports whether the Z13's 0xC0 init packet is sent.
func (c LightingConfig) SendsDynamicLightingInit() bool {
	return c.DynamicLightingInit == nil || *c.DynamicLightingInit
}

// SendsPowerFlags reports whether the 0xBD power message is sent.
func (c LightingConfig) SendsPowerFlags() bool { return c.PowerFlags == nil || *c.PowerFlags }

// LightingZoneConfig is one zone, as [[lighting.zones]]: its wire name, the
// label to show, the USB vendor:product of the HID device that carries it, and
// the protocol's zone byte for it.
type LightingZoneConfig struct {
	Name  string `toml:"name"`
	Label string `toml:"label"`
	USB   string `toml:"usb"`  // "0b05:1a30"
	Zone  int    `toml:"zone"` // the protocol's zone byte

	// Interface is the USB interface carrying the lighting reports
	// (bInterfaceNumber), the only one voltaire setup grants. Required: a
	// keyboard's other interfaces include the one it types through, and a
	// grant by vendor:product alone hands every process in the group its raw
	// keystrokes over hidraw — which every z13ctl release did for the Z13's
	// cover until 2026-11-15.
	Interface *int `toml:"interface"`
}

// USBIDs parses the zone's "vvvv:pppp" USB identifier.
func (z LightingZoneConfig) USBIDs() (vendor, product uint16, err error) {
	return ParseUSB(z.USB)
}

// ParseUSB parses a "vvvv:pppp" USB identifier in hex.
func ParseUSB(id string) (vendor, product uint16, err error) {
	v, p, ok := strings.Cut(id, ":")
	if !ok || len(v) != 4 || len(p) != 4 {
		return 0, 0, fmt.Errorf("usb %q must be vvvv:pppp in hex", id)
	}
	vn, err1 := strconv.ParseUint(v, 16, 16)
	pn, err2 := strconv.ParseUint(p, 16, 16)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("usb %q must be vvvv:pppp in hex", id)
	}
	return uint16(vn), uint16(pn), nil
}

// ZoneNames returns the zones' wire names in declaration order.
func (c LightingConfig) ZoneNames() []string {
	out := make([]string, len(c.Zones))
	for i, z := range c.Zones {
		out[i] = z.Name
	}
	return out
}

// ToggleEntry is one firmware toggle the device offers.
//
// Description is optional prose shown beside the control, carrying anything the
// label cannot — most usefully a consequence ("may cause ghosting"). It lives in
// device data because it is a fact about the hardware, so every client gets the
// same warning without restating it.
//
// The driver decides which toggles exist — the kernel's attributes, through an
// allowlist in code — so an entry is wording for one of them; Hidden withholds
// an allowlisted attribute this device should not offer.
type ToggleEntry struct {
	ID          string `toml:"id"`
	Label       string `toml:"label"`
	Description string `toml:"description"`
	Hidden      bool   `toml:"hidden"`
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

	// ChargeLimitMin and ChargeLimitMax bound the accepted threshold, in
	// percent. The kernel publishes no range for charge_control_end_threshold,
	// so this is the one place it can come from; zero means the default below.
	ChargeLimitMin int `toml:"charge_limit_min"`
	ChargeLimitMax int `toml:"charge_limit_max"`

	// Charger is where the charger kind (adapter or USB-C) is read: empty for
	// the generic power_supply view, "asus-armoury" for ASUS's charge_mode
	// attribute. The generic view needs no firmware call; the ASUS attribute is
	// the one the Z13's values were measured against.
	Charger string `toml:"charger"`

	// ChargingNeedsFlow marks a pack whose `status` reads "Charging" whether
	// or not current flows (driver.BatteryCaps.ChargingNeedsFlow).
	ChargingNeedsFlow bool `toml:"charging_needs_flow"`

	// YieldInGamingMode leaves the charge limit to Steam while a gamescope
	// session runs (steamos-manager writes it there); see the power key.
	YieldInGamingMode bool `toml:"yield_in_gaming_mode"`
}

// The charge-limit range a device gets when its data declares none: what
// every daemon accepted before the range was device data, so an existing
// device file keeps its behaviour.
const (
	DefaultChargeLimitMin = 40
	DefaultChargeLimitMax = 100
)

// Caps returns the battery capabilities this device declares.
func (c BatteryConfig) Caps() driver.BatteryCaps {
	limit := true
	if c.ChargeLimit != nil {
		limit = *c.ChargeLimit
	}
	caps := driver.BatteryCaps{
		ChargeLimit: limit, Health: c.Health, ChargerSource: c.Charger,
		ChargingNeedsFlow: c.ChargingNeedsFlow,
	}
	if limit {
		caps.ChargeLimitMin, caps.ChargeLimitMax = DefaultChargeLimitMin, DefaultChargeLimitMax
		if c.ChargeLimitMin != 0 {
			caps.ChargeLimitMin = c.ChargeLimitMin
		}
		if c.ChargeLimitMax != 0 {
			caps.ChargeLimitMax = c.ChargeLimitMax
		}
	}
	return caps
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

	// Wording overrides; each empty one takes the driver's own (see
	// driver.UndervoltText).
	Label       string `toml:"label"`
	ShortLabel  string `toml:"short_label"`
	Unit        string `toml:"unit"`
	Description string `toml:"description"`
}

// Text is the wording device data gives the undervolt control, for the
// driver to fill in where it is empty.
func (c UndervoltConfig) Text() driver.UndervoltText {
	return driver.UndervoltText{Label: c.Label, ShortLabel: c.ShortLabel, Unit: c.Unit, Description: c.Description}
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
	Fans           string `toml:"fans"`
	HistorySeconds int    `toml:"history_seconds"`
}

// DefaultHistorySeconds is the sample history a device keeps when its data does
// not say: one hour at the sampler's 1 Hz.
//
// Raised from five minutes when the dashboard grew longer windows (Jeff,
// 2026-08-14: "I imagine people may want to see longer history"). The cost is
// measured rather than assumed: driver.Sample is 288 bytes plus a two-int RPM
// slice, so an hour is ~1 MiB of ring per device — nothing, for a machine the
// daemon is managing the power of.
//
// The bound that does exist is on the *wire*, not the ring. telemetry-history
// runs ~440 bytes per sample, and api.sendCommand reads replies through a
// 4 MiB ceiling, so a device declaring much past two hours would make a
// whole-window request fail as "token too long" — which presents as a broken
// daemon rather than as too much history. An hour leaves ample margin; anything
// substantially longer needs the reply paged before the ring is grown.
const DefaultHistorySeconds = 3600

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
		Fans:           c.Fans,
		HistorySeconds: secs,
	}
}

// ButtonConfig is one hardware button the user may choose to open voltaire
// with. The list is the device's, never the user's: a user picks among these
// entries, and nothing reads a keyboard to learn a key. Only list a button
// captured on the real machine (evtest) that has no other job there — the
// kernel's capability bitmap advertises keys a model never sends, and since
// the watcher never grabs, a key that already does something would do both.
type ButtonConfig struct {
	// ID names the button in state and on the wire ("armoury-crate"). Lowercase
	// letters, digits and hyphens; stable once shipped, since a user's choice
	// is stored by it.
	ID string `toml:"id"`

	// Label is what the button is called on this machine, as it would appear
	// mid-sentence ("Armoury Crate button").
	Label string `toml:"label"`

	// Default marks the button that opens voltaire out of the box. At most
	// one; with none, no button opens it until the user picks one.
	Default bool `toml:"default"`

	Method  string `toml:"method"`
	Device  string `toml:"device"`  // input device name to find by sysfs
	Keycode int    `toml:"keycode"` // key code to watch for (evdev-key)
	Keys    []int  `toml:"keys"`    // keys held together (evdev-chord)

	// hidraw-report: the node by USB vendor:product and interface (the grant
	// is written from the same values), and the report bytes meaning a press.
	// On the evdev methods usb and interface are optional, together, and
	// narrow the node beyond its name: the OneXPlayer X2 Mini Pro's controller
	// registers two nodes named "HID 1a86:fe00" (interfaces 0 and 1) and
	// only interface 0 carries the button chords.
	USB       string `toml:"usb"`
	Interface *int   `toml:"interface"`
	ReportID  int    `toml:"report_id"` // first report byte; 0 for unnumbered reports
	// Match is [offset, value] pairs that must all hold in a press report
	// (offsets count the report ID). It has to tell a press from a release
	// wherever a device reports both, or every tap counts twice.
	Match [][]int `toml:"match"`

	// InputPlumber is the gamepad button capability InputPlumber gives this
	// button on this machine ("QuickAccess2"), for when InputPlumber manages
	// the device: it grabs and hides the node the method reads, and the
	// button then reaches voltaire only through a profile mapping that
	// capability to voltaire's DBus action, which the daemon loads. Empty: no
	// such route, and the button is silent while InputPlumber manages it.
	InputPlumber string `toml:"inputplumber"`

	// KeyboardNode opts a node that udev classes as a typing keyboard in, and
	// says why it is safe on this model ("the AT keyboard carries only the
	// button chords on the X2 Mini Pro"). Without it neither the udev grant
	// nor the watcher will touch a keyboard. With it the grant is written
	// scoped to this device's DMI match, so it can never apply to another
	// machine, and the match may not contain udev glob characters.
	KeyboardNode string `toml:"keyboard_node"`
}

// inputPlumberCapability is the shape of an InputPlumber GamepadButton name
// ("QuickAccess2", "LeftPaddle1"). The shape only: the list is InputPlumber's
// and grows, and a name it does not know fails loudly there.
var inputPlumberCapability = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// MaxHIDReport is the longest input report a hidraw-report button reads: a
// full-speed interrupt report. A match offset beyond it could never hold.
const MaxHIDReport = 64

// USBIDs parses a hidraw-report button's "vvvv:pppp" USB identifier.
func (b ButtonConfig) USBIDs() (vendor, product uint16, err error) {
	return ParseUSB(b.USB)
}

// buttonIDPattern is what a button id may contain.
var buttonIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// evdevMethods are the button methods that read an evdev node, the only kind
// a keyboard opt-in can apply to.
var evdevMethods = []string{"evdev-key", "evdev-chord"}

// udevGlob reports whether s holds a character udev treats as a pattern.
func udevGlob(s string) bool { return strings.ContainsAny(s, "*?[|") }

// failFunc records one validation failure.
type failFunc func(format string, a ...any)

// Validate reports everything wrong with a config at once, so a device data PR
// gets one round of feedback rather than one error per push. A valid config is
// one the registry can assemble without further checks.
func (c Config) Validate() error {
	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	c.validateIdentity(fail)
	c.validateQuickbar(fail)
	c.validateRumble(fail)
	c.validateFans(fail)
	c.validatePower(fail)
	c.validateProfiles(fail)
	c.validateLighting(fail)
	c.validateToggles(fail)
	c.validateBattery(fail)
	c.validateSmall(fail)
	c.validateButtons(fail)
	return errors.Join(errs...)
}

// validateIdentity checks the device block and its DMI match.
func (c Config) validateIdentity(fail failFunc) {
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
}

// validateQuickbar checks [quickbar].
func (c Config) validateQuickbar(fail failFunc) {
	if c.Quickbar != nil {
		if len(c.Quickbar.Controls) == 0 {
			fail("quickbar.controls is empty: drop the block for the built-in default")
		}
		seen := map[string]bool{}
		for _, id := range c.Quickbar.Controls {
			if id == "" || seen[id] {
				fail("quickbar.controls: %q is empty or listed twice", id)
			}
			seen[id] = true
		}
	}
}

// validateRumble checks [rumble].
func (c Config) validateRumble(fail failFunc) {
	if c.Rumble != nil {
		if c.Rumble.Method == "" {
			fail("rumble.method is required")
		}
		if _, _, err := ParseUSB(c.Rumble.USB); err != nil {
			fail("rumble.usb: %v", err)
		}
	}
}

// validateFans checks [fans], its presets and the floor curve's shape.
func (c Config) validateFans(fail failFunc) {
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
		if c.Fans.RPMMax < 0 {
			fail("fans.rpm_max %d must not be negative", c.Fans.RPMMax)
		}
		// The hold runs inside logind's delay budget, 5 s by default; the daemon
		// cuts it short to fit anyway, but a value at or past the budget would
		// never be what the file says.
		if c.Fans.SleepReleaseSettleMs < 0 || c.Fans.SleepReleaseSettleMs >= 5000 {
			fail("fans.sleep_release_settle_ms %d must be 0 or more and under 5000", c.Fans.SleepReleaseSettleMs)
		}
		seen := make(map[string]bool, len(c.Fans.Presets))
		for i, p := range c.Fans.Presets {
			if p.Name == "" || p.Label == "" {
				fail("fans.presets[%d] needs both name and label", i)
			}
			// Case-insensitively unique, because lookup folds case: two presets
			// differing only in case would make one of them unreachable.
			if key := strings.ToLower(p.Name); seen[key] {
				fail("fans.presets[%d]: duplicate name %q", i, p.Name)
			} else {
				seen[key] = true
			}
			if c.Fans.Points > 0 && len(p.Curve) != c.Fans.Points {
				fail("fans.presets[%d] (%s) has %d points, want the fans block's %d", i, p.Name, len(p.Curve), c.Fans.Points)
			}
			if pts, ok := curvePoints(p.Curve); !ok {
				fail("fans.presets[%d] (%s): every point must be a [temp, pwm] pair", i, p.Name)
			} else if err := safety.CheckCurve(pts, 255); err != nil {
				fail("fans.presets[%d] (%s): %v", i, p.Name, err)
			}
		}
	}
}

// validatePower checks [power] and its stock rows.
func (c Config) validatePower(fail failFunc) {
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
		if p.BootCachePL1 < 0 || p.BootCachePL1 > p.TDPMaxForced {
			fail("power.boot_cache_pl1 %d outside [0, %d]", p.BootCachePL1, p.TDPMaxForced)
		}
		seenLimit := map[string]bool{}
		for _, l := range p.Limits {
			switch {
			case !slices.Contains(PowerLimitIDs, l.ID):
				fail("power.limits id %q is not one of %v", l.ID, PowerLimitIDs)
			case seenLimit[l.ID]:
				fail("power.limits lists %q twice", l.ID)
			}
			seenLimit[l.ID] = true
		}
		if len(p.Limits) > 0 && !seenLimit["pl1"] {
			fail("power.limits must include pl1, the sustained limit every TDP sets")
		}
		if pts, ok := curvePoints(p.FloorCurve); !ok {
			fail("power.floor_curve: every point must be a [temp, pwm] pair")
		} else if err := safety.CheckCurve(pts, 255); err != nil {
			fail("power.floor_curve: %v", err)
		}
		// A floor with no fan control cannot be satisfied, only refused —
		// safety.Engine would deny every high-TDP request. Refuse the data
		// instead, at review time.
		if len(p.FloorCurve) > 0 && c.Fans == nil {
			fail("power.floor_curve requires a fans block: a floor without fan control cannot be enforced")
		}
		// Every selectable firmware profile needs a stock row: switching to it
		// writes the row to bring a high custom limit down before the fans are
		// released (HandBackToFirmware), and the row is what the PPT attributes
		// then show.
		if c.Profiles != nil {
			// With a pin, only the pin is selectable while this power control
			// exists; the other names are offered only when it is unavailable
			// (Assemble), and then there are no limits to hand back.
			selectable := c.Profiles.Names
			if c.Profiles.Pin != "" {
				selectable = []string{c.Profiles.Pin}
			}
			for _, name := range selectable {
				if _, ok := p.StockPPT[name]; !ok {
					fail("power.stock_ppt is missing profile %q", name)
				}
			}
			// The other direction: a row for a name the device does not offer
			// is keyed by nothing the daemon will ever look up, and reads as
			// a typo nobody is told about.
			for name := range p.StockPPT {
				if !slices.Contains(c.Profiles.Names, name) {
					fail("power.stock_ppt has a row for %q, which profiles.names does not list", name)
				}
			}
			// The reset target's own limits must be safe: tdp-reset lowers
			// power by writing this row *before* it releases the fans, which
			// is only fail-closed if the row is below the fan floor's limit.
			if d := c.Profiles.Default; d != "" {
				if row, ok := p.StockPPT[d]; ok && row.PL1 > p.TDPMaxSafe {
					fail("profiles.default %q has a stock PL1 of %dW, above tdp_max_safe %dW", d, row.PL1, p.TDPMaxSafe)
				}
			}
		}
	}

	if p := c.Power; p != nil {
		if p.TDPMaxBattery != 0 && (p.TDPMaxBattery < p.TDPMin || p.TDPMaxBattery > p.TDPMaxForced) {
			fail("power.tdp_max_battery %dW must be within tdp_min..tdp_max_forced (%d..%d)", p.TDPMaxBattery, p.TDPMin, p.TDPMaxForced)
		}
		for name := range p.StockPPTBattery {
			if _, ok := p.StockPPT[name]; !ok {
				fail("power.stock_ppt_battery has a row for %q, which power.stock_ppt does not", name)
			}
		}
		if len(p.StockPPTBattery) > 0 {
			for name := range p.StockPPT {
				if _, ok := p.StockPPTBattery[name]; !ok {
					fail("power.stock_ppt_battery is missing profile %q", name)
				}
			}
		}
		if p.Method == "amd-smu" && len(p.StockPPT) != 1 {
			fail("power.method amd-smu needs exactly one stock_ppt row, the firmware profile it runs under (got %d)", len(p.StockPPT))
		}
	}
}

// validateProfiles checks [profiles], including the pin.
func (c Config) validateProfiles(fail failFunc) {
	if c.Profiles != nil && c.Profiles.Pin != "" {
		if !slices.Contains(c.Profiles.Names, c.Profiles.Pin) {
			fail("profiles.pin %q is not in profiles.names", c.Profiles.Pin)
		}
		if c.Power == nil {
			fail("profiles.pin needs a power block: the pin protects the power control, and holds only while it exists")
		}
	}
	if c.Profiles != nil {
		if c.Profiles.Method == "" {
			fail("profiles.method is required")
		}
		if len(c.Profiles.Names) == 0 {
			fail("profiles.names must list the firmware profile names")
		}
		for _, name := range c.Profiles.Names {
			if !api.IsKernelProfileName(name) {
				fail("profiles.names entry %q is not a kernel platform_profile name", name)
			}
		}
		if d := c.Profiles.Default; d != "" && !slices.Contains(c.Profiles.Names, d) {
			fail("profiles.default %q is not in profiles.names", d)
		}
		for name := range c.Profiles.Labels {
			if !slices.Contains(c.Profiles.Names, name) {
				fail("profiles.labels has a label for %q, which profiles.names does not list", name)
			}
		}
	}
}

// validateLighting checks [lighting] and its zones.
func (c Config) validateLighting(fail failFunc) {
	if c.Lighting != nil {
		if c.Lighting.Method == "" {
			fail("lighting.method is required")
		}
		if len(c.Lighting.Zones) == 0 {
			fail("lighting.zones must name at least one zone")
		}
		if r := c.Lighting.Report; r != "" && r != "output" && r != "feature" {
			fail("lighting.report %q must be \"output\" or \"feature\"", r)
		}
		seen := map[string]bool{}
		for i, z := range c.Lighting.Zones {
			switch {
			case z.Name == "" || z.Label == "":
				fail("lighting.zones[%d] needs both name and label", i)
			case strings.HasPrefix(z.Name, "/"):
				fail("lighting.zones[%d] name %q must not start with '/' (that is a hidraw path)", i, z.Name)
			case seen[z.Name]:
				fail("lighting.zones[%d]: duplicate name %q", i, z.Name)
			}
			seen[z.Name] = true
			if _, _, err := z.USBIDs(); err != nil {
				fail("lighting.zones[%d] (%s): %v", i, z.Name, err)
			}
			if z.Zone < 0 || z.Zone > 255 {
				fail("lighting.zones[%d] (%s) zone byte %d outside 0–255", i, z.Name, z.Zone)
			}
			if z.Interface == nil || *z.Interface < 0 || *z.Interface > 255 {
				fail("lighting.zones[%d] (%s) needs interface, the USB interface number (0–255) carrying "+
					"its reports: only that interface is granted", i, z.Name)
			}
		}
	}
}

// validateToggles checks [toggles].
func (c Config) validateToggles(fail failFunc) {
	if c.Toggles != nil {
		if c.Toggles.Method == "" {
			fail("toggles.method is required")
		}
		if len(c.Toggles.Entries) == 0 {
			fail("toggles block with no entries; drop the block instead")
		}
		for i, e := range c.Toggles.Entries {
			if e.ID == "" || (e.Label == "" && !e.Hidden) {
				fail("toggles.entries[%d] needs both id and label (or hidden = true)", i)
			}
		}
	}
}

// validateBattery checks [battery].
func (c Config) validateBattery(fail failFunc) {
	if c.Battery != nil {
		if c.Battery.Method == "" {
			fail("battery.method is required")
		}
		// Same rule as an empty toggles block: a capability that offers nothing
		// still tells every client the controls exist. Say so by omission.
		caps := c.Battery.Caps()
		if ch := c.Battery.Charger; ch != "" && ch != "asus-armoury" {
			fail("battery.charger %q is not a known source (\"asus-armoury\", or omit for the generic power_supply view)", ch)
		}
		if !caps.ChargeLimit && !caps.Health {
			fail("battery block offers neither charge_limit nor health; drop the block instead")
		}
		// A range on a battery with no limit to bound would be served as zero
		// and silently ignored, which reads as a typo nobody is told about.
		if !caps.ChargeLimit && (c.Battery.ChargeLimitMin != 0 || c.Battery.ChargeLimitMax != 0) {
			fail("battery.charge_limit_min/max set with charge_limit = false")
		}
		if caps.ChargeLimit && (caps.ChargeLimitMin < 1 || caps.ChargeLimitMin >= caps.ChargeLimitMax || caps.ChargeLimitMax > 100) {
			fail("battery charge limit range must satisfy 1 <= min < max <= 100 (got %d..%d)",
				caps.ChargeLimitMin, caps.ChargeLimitMax)
		}
	}
}

// validateSmall checks [cpu], [undervolt] and [telemetry].
func (c Config) validateSmall(fail failFunc) {
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
}

// validateButtons checks [[buttons]].
func (c Config) validateButtons(fail failFunc) {
	seenButton := map[string]bool{}
	defaults := 0
	for i, b := range c.Buttons {
		name := fmt.Sprintf("buttons[%d]", i)
		if b.ID != "" {
			name = fmt.Sprintf("button %q", b.ID)
		}
		switch {
		case b.ID == "":
			fail("%s: id is required — the user's choice is stored by it", name)
		case !buttonIDPattern.MatchString(b.ID):
			fail("%s: id must be lowercase letters, digits and single hyphens", name)
		case seenButton[b.ID]:
			fail("%s: duplicate id", name)
		}
		seenButton[b.ID] = true
		if b.Label == "" {
			fail("%s: label is required — it is what the button list shows", name)
		}
		if b.Default {
			defaults++
		}
		if b.Method == "" {
			fail("%s: method is required", name)
		}
		if b.Method == "evdev-key" && (b.Device == "" || b.Keycode <= 0) {
			fail("%s: evdev-key needs both device and a positive keycode", name)
		}
		if b.Method == "hidraw-report" {
			if _, _, err := b.USBIDs(); err != nil {
				fail("%s: %v", name, err)
			}
			if b.Interface == nil || *b.Interface < 0 || *b.Interface > 255 {
				fail("%s: hidraw-report needs interface, the USB interface number (0–255): only that interface is granted", name)
			}
			if b.ReportID < 0 || b.ReportID > 255 {
				fail("%s: hidraw-report needs report_id in 0–255 (0 for unnumbered reports)", name)
			}
			if len(b.Match) == 0 {
				fail("%s: hidraw-report needs match, the [offset, value] pairs of a press report", name)
			}
			for _, m := range b.Match {
				if len(m) != 2 || m[0] < 0 || m[0] >= MaxHIDReport || m[1] < 0 || m[1] > 255 {
					fail("%s: each match entry is [offset, value]: offset 0–%d, value 0–255 (got %v)", name, MaxHIDReport-1, m)
					break
				}
			}
		}
		if slices.Contains(evdevMethods, b.Method) && (b.USB != "" || b.Interface != nil) {
			if _, _, err := b.USBIDs(); err != nil {
				fail("%s: %v", name, err)
			}
			if b.Interface == nil || *b.Interface < 0 || *b.Interface > 255 {
				fail("%s: usb on an evdev button needs interface too (0–255): the two together name the node", name)
			}
		}
		if b.InputPlumber != "" && !inputPlumberCapability.MatchString(b.InputPlumber) {
			fail("%s: inputplumber must be an InputPlumber gamepad button name such as QuickAccess2 (got %q)", name, b.InputPlumber)
		}
		if b.Method == "evdev-chord" {
			distinct := map[int]bool{}
			for _, k := range b.Keys {
				if k > 0 {
					distinct[k] = true
				}
			}
			if b.Device == "" || len(b.Keys) < 2 || len(distinct) != len(b.Keys) {
				fail("%s: evdev-chord needs device and at least two distinct positive keys", name)
			}
		}
		if b.KeyboardNode != "" {
			if !slices.Contains(evdevMethods, b.Method) {
				fail("%s: keyboard_node applies only to evdev methods", name)
			}
			for _, m := range c.Device.Match {
				if udevGlob(m.Vendor) || udevGlob(m.Product) || udevGlob(m.ProductPrefix) {
					fail("%s: keyboard_node needs a device.match free of udev glob characters (*?[|), "+
						"or the grant's DMI scope could match other machines", name)
					break
				}
			}
		}
	}
	if defaults > 1 {
		fail("at most one button may be the default (got %d)", defaults)
	}
}

// curvePoints converts a device file's [[temp, pwm], ...] to points, false
// when any entry is not a pair.
func curvePoints(raw [][]int) ([]api.FanCurvePoint, bool) {
	pts := make([]api.FanCurvePoint, len(raw))
	for i, p := range raw {
		if len(p) != 2 {
			return nil, false
		}
		pts[i] = api.FanCurvePoint{Temp: p[0], PWM: p[1]}
	}
	return pts, true
}
