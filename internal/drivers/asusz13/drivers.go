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

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
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

// NewBattery returns the power_supply battery driver with the capabilities
// device data declares.
func NewBattery(caps driver.BatteryCaps) driver.Battery {
	return battery{caps: caps}
}

type battery struct{ caps driver.BatteryCaps }

func (b battery) Caps() driver.BatteryCaps { return b.caps }

func (battery) ChargeLimit() (int, error) { return readIntFile(FindBatteryThresholdPath()) }

func (battery) SetChargeLimit(percent int) error {
	return writeIntFile(FindBatteryThresholdPath(), percent)
}

func (b battery) Status() (driver.BatteryStatus, error) {
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
	// Best-effort, on the same terms as health: a pack whose `status` cannot be
	// read must not make the charge level unreadable.
	st.State = ReadBatteryState()
	// Health is best-effort for the same reason RPM is in Sample: a pack whose
	// full-charge attributes are missing must not make the charge level
	// unreadable. Zero is the documented "not reported".
	if b.caps.Health {
		if health, err := ReadBatteryHealthPercent(); err == nil {
			st.HealthPercent = health
		}
	}
	// The energy pair backs the client-side time estimate, and is undeclared
	// best-effort like the flow and state it annotates — the three are halves
	// of one feature, and gating one behind a capability the others do not
	// have would serve an estimate with no rate beside it or the reverse.
	if now, full, err := ReadBatteryEnergyWh(); err == nil {
		st.EnergyWh, st.EnergyFullWh = now, full
	}
	return st, nil
}

// NewTelemetry returns the telemetry source described by device data: APU
// temperature and both fan speeds from hwmon, the package energy counter from
// powercap RAPL, and battery flow plus state of charge from power_supply.
func NewTelemetry(info driver.TelemetryInfo) driver.Telemetry {
	return telemetry{info: info}
}

type telemetry struct{ info driver.TelemetryInfo }

func (t telemetry) Info() driver.TelemetryInfo { return t.info }

// Sample reads every quantity this device measures.
//
// Only the temperature is required. Everything else is best-effort, for the
// reason RPM already was: a missing hwmon, a powercap grant the user has not
// run setup for, or a battery that reports no power must not make the
// temperature unreadable — a sample that fails is a *gap* in the history,
// which costs every series and not just the one that could not be read.
func (t telemetry) Sample() (driver.Sample, error) {
	var s driver.Sample
	temp, err := ReadAPUTemperature()
	if err != nil {
		return s, err
	}
	s.TempC = temp
	if rpms, err := ReadBothFanRPM(); err == nil {
		s.RPM = rpms[:]
	}
	// The counter, not a power figure: converting needs the previous reading,
	// and this driver holds no state (see driver.Sample).
	if energy, maxUJ, err := ReadPackageEnergy(); err == nil {
		s.PackageEnergyUJ, s.PackageEnergyMaxUJ = energy, maxUJ
	}
	if watts, err := ReadBatteryPowerW(); err == nil {
		s.BatteryPowerW, s.BatteryPowerKnown = watts, true
	}
	if pct, err := readIntFile(FindBatteryCapacityPath()); err == nil {
		s.BatteryLevelPct, s.BatteryLevelKnown = pct, true
	}

	// The expanded sources, each gated on its declaration: a capability the
	// document does not claim must not be read, or the guard that keeps
	// declared-and-read in lockstep loses one of its directions.
	if t.info.GPU != "" {
		if v, err := ReadGPUTempC(); err == nil {
			s.GPUTempC = v
		}
		if v, err := ReadGPUBusyPct(); err == nil {
			s.GPUBusyPct, s.GPUBusyKnown = v, true
		}
		if v, err := ReadGPUClockMHz(); err == nil {
			s.GPUClockMHz = v
		}
		if w, uclk, err := ReadGPUMetrics(); err == nil {
			s.GPUPowerW, s.GPUPowerKnown = w, true
			s.MemClockMHz = uclk
		}
		if used, total, err := ReadVRAMMB(); err == nil {
			s.VRAMUsedMB, s.VRAMTotalMB = used, total
		}
	}
	if t.info.CPUStats != "" {
		// Counters, not a percentage — the sampler derives CPUUtilPct, on the
		// energy-counter pattern.
		if busy, total, err := ReadCPUJiffies(); err == nil {
			s.CPUBusyJiffies, s.CPUTotalJiffies = busy, total
		}
		if v, err := ReadCPUClockMHz(); err == nil {
			s.CPUClockMHz = v
		}
		if used, total, err := ReadMemoryMB(); err == nil {
			s.MemUsedMB, s.MemTotalMB = used, total
		}
	}
	if t.info.NPU != "" {
		if w, util, clock, known := ReadNPUTelemetry(); known {
			s.NPUPowerW, s.NPUBusyPct, s.NPUClockMHz, s.NPUKnown = w, util, clock, true
		}
	}
	if t.info.Net != "" {
		// Counters, not a rate — the sampler derives NetRxMBps/NetTxMBps, on
		// the energy-counter pattern.
		if rx, tx, err := ReadNetBytes(); err == nil {
			s.NetRxBytes, s.NetTxBytes = rx, tx
		}
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
