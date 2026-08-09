package asusz13

// drivers.go — the driver.* implementations over this package's sysfs layer.
// Constructors are pure (no I/O); hardware is touched only when a method runs.
// The registry wiring lives in the register subpackage so this package does
// not import internal/device — which keeps this package's own tests free to
// pull the authoritative Z13 envelope from the embedded device data.

import (
	"fmt"
	"os"
	"strings"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/driver"
)

// NewFanController returns the asus-nb-wmi fan driver with the given shape
// (from device data).
func NewFanController(shape driver.FanShape) driver.FanController {
	return fanController{shape: shape}
}

type fanController struct{ shape driver.FanShape }

func (f fanController) Shape() driver.FanShape { return f.shape }

func (fanController) ReadRPM() ([]int, error) {
	rpms, err := ReadBothFanRPM()
	return rpms[:], err
}

// ReadMode reports the curve device's pwm_enable folded to one value: custom
// (1) only when every readable fan reports the curve active — the
// VerifyFanCurveActive rule — otherwise the first readable non-custom mode. A
// curve half-dropped by the kernel therefore reads as "not live", which is the
// answer the reconcile watcher needs to act on.
//
// Unreadable channels (-1) are skipped, exactly as VerifyFanCurveActive skips
// them: a SKU that exposes only the CPU curve channel is a supported
// configuration, and folding its permanently-unreadable second channel into
// the answer would leave the reconcile watcher and the sleep hook blind on the
// channel the machine does have. Only when no channel is readable at all does
// this return -1, meaning unknown.
func (fanController) ReadMode() (int, error) {
	modes, err := ReadFanCurveModes()
	if err != nil {
		return 0, err
	}
	mode := -1
	for _, m := range modes {
		if m == -1 {
			continue
		}
		if m != 1 {
			return m, nil
		}
		mode = 1
	}
	return mode, nil
}

func (fanController) ApplyCurve(pts []api.FanCurvePoint) error { return SetBothFanCurves(pts) }

func (fanController) Release() error { return ResetAllFanCurves() }

// LiveCurve returns the curve programmed into the curve registers whether or
// not it is active — the interface's contract, since the registers survive a
// release on the Z13. "The curve in force" is a composition the caller makes
// from this plus ReadMode (the no-daemon CLI's liveFanCurve helper is exactly
// that composition).
func (fanController) LiveCurve() ([]api.FanCurvePoint, error) {
	curves, err := ReadBothFanCurves()
	if err != nil {
		return nil, err
	}
	return curves[0], nil
}

// NewPowerLimiter returns the asus-nb-wmi PPT driver, whose envelope comes
// from device data — the single source of the Z13's power numbers.
func NewPowerLimiter(env driver.PowerEnvelope) driver.PowerLimiter {
	return powerLimiter{env: env}
}

type powerLimiter struct{ env driver.PowerEnvelope }

func (powerLimiter) Read() (api.TDPState, error)      { return ReadAllPPT() }
func (powerLimiter) Apply(s api.TDPState) error       { return SetTDPState(s) }
func (l powerLimiter) Envelope() driver.PowerEnvelope { return l.env }

// NewProfileController returns the platform-profile driver. names are the
// firmware profile names from device data — also the reserved names.
func NewProfileController(names []string) driver.ProfileController {
	return profileController{names: names}
}

type profileController struct{ names []string }

func (p profileController) Names() []string {
	out := make([]string, len(p.names))
	copy(out, p.names)
	return out
}

// Get reads the primary platform_profile attribute raw, exactly as every
// caller does today; name mapping applies on write (SetProfile), not read.
func (profileController) Get() (string, error) {
	data, err := os.ReadFile(FindProfilePath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (profileController) Set(name string) error { return SetProfile(name) }

// NewToggles returns the asus-armoury firmware-toggles driver for the given
// specs. It errors on a toggle id this driver has no attribute mapping for —
// device data naming an unknown toggle is a data bug surfaced at assembly, not
// a control that silently does nothing.
func NewToggles(specs []driver.ToggleSpec) (driver.Toggles, error) {
	for _, s := range specs {
		if _, ok := togglePaths[s.ID]; !ok {
			return nil, fmt.Errorf("unknown toggle id %q", s.ID)
		}
	}
	out := make([]driver.ToggleSpec, len(specs))
	copy(out, specs)
	return toggles{specs: out}, nil
}

// togglePaths maps toggle ids to their firmware-attribute accessors.
var togglePaths = map[string]struct {
	find func() string
	set  func(int) error
}{
	"boot_sound":      {FindBootSoundPath, SetBootSound},
	"panel_overdrive": {FindPanelOverdrivePath, SetPanelOverdrive},
}

// FindTogglePath returns the sysfs path a toggle id writes to, or "" for an id
// this driver has no mapping for. For dry-run display: a dry run's job is to
// spell out the exact write, and the path is this driver's own knowledge.
func FindTogglePath(id string) string {
	p, ok := togglePaths[id]
	if !ok {
		return ""
	}
	return p.find()
}

type toggles struct{ specs []driver.ToggleSpec }

func (t toggles) List() []driver.ToggleSpec {
	out := make([]driver.ToggleSpec, len(t.specs))
	copy(out, t.specs)
	return out
}

func (t toggles) Get(id string) (int, error) {
	p, ok := togglePaths[id]
	if !ok {
		return 0, driver.ErrUnsupported
	}
	return readIntFile(p.find())
}

func (t toggles) Set(id string, value int) error {
	p, ok := togglePaths[id]
	if !ok {
		return driver.ErrUnsupported
	}
	return p.set(value)
}

// NewBattery returns the power_supply battery driver.
func NewBattery() driver.Battery {
	return battery{}
}

type battery struct{}

func (battery) ChargeLimit() (int, error) { return readIntFile(FindBatteryThresholdPath()) }

func (battery) SetChargeLimit(percent int) error {
	return writeIntFile(FindBatteryThresholdPath(), percent)
}

func (battery) Status() (driver.BatteryStatus, error) {
	var st driver.BatteryStatus
	pct, err := readIntFile(FindBatteryCapacityPath())
	if err != nil {
		return st, err
	}
	st.Capacity = pct
	// A missing Mains supply is unknown, never "on battery" — ACKnown carries
	// the distinction OnACPower's error return expresses.
	if on, err := OnACPower(); err == nil {
		st.OnAC, st.ACKnown = on, true
	}
	return st, nil
}

// NewTelemetry returns the hwmon-based telemetry source. Package power (RAPL /
// pm-table) is not read yet and reports zero; it lands with the dashboard
// work, which is what consumes it.
func NewTelemetry() driver.Telemetry {
	return telemetry{}
}

type telemetry struct{}

func (telemetry) Sample() (driver.Sample, error) {
	var s driver.Sample
	temp, err := ReadAPUTemperature()
	if err != nil {
		return s, err
	}
	s.TempC = temp
	// RPM is best-effort: a missing hwmon must not make temperature
	// unavailable, mirroring how status treats the two today.
	if rpms, err := ReadBothFanRPM(); err == nil {
		s.RPM = rpms[:]
	}
	return s, nil
}

// NewUndervolter returns the ryzen_smu Curve Optimizer driver with bounds from
// device data. All the destructive-probe caveats on SMUProbeUndervolt apply.
func NewUndervolter(lo, hi int) driver.Undervolter {
	return undervolter{lo: lo, hi: hi}
}

type undervolter struct{ lo, hi int }

// Present is the stat-only check: the ryzen_smu sysfs interface exists. It
// says nothing about whether CO commands work on this platform — that is
// ProbeAvailable's (destructive) question.
func (undervolter) Present() bool         { return SMUAvailable() }
func (undervolter) ProbeAvailable() bool  { return SMUProbeUndervolt() }
func (u undervolter) Range() (lo, hi int) { return u.lo, u.hi }

// Apply validates against the device data's bounds before anything else: even
// the availability probe inside SetCurveOptimizer is a hardware write, and a
// rejected value must not reach it.
func (u undervolter) Apply(cpuCO int) error {
	if cpuCO < u.lo || cpuCO > u.hi {
		return fmt.Errorf("CPU undervolt %d out of range %d to %d", cpuCO, u.lo, u.hi)
	}
	return SetCurveOptimizer(cpuCO)
}

func (undervolter) Reset() error { return ResetCurveOptimizer() }
