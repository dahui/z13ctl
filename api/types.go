// Package api provides the public client interface for the voltaire daemon.
// It contains the shared protocol types and socket client functions used by
// CLI commands, GUI frontends, and any other tool that communicates with the
// voltaire daemon over its Unix socket.
package api

import (
	"strconv"
	"strings"
)

// State holds the last-applied settings for all controllable subsystems.
// It is returned by SendGetState and broadcast as part of daemon responses.
//
// CustomProfiles is the source of truth for custom settings. FanCurve, TDP and
// Undervolt are a projection, retained so clients written against earlier
// versions keep working: they carry the active custom profile's settings, or
// the default "custom" profile's when a firmware profile is active — the same
// values selecting "custom" would recall. Undervolt.Active is what says whether
// an offset is applied to hardware right now; the values themselves survive a
// switch to a firmware profile so they can be recalled.
type State struct {
	Lighting           LightingState            `json:"lighting"`
	Devices            map[string]LightingState `json:"devices,omitempty"` // per-device overrides keyed by name
	Profile            string                   `json:"profile,omitempty"`
	Battery            int                      `json:"battery_limit,omitempty"`
	BootSound          int                      `json:"boot_sound,omitempty"`
	PanelOverdrive     int                      `json:"panel_overdrive,omitempty"`
	CustomProfiles     map[string]CustomProfile `json:"custom_profiles,omitempty"` // saved custom profiles keyed by name
	Autoswitch         *AutoswitchState         `json:"autoswitch,omitempty"`
	FanCurve           *FanCurveState           `json:"fan_curve,omitempty"` // projection; see the type doc
	TDP                *TDPState                `json:"tdp,omitempty"`       // projection; see the type doc
	Undervolt          *UndervoltState          `json:"undervolt,omitempty"` // projection; see the type doc
	UndervoltAvailable bool                     `json:"undervolt_available"` // true if ryzen_smu is loaded

	// CPUBoost is whether the CPU's opportunistic boost clocks are enabled.
	//
	// A pointer, and absent rather than false when it could not be read — the
	// same rule State.Features follows for a toggle whose value is unknown, and
	// for the same reason: false is "boost is off", a claim about the machine,
	// and a client showing a switch has to be able to render "I do not know".
	// A device with no boost control omits it too, so a client that hides the
	// control on absence is right on both counts.
	CPUBoost    *bool `json:"cpu_boost,omitempty"`
	OnAC        bool  `json:"on_ac"`                  // true when running on mains power
	SourceKnown bool  `json:"source_known,omitempty"` // true when OnAC reflects a real reading; false = unknown, not battery
	Temperature int   `json:"temperature,omitempty"`  // APU temp, degrees Celsius
	FanRPM      int   `json:"fan_rpm,omitempty"`      // fan1 speed in RPM

	// RPM is every fan the device reports, in the driver's order; FanRPM is
	// RPM[0]. Both are carried because they answer different questions and one
	// of them is a compatibility surface: FanRPM predates multi-fan support and
	// every pre-2.0 client reads it, so it is served forever, while a machine
	// with two fans cooling the same die is misdescribed by either one of them
	// alone — the Z13's fans routinely differ by several hundred RPM, and a
	// header quoting only the quieter one reads as a stopped fan.
	//
	// The same values a TelemetrySample carries, from the same driver call; this
	// is the live edge of the series the dashboard plots.
	RPM []int `json:"rpm,omitempty"`

	// PackagePowerW is the current CPU package draw in watts, absent on a
	// device that reports none. See TelemetrySample.PackagePowerW — the value
	// here is the same quantity and, on a device whose hardware offers a
	// cumulative energy counter rather than instantaneous power, literally the
	// same number: the conversion needs two readings taken a known interval
	// apart, so the daemon serves its sampler's most recent figure rather than
	// deriving a second one from a different baseline. Two derivations would
	// disagree, and a header disagreeing with the right-hand edge of the chart
	// beside it is indistinguishable from a bug.
	//
	// Absent rather than stale: a figure the sampler has not refreshed recently
	// (it stands down across a suspend) is omitted, because a reading labelled
	// "now" that describes ten hours ago is worse than no reading.
	PackagePowerW float64 `json:"package_power_w,omitempty"`

	// BatteryPowerW is battery flow in watts: positive while discharging,
	// negative while charging. A pointer for the same reason
	// TelemetrySample.BatteryPowerW is one — zero is a real reading here, so
	// absent and zero must be distinguishable.
	BatteryPowerW *float64 `json:"battery_power_w,omitempty"`

	// Telemetry is the full live edge of the series telemetry-history plots:
	// the most recent sample, every expanded quantity included. The named
	// fields above remain forever as the vocabulary pre-2.0 clients read;
	// this carries what a client showing the expanded readouts (GPU, load,
	// clocks, memory, NPU) needs without a field-per-quantity forever. Absent
	// rather than stale, on PackagePowerW's terms — a reading labelled "now"
	// that describes ten hours ago is worse than no reading.
	Telemetry *TelemetrySample `json:"telemetry,omitempty"`

	// Features is every firmware toggle the device offers, keyed by the same id
	// DeviceInfo.Toggles and the feature commands use.
	//
	// It exists so a client can render toggle rows generically from the device
	// document. BootSound and PanelOverdrive above are the same two values
	// under fixed names: they are the whole vocabulary a pre-2.0 client knows,
	// so they are served forever, but a device with a different set of toggles
	// cannot be described by them and a generic row had no way to learn its own
	// state without one socket round trip per toggle per refresh.
	//
	// A toggle whose current value cannot be read is omitted rather than
	// reported as zero — zero is "off", which is a claim about the hardware.
	Features map[string]int `json:"features,omitempty"`

	// PendingReboot reports whether a changed firmware setting is waiting on a
	// restart to take effect.
	//
	// A pointer for the same reason CPUBoost is one: absent means the device
	// cannot say, which is not "nothing is pending" and must not be rendered as
	// it. A pre-2.0 daemon, or a device whose firmware interface has no such
	// flag, omits the field entirely.
	//
	// It exists because a BIOS setting that silently needs a reboot looks
	// exactly like one that did not work — the switch moves, the machine does
	// not change, and nothing on screen accounts for the gap.
	PendingReboot *bool `json:"pending_reboot,omitempty"`

	// BatteryLevel is the pack's current charge as a percentage, zero when the
	// device has no battery.
	//
	// Note the neighbour it is easily confused with: `Battery` above is the
	// charge *limit* (the end threshold voltaire writes), which is a setting,
	// while this is the reading. They are routinely different numbers and the
	// interesting case is when the level sits above the limit — see
	// BatteryState.
	BatteryLevel int `json:"battery_level,omitempty"`

	// BatteryState is what the pack is doing: "charging", "discharging",
	// "full", "not-charging", or absent when the device does not say.
	//
	// It exists because BatteryPowerW is not self-explaining, and on a machine
	// with a charge limit the commonest reading is the confusing one: a pack
	// resting above its threshold on mains reports exactly 0 W, correctly,
	// because nothing is moving. Without a state beside it that is
	// indistinguishable from a dead sensor — which is how it was first
	// reported. "not-charging" is deliberately distinct from "full": it means
	// the pack could charge and is being held back, which together with
	// BatteryLevel and Battery lets a client say "81%, holding at your 75%
	// limit" instead of showing a bare zero.
	BatteryState string `json:"battery_state,omitempty"`

	// Charger names which power input is supplying the machine: "adapter"
	// (a proprietary high-wattage DC input), "usb-c", or "none". Absent when
	// the device cannot say, which includes every device with only one way to
	// take power.
	//
	// It is not derivable from OnAC and does not replace it. The Z13 takes power
	// two ways and its Mains supply reads online for *both* — correctly, since
	// mains power is attached either way — so OnAC answers "is it plugged in"
	// and this answers "into what". The two inputs have very different ceilings,
	// which is what makes the distinction worth carrying.
	Charger string `json:"charger,omitempty"`

	// BatteryHealth is full-charge capacity as a percentage of design
	// capacity, or zero when the device does not report it — which is what
	// DeviceInfo.Battery.Health says in advance, so a client knows whether to
	// show the reading before it has one. It is read on demand here rather
	// than sampled into the telemetry ring: it moves over months, not seconds.
	//
	// It is not clamped to 100; a freshly calibrated pack genuinely reads
	// slightly above its design capacity.
	BatteryHealth int `json:"battery_health,omitempty"`

	// BatteryEnergyWh and BatteryEnergyFullWh are the pack's remaining and
	// full-charge energy in watt-hours: the pair that turns BatteryPowerW into
	// a time estimate — remaining over rate while discharging, the gap to the
	// charge target over rate while charging (the target being the Battery
	// limit when one is set, else the full charge).
	//
	// Watt-hours rather than a second health-style ratio because Wh is
	// portable once the driver has converted a charge-reporting pack through
	// its voltage, and the estimate needs the absolute size — a percentage
	// cannot become hours without it. Both zero when the pack reports neither
	// energy form; they always travel as a pair.
	BatteryEnergyWh     float64 `json:"battery_energy_wh,omitempty"`
	BatteryEnergyFullWh float64 `json:"battery_energy_full_wh,omitempty"`
}

// TelemetrySample is one reading from the daemon's sample history, as returned
// by SendTelemetryHistory. The daemon samples at 1 Hz.
//
// At is Unix seconds — compact enough to carry a few hundred of them, and
// unambiguous, which a relative offset would not be across the gap a suspend
// leaves. Samples are not evenly spaced: the sampler stands down while the
// machine is suspending and skips a failed read rather than recording a zero,
// so a client must plot against At and never against the slice index.
//
// A field that the device does not report is omitted rather than zeroed.
// PackagePowerW in particular is absent on any device whose
// DeviceInfo.Telemetry.PowerDraw is empty, which is the signal to hide the
// power graph instead of drawing it flat.
type TelemetrySample struct {
	At            int64   `json:"at"`
	TempC         int     `json:"temp_c,omitempty"`
	RPM           []int   `json:"rpm,omitempty"`
	PackagePowerW float64 `json:"package_power_w,omitempty"`

	// BatteryPowerW is battery flow in watts: positive while discharging,
	// negative while charging.
	//
	// It is a pointer because zero is a real reading here and every other
	// field in this struct is omitempty — a full pack on mains moves no
	// energy, so a plain float64 would make a laptop at 100% indistinguishable
	// from a desktop with no battery, and a client would have to choose
	// between hiding a true reading and drawing a chart flat at zero for a
	// machine that has no pack. nil means the device reported none. Same
	// reasoning as BatteryInfo.ChargeLimit being a *bool.
	BatteryPowerW *float64 `json:"battery_power_w,omitempty"`

	// BatteryLevelPct is the pack's state of charge as a percentage — the
	// quantity the battery chart plots, with BatteryPowerW as the rate behind
	// its header. A pointer on the same terms as the utilisations: zero is a
	// real reading (a flat pack), and nil means no pack reported one.
	BatteryLevelPct *int `json:"battery_level_pct,omitempty"`

	// The expanded quantities, present only on devices whose
	// DeviceInfo.Telemetry declares the matching source (gpu, cpu_stats,
	// npu, net). Pointer fields are the ones whose zero is a real reading —
	// an idle CPU is genuinely at 0%, a GFXOFF'd GPU and a runtime-suspended
	// NPU genuinely draw ~0 W, an idle link moves 0.0 MB/s — exactly
	// BatteryPowerW's reasoning. Plain fields omit their zero because it is
	// never a measurement (a 0°C die, a 0 MHz clock, an empty memory gauge).
	// Network throughput is decimal megabytes per second, received and
	// transmitted summed over the machine's physical interfaces.
	GPUTempC    int      `json:"gpu_temp_c,omitempty"`
	CPUUtilPct  *int     `json:"cpu_util_pct,omitempty"`
	GPUUtilPct  *int     `json:"gpu_util_pct,omitempty"`
	NPUUtilPct  *int     `json:"npu_util_pct,omitempty"`
	GPUPowerW   *float64 `json:"gpu_power_w,omitempty"`
	NPUPowerW   *float64 `json:"npu_power_w,omitempty"`
	NetRxMBps   *float64 `json:"net_rx_mbps,omitempty"`
	NetTxMBps   *float64 `json:"net_tx_mbps,omitempty"`
	CPUClockMHz int      `json:"cpu_clock_mhz,omitempty"`
	GPUClockMHz int      `json:"gpu_clock_mhz,omitempty"`
	MemClockMHz int      `json:"mem_clock_mhz,omitempty"`
	NPUClockMHz int      `json:"npu_clock_mhz,omitempty"`
	VRAMUsedMB  int      `json:"vram_used_mb,omitempty"`
	VRAMTotalMB int      `json:"vram_total_mb,omitempty"`
	MemUsedMB   int      `json:"mem_used_mb,omitempty"`
	MemTotalMB  int      `json:"mem_total_mb,omitempty"`
}

// StockProfiles are the firmware performance profiles that can be written to
// platform_profile. They are reserved: a custom profile can never take one of
// these names, so selecting one always reaches the firmware profile.
var StockProfiles = []string{"quiet", "balanced", "performance"}

// DefaultCustomProfile is the name of the custom profile created implicitly by
// the first fan curve, TDP, or undervolt setting made while a stock profile is
// active. It is reserved and cannot be chosen as a user-supplied name.
const DefaultCustomProfile = "custom"

// IsStockProfileName reports whether name is one of the reserved firmware
// profile names.
func IsStockProfileName(name string) bool {
	for _, p := range StockProfiles {
		if name == p {
			return true
		}
	}
	return false
}

// CustomProfile is a named set of custom hardware settings. Each subsystem is a
// pointer so that nil means "this profile does not control that subsystem",
// which is what lets a profile stay loadable as new subsystems are added.
type CustomProfile struct {
	Name      string          `json:"name"`
	FanCurve  *FanCurveState  `json:"fan_curve,omitempty"`
	TDP       *TDPState       `json:"tdp,omitempty"`
	Undervolt *UndervoltState `json:"undervolt,omitempty"`
}

// Empty reports whether the profile controls no subsystem at all. An empty
// profile cannot be activated: there would be nothing to apply.
func (p CustomProfile) Empty() bool {
	return p.FanCurve == nil && p.TDP == nil && p.Undervolt == nil
}

// AutoswitchState configures automatic profile selection by power source.
// An empty AC or Battery target means "leave the profile alone on that source",
// which is how a caller hands one side back to power-profiles-daemon.
type AutoswitchState struct {
	Enabled bool   `json:"enabled"`
	AC      string `json:"ac,omitempty"`
	Battery string `json:"battery,omitempty"`
}

// Target returns the profile to apply for the given power source, or "" when
// autoswitch is disabled or that side is unconfigured.
func (a *AutoswitchState) Target(onAC bool) string {
	if a == nil || !a.Enabled {
		return ""
	}
	if onAC {
		return a.AC
	}
	return a.Battery
}

// IsCustomProfile reports whether name identifies a voltaire-managed custom
// profile: the default "custom" profile, or a saved named one.
//
// Clients that check Profile == "custom" to decide whether custom controls
// apply must move to this — a named profile would otherwise read as stock.
//
// A reserved firmware profile name is never custom, whatever the map contains.
// The check is deliberately ahead of the lookup so that a hand-edited state file
// cannot make a stock profile look custom to the fan curve reconciler.
func (s State) IsCustomProfile(name string) bool {
	if name == "" || IsStockProfileName(name) {
		return false
	}
	if name == DefaultCustomProfile {
		return true
	}
	_, ok := s.CustomProfiles[name]
	return ok
}

// InCustomProfile reports whether the active profile is a custom one.
func (s State) InCustomProfile() bool { return s.IsCustomProfile(s.Profile) }

// ActiveCustomProfile returns the active custom profile and true, or the zero
// value and false when a stock profile is active.
func (s State) ActiveCustomProfile() (CustomProfile, bool) {
	if !s.InCustomProfile() {
		return CustomProfile{}, false
	}
	p, ok := s.CustomProfiles[s.Profile]
	if !ok {
		// "custom" is addressable before it has ever been populated.
		return CustomProfile{Name: s.Profile}, true
	}
	return p, true
}

// LightingState captures all parameters needed to reproduce one lighting zone.
type LightingState struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	Color      string `json:"color"`  // "RRGGBB" hex
	Color2     string `json:"color2"` // "RRGGBB" hex
	Speed      string `json:"speed"`
	Brightness int    `json:"brightness"` // 0–3
}

// FanCurvePoint represents one point on an 8-point fan curve.
type FanCurvePoint struct {
	Temp int `json:"temp"` // degrees Celsius
	PWM  int `json:"pwm"`  // 0–255 duty cycle
}

// FormatFanCurve renders points in the "temp:pwm,temp:pwm,..." form that
// SendFanCurveSet and the fancurve command take. It is the inverse of the
// daemon's own parser and lives here so that a client holding a curve — a
// FanPreset's, or one it built — never has to restate the wire format.
func FormatFanCurve(points []FanCurvePoint) string {
	parts := make([]string, 0, len(points))
	for _, p := range points {
		parts = append(parts, strconv.Itoa(p.Temp)+":"+strconv.Itoa(p.PWM))
	}
	return strings.Join(parts, ",")
}

// FanCurveState captures the fan curve and mode applied to both fans.
type FanCurveState struct {
	Mode   int             `json:"mode"`   // pwm_enable: 0=full-speed, 1=custom, 2=auto
	Points []FanCurvePoint `json:"points"` // 8 points
}

// UndervoltState captures the AMD Curve Optimizer offset applied to the CPU.
// Values are non-positive integers (0 = stock, negative = undervolt).
// Active indicates whether the offset is currently applied to hardware.
type UndervoltState struct {
	CPUCO  int  `json:"cpu_co"` // all-core CPU Curve Optimizer offset
	Active bool `json:"active"` // true when CO is applied to hardware
}

// TDPState captures all PPT (Package Power Tracking) values in watts.
type TDPState struct {
	PL1SPL       int `json:"pl1_spl"`       // Sustained Power Limit
	PL2SPPT      int `json:"pl2_sppt"`      // Short Boost
	FPPT         int `json:"fppt"`          // Fast Boost
	APUSPPT      int `json:"apu_sppt"`      // APU Short PPT
	PlatformSPPT int `json:"platform_sppt"` // Platform Short PPT
}
