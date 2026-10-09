package api

// device.go — the capability/limits document served by the daemon's
// device-get command.
//
// DeviceInfo is how a client learns what the machine it is talking to can do
// and which bounds to validate and render against, instead of hardcoding one
// device's numbers. Capability discovery is by absence, never by error: a
// section that is nil (omitted on the wire) means the device does not have
// that capability, and the client hides the corresponding controls. The
// values come from the device data the daemon was assembled from, so they are
// static for the daemon's lifetime — fetch once and cache.

// DeviceInfo describes the assembled device: its identity, the capabilities
// it has, and the limits that go with them. Returned by SendDeviceGet.
type DeviceInfo struct {
	ID    string `json:"id"`    // device data identifier, e.g. "asus-rog-flow-z13-2025"
	Model string `json:"model"` // short hardware name for display, e.g. "GZ302"

	Fans      *FanInfo       `json:"fans,omitempty"`
	Power     *PowerInfo     `json:"power,omitempty"`
	Profiles  *ProfileInfo   `json:"profiles,omitempty"`
	Lighting  *LightingInfo  `json:"lighting,omitempty"`
	Toggles   []ToggleInfo   `json:"toggles,omitempty"`
	Undervolt *UndervoltInfo `json:"undervolt,omitempty"`
	CPU       *CPUInfo       `json:"cpu,omitempty"`
	Battery   *BatteryInfo   `json:"battery,omitempty"`
	Telemetry *TelemetryInfo `json:"telemetry,omitempty"`

	// Presence-only capability: the daemon watches a hardware button and emits
	// the events a client can subscribe to. There is nothing to parameterize.
	Buttons bool `json:"buttons,omitempty"`
}

// FanInfo is the device's fan-curve shape: how many points a curve holds and
// the axes an editor should draw. TempMin/TempMax are the editor's temperature
// axis, not validation bounds — the hardware tolerates points outside them.
// Points is the kernel's count where the daemon can read it; size an editor
// from it, never from a constant.
type FanInfo struct {
	Points  int `json:"points"`
	TempMin int `json:"temp_min"` // degrees Celsius
	TempMax int `json:"temp_max"`
	PWMMax  int `json:"pwm_max"`

	// Labels names each fan, in the order get-state's rpm reports them, so
	// its length is the number of fans. Absent from a daemon older than the
	// field, or one that cannot say; a client then labels by position.
	Labels []string `json:"labels,omitempty"`

	// RPMMax is the fans' top speed where the device data states one — hwmon
	// publishes no fan*_max on most machines. A chart hint, absent when
	// unknown.
	RPMMax int `json:"rpm_max,omitempty"`

	// Presets are named starting-point curves the device data ships, in the
	// order a client should offer them. Empty means the device declares none,
	// in which case a client shows no preset control at all rather than an
	// empty list — capability by absence, as everywhere else in this document.
	//
	// A preset is only ever a curve: applying one is the ordinary fancurve set
	// with the preset's points, so every check that governs a hand-drawn curve
	// governs a preset too. There is deliberately no preset for firmware auto,
	// which is a fan *mode* rather than a curve and has its own command
	// (fancurve --reset).
	Presets []FanPreset `json:"presets,omitempty"`
}

// FanPreset is one named fan curve offered as a starting point. Name is the
// wire/CLI identifier (lowercase, matched case-insensitively); Label is what to
// show; Description is optional prose for a tooltip.
//
// Both strings are device data rather than client-side text for the same reason
// ToggleInfo.Description is: what a curve does to a *particular* machine — where
// its fans stop, how hot it lets the package run — is hardware knowledge a
// client rendering these generically cannot derive, and a name is not always
// title-case ("zero-rpm" is "Zero RPM", not "Zero-Rpm").
type FanPreset struct {
	Name        string          `json:"name"`
	Label       string          `json:"label"`
	Description string          `json:"description,omitempty"`
	Curve       []FanCurvePoint `json:"curve"`
}

// PowerInfo is the device's power-limit envelope. Sustained limits above
// TDPMaxSafe require the caller's explicit force flag and put the fans on
// FloorCurve; TDPMaxForced is the absolute ceiling. An empty FloorCurve means
// the device imposes no floor.
//
// TDPMin..TDPMaxForced is the range the sustained limit (PL1) accepts. Where
// the daemon can read the kernel's own bounds it reports those — on the
// GZ302EA through asus-armoury, PL1 28–80, PL2 32–92 and PL3 45–93 W — so they
// can differ from the device data, and can differ between AC and battery on a
// device whose firmware keeps separate tables.
type PowerInfo struct {
	TDPMin       int             `json:"tdp_min"`
	TDPMaxSafe   int             `json:"tdp_max_safe"`
	TDPMaxForced int             `json:"tdp_max_forced"`
	FloorCurve   []FanCurvePoint `json:"floor_curve,omitempty"`

	// Interface names the kernel interface the limits go through
	// ("asus-armoury", "asus-nb-wmi"); empty when the daemon reports none.
	Interface string `json:"interface,omitempty"`

	// PL2 and PL3 are the ranges the burst limits accept; absent means
	// TDPMin..TDPMaxForced. A request below a burst limit's minimum is raised to
	// it, and one above its maximum is refused.
	PL2 *PowerRange `json:"pl2,omitempty"`
	PL3 *PowerRange `json:"pl3,omitempty"`

	// StockProfilePPT maps each firmware profile name to the PPT values the
	// daemon writes when that profile is selected. A client needs these to tell
	// "the firmware's numbers" from "numbers the user chose" — without them it
	// cannot label a limit as stock, and any client deriving that from a
	// hardcoded table is answering for the wrong machine the moment voltaire
	// supports a second one. Empty means the device declares no stock table.
	StockProfilePPT map[string]TDPState `json:"stock_profile_ppt,omitempty"`
}

// PowerRange is an inclusive range in watts.
type PowerRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// ProfileInfo lists the firmware performance profiles this device offers, in
// the order to present them (the kernel's: least to most power).
//
// Names is what `profile` accepts as a firmware profile and the only list a
// client should build firmware-profile controls from. The names a custom
// profile may never take are wider — every kernel profile name, see
// IsReservedProfileName — so validate a new custom name against that, not
// against this list.
//
// Default is the profile a reset lands on (tdp-reset, tuning-reset): one
// whose firmware power limits are within the safe sustained maximum. Entries
// carries a display label per name, in the same order as Names; a daemon
// older than either field omits it, and ProfileLabel is the fallback.
type ProfileInfo struct {
	Names   []string       `json:"names"`
	Default string         `json:"default,omitempty"`
	Entries []ProfileEntry `json:"entries,omitempty"`
}

// ProfileEntry is one firmware profile's display label.
type ProfileEntry struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// LightingInfo lists the addressable lighting zone names.
type LightingInfo struct {
	Zones []string `json:"zones"`
}

// ToggleInfo describes one firmware toggle the device offers. ID is the wire
// identifier for the feature/feature-get commands; Label is a human-readable
// fallback for clients with no nicer name of their own. Kind says how the
// value is shaped — "bool" toggles take 0 or 1.
type ToggleInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`

	// Description is prose to show beside the control: what the toggle does,
	// and any consequence worth warning about. Empty when the device data
	// offers none, in which case a client shows the Label alone.
	//
	// It is served rather than left to the client because it is device
	// knowledge — whether panel overdrive causes ghosting is a fact about the
	// panel — and a client rendering these rows generically cannot know it.
	Description string `json:"description,omitempty"`

	// Values are the legal values, from the firmware where it reports them;
	// absent means the kind's own (0 and 1 for "bool"). A daemon older than
	// the field omits it.
	Values []int `json:"values,omitempty"`

	// Source is where the toggle comes from: ToggleSourceCore for one a
	// compiled-in driver provides, "plugin:<id>" for one an external plugin
	// contributes. Always populated — the daemon substitutes "core" — so a
	// client grouping rows by provenance never has to treat absence as a third
	// case.
	Source string `json:"source"`
}

// ToggleKindBool is a 0/1 firmware switch — the only kind that exists today.
// It is named here because a client rendering these rows generically has to
// decide what widget to build, and "kind == bool" written as a literal at each
// such site is how one of them comes to render a future enumerated toggle as a
// switch. A client must skip a kind it does not recognize rather than guess:
// showing an unknown shape as a switch would misrepresent the setting and, on
// a write, send 0 or 1 to something that means neither.
const ToggleKindBool = "bool"

// Toggle sources. A toggle provided by a compiled-in driver is ToggleSourceCore;
// one contributed by an external plugin is ToggleSourcePluginPrefix + its plugin
// id. A client that does not care about provenance can ignore the field
// entirely; one that groups rows by it should treat any unrecognized value as
// its own group rather than hiding the row.
const (
	ToggleSourceCore         = "core"
	ToggleSourcePluginPrefix = "plugin:"
)

// UndervoltInfo is the legal Curve Optimizer offset range (Min ≤ value ≤ Max;
// on the Z13, -40 to 0).
type UndervoltInfo struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// CPUInfo says what CPU-level controls the device offers. A section rather
// than a bool for the same reason BatteryInfo is one: its contents are
// independently absent, so a machine that gains a second CPU control later can
// say so without this field having meant two things.
type CPUInfo struct {
	// Boost is true when cpuboost get/set work. Boost clocks are a kernel
	// runtime setting rather than a firmware one — they come back on at every
	// boot — so unlike a toggle in Toggles the daemon persists and restores the
	// user's choice.
	Boost bool `json:"boost,omitempty"`
}

// BatteryInfo says what the device's battery interface offers. The section
// being present means there is a battery to report on at all; the two fields
// are independently absent, so a machine can report state of health while
// exposing no charge-limit attribute, or the reverse.
//
// ChargeLimitMin and ChargeLimitMax are the inclusive range batterylimit
// accepts, in percent. The kernel publishes only the threshold itself, never
// its bounds, so they come from device data. Both are zero when ChargeLimit
// is false, and a daemon older than these fields omits them; a client should
// then assume 40–100, the range every daemon accepted before it said so.
type BatteryInfo struct {
	ChargeLimit    bool `json:"charge_limit,omitempty"`     // batterylimit get/set work
	ChargeLimitMin int  `json:"charge_limit_min,omitempty"` // lowest accepted limit, percent
	ChargeLimitMax int  `json:"charge_limit_max,omitempty"` // highest accepted limit; writing it removes the cap
	Health         bool `json:"health,omitempty"`           // get-state reports state of health
}

// TelemetryInfo describes what the device's telemetry source reports, so a
// dashboard knows which graphs to draw before it has asked for a single
// sample.
//
// PowerDraw names the package-power source ("rapl", "pm-table") and is empty
// when the device reads none — in which case Sample's package power is always
// zero and the graph should be hidden rather than drawn flat. GPU ("amdgpu"),
// CPUStats ("procfs"), NPU ("amdxdna") and Net ("procfs") name the expanded
// sources on the same terms: a name for provenance, absence meaning the
// matching sample fields are never filled and their graphs should not exist.
// HistorySeconds is the largest window a telemetry-history request can
// usefully ask for; zero means the daemon keeps no history for this device
// and only live readings are available.
type TelemetryInfo struct {
	PowerDraw      string `json:"power_draw,omitempty"`
	GPU            string `json:"gpu,omitempty"`
	CPUStats       string `json:"cpu_stats,omitempty"`
	NPU            string `json:"npu,omitempty"`
	Net            string `json:"net,omitempty"`
	HistorySeconds int    `json:"history_seconds,omitempty"`

	// Chart hints, from the kernel where the daemon can read them; absent
	// means no hint, and a client keeps its own frame. ClockMaxMHz is the
	// highest any charted clock reaches (the CPU's boost ceiling, the GPU's
	// top P-state). TempLimitC is where the firmware starts throttling (the
	// ACPI passive trip). Both are where to frame an axis, not limits: a
	// reading past either is real and should expand the frame.
	ClockMaxMHz int `json:"clock_max_mhz,omitempty"`
	TempLimitC  int `json:"temp_limit_c,omitempty"`
}
