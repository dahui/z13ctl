// Package driver defines the per-hardware-class interfaces the daemon and CLI
// program against, so that device support is a set of implementations selected
// by device data rather than calls into one device's sysfs layout.
//
// Drivers are deliberately dumb. A driver implementation must not start
// goroutines, must not hold locks across calls, and must not contain policy —
// serialization stays with the daemon's hwMu, and every safety rule (the
// high-TDP fan floor above all) lives in the safety engine that sits between
// handlers and these interfaces. A driver that needs to "decide" something has
// the wrong shape: it reports facts (its envelope, its shape, its readings)
// and performs writes it was handed.
//
// Interfaces are per hardware class, not per device. A device is an assembly
// of implementations chosen by the device registry from DMI-matched device
// data; a nil interface on the assembled device means the capability does not
// exist there, which is also exactly what the socket API reports to clients —
// capability discovery by absence, never by error.
package driver

import (
	"context"
	"errors"
	"fmt"

	"github.com/dahui/voltaire/api/v2"
)

// ErrUnsupported reports that a device's driver does not implement an optional
// method. Callers treat it as "capability absent", not as a failure: a missing
// reading is skipped, a missing control is not offered. It is distinct from a
// nil interface — a driver may support a class partially (RPM readable, mode
// not) and this sentinel is how it says so per method.
var ErrUnsupported = errors.New("driver: not supported on this device")

// FanShape describes a device's fan-curve envelope: how many points a curve
// holds and the axes they live on. The GUI's editor and the daemon's parser
// both derive their bounds from this rather than from constants.
type FanShape struct {
	Points  int // points per curve (8 on the Z13)
	TempMin int // curve temperature axis, Celsius
	TempMax int
	PWMMax  int // hwmon PWM ceiling (255)
}

// FanController drives a device's fan-curve hardware.
//
// Two contracts carry over from the Z13 implementation and are not optional,
// because the callers' safety reasoning depends on them:
//
//   - ApplyCurve MUST read back and verify the curve is active before
//     returning nil (the Z13's VerifyFanCurveActive): a platform_profile write
//     from any other process silently drops a curve, and a false success here
//     is what turns the fan floor into a fiction.
//   - Release MUST verify the release the same way (verifyFanModeReleased).
//     The pre-sleep release depends on it — an unnoticed failure is a machine
//     that runs its fans through the whole suspend.
type FanController interface {
	Shape() FanShape

	// ReadRPM returns the current speed of each fan.
	ReadRPM() ([]int, error)

	// ReadMode returns the pwm_enable state: 0 full speed, 1 custom curve,
	// 2 firmware auto. This is the reconcile watcher's ground truth.
	ReadMode() (int, error)

	// ApplyCurve writes pts to every fan and verifies it took effect.
	ApplyCurve(pts []api.FanCurvePoint) error

	// Release hands the fans back to firmware auto and verifies it took effect.
	Release() error

	// LiveCurve returns the curve currently programmed, whether or not it is
	// active — curve registers survive a release on the Z13.
	LiveCurve() ([]api.FanCurvePoint, error)
}

// FanModeName returns a human-readable name for a FanController.ReadMode
// value. It lives with the interface because the 0/1/2 semantics are the
// interface's own contract, not any one device's.
func FanModeName(mode int) string {
	switch mode {
	case 0:
		return "full-speed"
	case 1:
		return "custom"
	case 2:
		return "auto"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
}

// PowerEnvelope is a device's power-limit envelope: the bounds the daemon
// validates against, the per-profile firmware defaults it restores, and the
// fan floor the safety engine enforces above TDPMaxSafe. It is data, not
// policy — the safety engine owns what these numbers mean.
type PowerEnvelope struct {
	TDPMin       int // lowest legal sustained limit, watts
	TDPMaxSafe   int // above this the force flag and the fan floor apply
	TDPMaxForced int // absolute ceiling, force flag or not

	// StockProfilePPT maps each firmware profile name to the PPT values the
	// daemon writes when that profile is selected. Authoritative on write: the
	// firmware does not restore these itself (z13ctl issue #12).
	StockProfilePPT map[string]api.TDPState

	// FloorCurve is the per-point fan floor enforced while the sustained limit
	// exceeds TDPMaxSafe, measured at each user point's temperature. Empty
	// means the device imposes no floor. On the Z13 this is the high-TDP
	// curve: a 50% bottom ramping to full speed at 80°C.
	FloorCurve []api.FanCurvePoint
}

// PowerLimiter reads and writes a device's power limits.
//
// Apply is a raw hardware write with no safety checks; nothing outside the
// safety engine may call it. The device registry enforces that structurally —
// the assembled device exposes the safety engine, not this interface — so a
// new caller reaching for Apply directly is a design error, not a shortcut.
type PowerLimiter interface {
	Read() (api.TDPState, error)
	Apply(api.TDPState) error
	Envelope() PowerEnvelope
}

// Lighting drives a device's RGB zones.
//
// Reopen and Present exist for hot-pluggable lighting hardware: the Z13's
// detachable keyboard powers off when removed and reappears as a new hidraw
// node, so the hotplug watcher polls Present and calls Reopen on an
// absent-to-present transition. Devices without detachable lighting return
// true and nil respectively.
type Lighting interface {
	// Zones returns the addressable zone names ("keyboard", "lightbar").
	Zones() []string

	// Apply writes a lighting state to one zone; an empty zone means all.
	Apply(zone string, ls api.LightingState) error

	// Off turns one zone off; an empty zone means all.
	Off(zone string) error

	// SetBrightness changes only the brightness of one zone (empty means all),
	// leaving the running effect untouched — a full Apply would restart the
	// effect's animation. Level 0 also powers the zone down, and any non-zero
	// level powers it up, matching the Aura brightness-off semantics.
	SetBrightness(zone string, level int) error

	// Present reports whether the lighting hardware is currently attached.
	// Sysfs-only: it must not open the device.
	Present() bool

	// Reopen re-discovers and reopens the underlying device, replacing any
	// stale handle. Called under the daemon's device lock.
	Reopen() error
}

// ProfileController reads and writes the platform performance profile.
//
// Names is the source of the reserved profile names: a name it returns can
// never be used for a custom profile, and only a name it returns may ever be
// written to the platform. Both invariants predate this interface (the Z13's
// quiet/balanced/performance) and every implementation inherits them.
type ProfileController interface {
	Names() []string
	Get() (string, error)
	Set(name string) error
}

// ToggleKind says how a firmware toggle's value is shaped. Only booleans exist
// today; the kind exists so an enumerated toggle is an additive change.
type ToggleKind string

// ToggleBool is a 0/1 firmware switch.
const ToggleBool ToggleKind = "bool"

// ToggleSpec describes one firmware toggle a device offers, in the form the
// capability API serves to clients: stable ID for the wire, human label for
// fallback display.
type ToggleSpec struct {
	ID    string // wire identifier: "boot_sound", "panel_overdrive"
	Label string // human-readable: "POST boot sound"
	Kind  ToggleKind

	// Description is the prose a UI shows beside the control — what the toggle
	// does, and any consequence worth warning about ("may cause ghosting").
	//
	// It is here rather than left to each client because it is device
	// knowledge, not presentation: whether panel overdrive ghosts is a fact
	// about the panel, and a client rendering rows generically from this list
	// has no way to know it. Without the field the only options were to drop
	// the warnings or to hardcode per-id prose in every UI, which is the same
	// duplication api.ValidateProfileName exists to prevent. Optional — a
	// toggle whose label says everything leaves it empty.
	Description string

	// Source is where this toggle comes from: "core" for one a compiled-in
	// driver provides, "plugin:<id>" for one contributed by an external plugin.
	//
	// Empty means "core" — the daemon fills it in on the way to the wire, so a
	// driver that predates plugins needs no change and a client never has to
	// treat absence as a third case. Every toggle is core today; the field is
	// carried now because a client that groups rows by provenance has to be
	// able to, and unlike a Kind with no renderer this value is always true and
	// complete, so it misleads nobody in the meantime.
	Source string
}

// ToggleSourceCore is the Source of a toggle provided by a compiled-in driver.
// Plugin-provided toggles use "plugin:<id>".
const ToggleSourceCore = "core"

// Toggles reads and writes a device's firmware toggles (BIOS switches exposed
// through firmware-attributes or equivalent). The set is discovered per
// device; clients render controls from List, so an ID must stay stable for
// the life of the device data that declares it.
type Toggles interface {
	List() []ToggleSpec
	Get(id string) (int, error)
	Set(id string, value int) error
}

// BatteryStatus is a point-in-time battery reading. Fields the hardware does
// not report are zero.
//
// OnAC is only meaningful when ACKnown is true. On machines with no Mains
// supply (VMs, desktops, a driver not yet bound) the source cannot be
// observed, and the established invariant is that unknown must never be read
// as "on battery" — a bool alone cannot say that.
//
// HealthPercent is state of health as a ratio, not the raw pair it is computed
// from, because the pair is not portable: power_supply reports either
// charge_full/charge_full_design in µAh or energy_full/energy_full_design in
// µWh depending on the battery driver, and the Z13's ACPI battery reports
// energy where this interface's first draft assumed charge. Only the ratio
// means the same thing on every machine, and it is the form a dashboard shows
// anyway. A driver that cannot read either pair leaves it zero.
type BatteryStatus struct {
	Capacity      int     // percent
	HealthPercent int     // full charge as a percentage of design capacity; 0 = not reported
	OnAC          bool    // mains power attached; meaningless unless ACKnown
	ACKnown       bool    // whether the power source could be observed
	PowerNowW     float64 // instantaneous draw or charge rate, watts

	// State is what the pack is doing: one of the BatteryState constants, or
	// BatteryStateUnknown when the device does not say.
	//
	// It is carried because the flow figure is not self-explaining, and on a
	// machine with a charge limit the commonest reading is the confusing one.
	// A pack sitting above its threshold on mains reports 0 W — correctly,
	// nothing is moving — and 0 W with no state beside it is indistinguishable
	// from a broken sensor. That is not hypothetical: it was reported as one.
	State BatteryState
}

// BatteryState is what a pack is doing. The values are the portable subset of
// power_supply's `status`, lowercased and hyphenated for the wire.
//
// NotCharging is the one worth knowing about: it is not an error and not
// "idle" in general, but specifically a pack that *could* charge and is being
// held back — almost always by a charge-end threshold. Distinguishing it from
// Full is what lets a client say "holding at your 75% limit" rather than
// leaving a user to conclude the reading is broken.
type BatteryState string

// The battery states. Unknown is the zero value, so a driver that does not set
// State reports it, and it is omitted from the wire.
const (
	BatteryStateUnknown     BatteryState = ""
	BatteryStateCharging    BatteryState = "charging"
	BatteryStateDischarging BatteryState = "discharging"
	BatteryStateFull        BatteryState = "full"
	BatteryStateNotCharging BatteryState = "not-charging"
)

// BatteryCaps is what a device's battery interface offers. Like every other
// capability in this package it is declared by device data rather than probed,
// so it is static for the device's lifetime and safe to serve in the device
// document without touching hardware.
//
// Both fields earn their place by being independently absent: a machine can
// report a charge level and its state of health while exposing no charge-limit
// attribute at all, and the Z13 exposes the limit while a generic fallback
// device may not.
type BatteryCaps struct {
	ChargeLimit bool // the charge end threshold can be read and written
	Health      bool // Status reports HealthPercent
}

// Battery reads and writes battery charge policy.
type Battery interface {
	Caps() BatteryCaps
	ChargeLimit() (int, error)
	SetChargeLimit(percent int) error
	Status() (BatteryStatus, error)
}

// Undervolter applies CPU Curve Optimizer offsets.
//
// The Z13 contracts carry over: offsets are write-only (no hardware readback
// exists, so state is the only record), volatile across suspend, and the
// availability probe may be destructive — ProbeAvailable is called once by the
// daemon at startup and its result cached for the process lifetime. A
// short-lived caller must never probe.
type Undervolter interface {
	// Present reports whether the undervolt interface exists on this machine
	// at all — for the Z13, whether the ryzen_smu module is loaded — without
	// touching hardware. It answers less than ProbeAvailable: presence says
	// the path could work, not that it does. It is the only availability
	// question a short-lived caller (the CLI) may ask.
	Present() bool

	// ProbeAvailable reports whether the undervolt path actually works on this
	// machine. May write to hardware; see above for who is allowed to call it.
	ProbeAvailable() bool

	// Range returns the legal offset bounds (Z13: -40 to 0).
	Range() (min, max int)

	Apply(cpuCO int) error
	Reset() error
}

// Sample is one telemetry reading.
//
// Package power arrives in one of two shapes, because the hardware offers one
// of two and converting in the driver would be wrong. A device with an
// instantaneous reading (OXP's pm-table) fills PackagePowerW. A device with a
// cumulative *energy counter* (powercap RAPL, which is what x86 offers) fills
// PackageEnergyUJ, and the caller divides the difference between two readings
// by the time between them.
//
// The conversion is the caller's because it needs the previous reading, and a
// driver that remembered one would be holding mutable state across concurrent
// calls: Sample is called by the daemon's 1 Hz sampler *and* by every get-state
// handler. Drivers are passive by design — no locking, no goroutines — so the
// counter goes out as a counter and the daemon's sampler, which is the one
// sequential caller, does the arithmetic.
type Sample struct {
	TempC int
	RPM   []int

	// PackagePowerW is an instantaneous package-power reading, zero when the
	// device reports none or reports energy instead.
	PackagePowerW float64

	// PackageEnergyUJ is a cumulative package-energy counter in microjoules,
	// zero when the device has no such counter. PackageEnergyMaxUJ is the value
	// it wraps at, so a consumer can tell a wrap from a counter reset.
	PackageEnergyUJ    uint64
	PackageEnergyMaxUJ uint64

	// BatteryPowerW is battery flow in watts: positive while the pack is
	// discharging (the machine is drawing from it), negative while charging.
	//
	// BatteryPowerKnown is what says a reading was taken at all, because unlike
	// every other quantity here zero is a *measurement*: a full pack on mains
	// genuinely moves no energy. Without the flag, a machine with no battery
	// and a laptop sitting at 100% are the same value, and a client can only
	// choose between hiding a real reading and drawing a chart flat at zero for
	// a desktop that has no pack at all.
	//
	// A bool rather than a *float64 because Sample is stored in the history
	// ring, which deep-copies in both directions precisely so a driver cannot
	// alias what it handed over; a pointer would be one more thing to remember
	// to copy, for no gain over a flag.
	BatteryPowerW     float64
	BatteryPowerKnown bool
}

// TelemetryInfo describes a telemetry source without reading it, so the device
// document can say what a dashboard may draw before any sample exists.
//
// PowerDraw names the package-power source — "rapl", "pm-table" — and is empty
// when the device has none. It is deliberately a name rather than a bool: a
// client showing provenance ("package power via RAPL") needs it, and a device
// that gains a second source later adds a name rather than a field. Leaving it
// empty is how a driver says Sample.PackagePowerW will be zero, and a driver
// must not name a source it does not actually read — capability absence is
// what makes a client hide the control, and a declared-but-unread source draws
// a flat zero line instead.
//
// HistorySeconds is how much history the daemon retains for this device, and
// so the largest window a telemetry-history request can usefully ask for. Zero
// means no history is kept; live readings still work.
type TelemetryInfo struct {
	PowerDraw      string
	HistorySeconds int
}

// Telemetry produces the readings the status command, the GUI title row, and
// the dashboard's history ring all consume.
type Telemetry interface {
	Info() TelemetryInfo
	Sample() (Sample, error)
}

// ButtonEvent is one hardware-button press. Kind names the button in device
// data terms; the daemon decides what an event means (single versus double
// press, which client event to emit).
type ButtonEvent struct {
	Kind string // "armoury-crate" on the Z13
}

// Buttons watches a device's extra hardware buttons.
//
// Watch blocks, delivering events on ch until ctx is cancelled, and must open
// the input device shared — never with an exclusive grab. The Z13's button
// node also carries the tablet-mode switch, and grabbing it exclusively broke
// keyboard reattach for the whole desktop session (issue #10); this interface
// deliberately offers no way to express a grab.
type Buttons interface {
	Watch(ctx context.Context, ch chan<- ButtonEvent) error
}
