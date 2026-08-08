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

	"github.com/dahui/z13ctl/api"
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
	// means the device imposes no floor. On the Z13 this is HighTDPFanCurve:
	// a 50% bottom ramping to full speed at 80°C.
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
}

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
// not report are zero; ChargeFull and ChargeFullDesign together give state of
// health.
//
// OnAC is only meaningful when ACKnown is true. On machines with no Mains
// supply (VMs, desktops, a driver not yet bound) the source cannot be
// observed, and the established invariant is that unknown must never be read
// as "on battery" — a bool alone cannot say that.
type BatteryStatus struct {
	Capacity         int     // percent
	ChargeFull       int     // µAh, current full-charge capacity
	ChargeFullDesign int     // µAh, design capacity
	OnAC             bool    // mains power attached; meaningless unless ACKnown
	ACKnown          bool    // whether the power source could be observed
	PowerNowW        float64 // instantaneous draw or charge rate, watts
}

// Battery reads and writes battery charge policy.
type Battery interface {
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
	// ProbeAvailable reports whether the undervolt path actually works on this
	// machine. May write to hardware; see above for who is allowed to call it.
	ProbeAvailable() bool

	// Range returns the legal offset bounds (Z13: -40 to 0).
	Range() (min, max int)

	Apply(cpuCO int) error
	Reset() error
}

// Sample is one telemetry reading. RPM is per fan; PackagePowerW is measured
// package power (RAPL or pm-table) and zero when the device has no source.
type Sample struct {
	TempC         int
	RPM           []int
	PackagePowerW float64
	Battery       BatteryStatus
}

// Telemetry produces the readings the status command, the GUI title row, and
// the dashboard's history ring all consume.
type Telemetry interface {
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
