package linux

// drivers.go — the driver.* implementations this package provides: the
// platform-profile controller, the power_supply battery, the telemetry composer
// and cpufreq boost. Each is vendor-neutral; where one needs a reading only a
// vendor driver has, the registry passes it in.

import (
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/internal/sysfsio"
)

// NewProfileController returns the platform-profile driver.
//
// names, handler, def and labels are the device data ([profiles]): names
// filters and orders what the kernel offers and is the fallback when sysfs
// cannot be read; handler is the class device that owns the profile ("" picks
// one); def is where a reset lands; labels overrides display labels. Nothing is
// read here — constructors are pure — so the kernel's list is resolved on the
// first Names call.
//
// extraPolicy names further attributes the kernel notifies when the firmware
// re-applies a profile's limits, beyond the generic ACPI one (see
// PolicyWritePaths): a vendor's own policy attribute, passed in by the
// registry so this driver need not know the vendor.
func NewProfileController(names []string, handler, def string, labels map[string]string, extraPolicy ...string) driver.ProfileController {
	return &profileController{names: names, handler: handler, def: def, labels: labels,
		extraPolicy: append([]string(nil), extraPolicy...)}
}

type profileController struct {
	names       []string
	handler     string
	def         string
	labels      map[string]string
	extraPolicy []string

	once     sync.Once
	resolved []string
}

// PolicyWritePaths names the attributes the kernel notifies when the firmware
// re-applies a profile's power limits (driver.PolicyWriteNotifier):
// /sys/firmware/acpi/platform_profile, notified after every successful profile
// write, same-value ones included (drivers/acpi/platform_profile.c), then any
// vendor attribute the registry passed in — on asus-wmi machines,
// ThrottlePolicyPath.
func (p *profileController) PolicyWritePaths() []string {
	return append([]string{sysProfileACPI}, p.extraPolicy...)
}

// Names is the kernel's offer filtered by the device data, resolved once per
// process (the handler's choices do not change while it is loaded).
func (p *profileController) Names() []string {
	p.once.Do(func() {
		p.resolved = resolveProfileNames(p.names, readChoices(FindProfilePathFor(p.handler)))
	})
	out := make([]string, len(p.resolved))
	copy(out, p.resolved)
	return out
}

// Get reads the owning handler's profile and maps it to one of Names (see
// normalizeProfile).
func (p *profileController) Get() (string, error) {
	data, err := os.ReadFile(FindProfilePathFor(p.handler))
	if err != nil {
		return "", err
	}
	return normalizeProfile(strings.TrimSpace(string(data)), p.Names())
}

// Set writes name to the platform. The daemon is what guarantees name is one
// of Names; this driver does not second-guess it.
func (p *profileController) Set(name string) error { return SetProfileFor(p.handler, name) }

// Default is the reset target from device data, or "balanced" when the data
// names none and the device offers it. A device with neither has no safe
// landing profile, and "" makes a reset refuse rather than guess.
func (p *profileController) Default() string {
	names := p.Names()
	if p.def != "" && slices.Contains(names, p.def) {
		return p.def
	}
	if p.def == "" && slices.Contains(names, "balanced") {
		return "balanced"
	}
	return ""
}

// Label is the device's label for name, else the kernel vocabulary's.
func (p *profileController) Label(name string) string {
	if l := p.labels[name]; l != "" {
		return l
	}
	return api.ProfileLabel(name)
}

// ChargerFunc names which power input is supplying the machine, given what
// power_supply already said about mains. It is how a battery driver that knows
// only power_supply takes a charger reading from vendor firmware.
type ChargerFunc func(onAC, acKnown bool) driver.Charger

// NewBattery returns the power_supply battery driver with the capabilities
// device data declares. charger reads the charger kind; nil reads it from
// power_supply itself (ReadChargerFromSupplies). The registry passes a vendor
// reader when device data names one, so this driver never names a vendor.
func NewBattery(caps driver.BatteryCaps, charger ChargerFunc) driver.Battery {
	if charger == nil {
		charger = ReadChargerFromSupplies
	}
	return battery{caps: caps, charger: charger}
}

type battery struct {
	caps    driver.BatteryCaps
	charger ChargerFunc
}

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
	// Best-effort like the rest: a machine whose firmware cannot say which
	// input is live still reports its charge level.
	st.Charger = b.charger(st.OnAC, st.ACKnown)
	return st, nil
}

// GPUTelemetry is a GPU's contribution to a telemetry sample, from whichever
// driver knows that GPU. Present must be a stat, never a read that wakes the
// device.
type GPUTelemetry interface {
	Present() bool
	MaxClockMHz() (int, error)
	Fill(s *driver.Sample)
}

// NPUTelemetry is an NPU's contribution to a telemetry sample, on the same
// terms as GPUTelemetry.
type NPUTelemetry interface {
	Present() bool
	Fill(s *driver.Sample)
}

// TelemetrySources are the vendor-specific readings a telemetry sample is
// composed from: the processor's die temperature (required — a sample without
// it fails), and the GPU and NPU when the machine has them. The registry
// passes the vendor's readers, so the composer itself names no vendor.
type TelemetrySources struct {
	Temp func() (int, error)
	GPU  GPUTelemetry
	NPU  NPUTelemetry
}

// NewTelemetry returns the telemetry source described by device data,
// composed from src: the processor temperature, the package energy counter
// from powercap RAPL, battery flow and state of charge from power_supply, and
// the GPU, CPU, NPU and network readings the data declares. What the data
// declares is checked against the machine on first use (Info).
func NewTelemetry(info driver.TelemetryInfo, src TelemetrySources) driver.Telemetry {
	return &telemetry{declared: info, src: src}
}

type telemetry struct {
	declared driver.TelemetryInfo
	src      TelemetrySources

	once sync.Once
	info driver.TelemetryInfo
}

// Info is the device data's declaration, keeping only the sources whose
// hardware is present here, plus the chart hints the kernel can give. Resolved
// once; every check is a stat or a static attribute — the NPU check in
// particular never opens the accel node, which would wake it.
//
// The device data stays the opt-in: a source it does not name is never read,
// whatever the machine has. What this adds is the other direction — a
// declared GPU on a machine without amdgpu bound, or an NPU on a SKU without
// one, is not offered, so a client does not frame a chart that can never fill.
func (t *telemetry) Info() driver.TelemetryInfo {
	t.once.Do(func() { t.info = resolveTelemetry(t.declared, t.src) })
	return t.info
}

func resolveTelemetry(d driver.TelemetryInfo, src TelemetrySources) driver.TelemetryInfo {
	out := d
	present := func(name, source string, ok bool) string {
		if source != "" && !ok {
			slog.Info("telemetry source declared but not present; not offered", "source", name, "method", source)
			return ""
		}
		return source
	}
	_, raplErr := FindRAPLPackagePath()
	out.PowerDraw = present("power_draw", d.PowerDraw, raplErr == nil)
	out.GPU = present("gpu", d.GPU, src.GPU != nil && src.GPU.Present())
	out.CPUStats = present("cpu_stats", d.CPUStats, fileExists(procStatPath))
	out.NPU = present("npu", d.NPU, src.NPU != nil && src.NPU.Present())
	out.Net = present("net", d.Net, fileExists(procNetDevPath))
	out.Fans = present("fans", d.Fans, d.Fans != "" && findFanHwmon(d.Fans) != "")

	// The clock axis spans every clock a chart shows: the CPU's boost ceiling
	// and the GPU's top P-state.
	if out.CPUStats != "" {
		if mhz, err := ReadCPUMaxClockMHz(); err == nil {
			out.ClockMaxMHz = mhz
		}
	}
	if out.GPU != "" {
		if mhz, err := src.GPU.MaxClockMHz(); err == nil && mhz > out.ClockMaxMHz {
			out.ClockMaxMHz = mhz
		}
	}
	if c, err := ReadPassiveTripC(); err == nil {
		out.TempLimitC = c
	}
	return out
}

func fileExists(path string) bool { return sysfsio.Exists(path) }

// Sample reads every quantity this device measures.
//
// Only the temperature is required. Everything else is best-effort, for the
// reason RPM already was: a missing hwmon, a powercap grant the user has not
// run setup for, or a battery that reports no power must not make the
// temperature unreadable — a sample that fails is a *gap* in the history,
// which costs every series and not just the one that could not be read.
func (t *telemetry) Sample() (driver.Sample, error) {
	var s driver.Sample
	info := t.Info()
	if t.src.Temp == nil {
		return s, errors.New("telemetry: no temperature source")
	}
	temp, err := t.src.Temp()
	if err != nil {
		return s, err
	}
	s.TempC = temp
	// Fan speed is read here only from a declared read-only source
	// (telemetry.fans): where voltaire controls the fans, the daemon fills RPM
	// from the fan controller (driver.Sample) instead.
	if info.Fans != "" {
		if rpm, err := ReadHwmonFanRPM(info.Fans); err == nil {
			s.RPM = rpm
		}
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
	if info.GPU != "" {
		t.src.GPU.Fill(&s)
	}
	if info.CPUStats != "" {
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
	if info.NPU != "" {
		t.src.NPU.Fill(&s)
	}
	if info.Net != "" {
		// Counters, not a rate — the sampler derives NetRxMBps/NetTxMBps, on
		// the energy-counter pattern.
		if rx, tx, err := ReadNetBytes(); err == nil {
			s.NetRxBytes, s.NetTxBytes = rx, tx
		}
	}
	return s, nil
}

// NewCPUBoost returns the cpufreq boost driver.
func NewCPUBoost() driver.CPUBoost { return cpuBoost{} }

type cpuBoost struct{}

func (cpuBoost) Get() (bool, error) { return ReadCPUBoost() }

func (cpuBoost) Set(on bool) error { return SetCPUBoost(on) }
