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
	Battery   *BatteryInfo   `json:"battery,omitempty"`
	Telemetry *TelemetryInfo `json:"telemetry,omitempty"`

	// Presence-only capability: the daemon watches a hardware button and emits
	// the events a client can subscribe to. There is nothing to parameterize.
	Buttons bool `json:"buttons,omitempty"`
}

// FanInfo is the device's fan-curve shape: how many points a curve holds and
// the axes an editor should draw. TempMin/TempMax are the editor's temperature
// axis, not validation bounds — the hardware tolerates points outside them.
type FanInfo struct {
	Points  int `json:"points"`
	TempMin int `json:"temp_min"` // degrees Celsius
	TempMax int `json:"temp_max"`
	PWMMax  int `json:"pwm_max"`
}

// PowerInfo is the device's power-limit envelope. Sustained limits above
// TDPMaxSafe require the caller's explicit force flag and put the fans on
// FloorCurve; TDPMaxForced is the absolute ceiling. An empty FloorCurve means
// the device imposes no floor.
type PowerInfo struct {
	TDPMin       int             `json:"tdp_min"`
	TDPMaxSafe   int             `json:"tdp_max_safe"`
	TDPMaxForced int             `json:"tdp_max_forced"`
	FloorCurve   []FanCurvePoint `json:"floor_curve,omitempty"`

	// StockProfilePPT maps each firmware profile name to the PPT values the
	// daemon writes when that profile is selected. A client needs these to tell
	// "the firmware's numbers" from "numbers the user chose" — without them it
	// cannot label a limit as stock, and any client deriving that from a
	// hardcoded table is answering for the wrong machine the moment voltaire
	// supports a second one. Empty means the device declares no stock table.
	StockProfilePPT map[string]TDPState `json:"stock_profile_ppt,omitempty"`
}

// ProfileInfo lists the firmware performance profiles. These are also the
// reserved names: a custom profile can never take one of them.
type ProfileInfo struct {
	Names []string `json:"names"`
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

	// Source is where the toggle comes from: ToggleSourceCore for one a
	// compiled-in driver provides, "plugin:<id>" for one an external plugin
	// contributes. Always populated — the daemon substitutes "core" — so a
	// client grouping rows by provenance never has to treat absence as a third
	// case.
	Source string `json:"source"`
}

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

// BatteryInfo says what the device's battery interface offers. The section
// being present means there is a battery to report on at all; the two fields
// are independently absent, so a machine can report state of health while
// exposing no charge-limit attribute, or the reverse.
type BatteryInfo struct {
	ChargeLimit bool `json:"charge_limit,omitempty"` // batterylimit get/set work
	Health      bool `json:"health,omitempty"`       // get-state reports state of health
}

// TelemetryInfo describes what the device's telemetry source reports, so a
// dashboard knows which graphs to draw before it has asked for a single
// sample.
//
// PowerDraw names the package-power source ("rapl", "pm-table") and is empty
// when the device reads none — in which case Sample's package power is always
// zero and the graph should be hidden rather than drawn flat. GPU ("amdgpu"),
// CPUStats ("procfs") and NPU ("amdxdna") name the expanded sources on the
// same terms: a name for provenance, absence meaning the matching sample
// fields are never filled and their graphs should not exist. HistorySeconds
// is the largest window a telemetry-history request can usefully ask for; zero
// means the daemon keeps no history for this device and only live readings are
// available.
type TelemetryInfo struct {
	PowerDraw      string `json:"power_draw,omitempty"`
	GPU            string `json:"gpu,omitempty"`
	CPUStats       string `json:"cpu_stats,omitempty"`
	NPU            string `json:"npu,omitempty"`
	HistorySeconds int    `json:"history_seconds,omitempty"`
}
