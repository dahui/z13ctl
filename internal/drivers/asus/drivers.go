package asus

// drivers.go — the driver.* implementations over this package's sysfs layer.
// Constructors are pure (no I/O); hardware is touched only when a method runs.
// The registry wiring lives in the register subpackage so this package does
// not import internal/device — which keeps this package's own tests free to
// pull the authoritative Z13 envelope from the embedded device data.

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// NewFanController returns the asus-nb-wmi fan driver. shape is the device
// data's; the kernel's own answer is laid over it on first use (see Shape), so
// the constructor stays pure.
func NewFanController(shape driver.FanShape) driver.FanController {
	return &fanController{data: shape}
}

type fanController struct {
	data driver.FanShape

	once  sync.Once
	shape driver.FanShape
}

// Shape is the device data with the kernel's answer laid over it, resolved
// once: the points per curve and the fans that report a speed come from hwmon's
// attribute names (listing a directory, never an EC read).
//
// The kernel's point count must match the device data's. When it does not, the
// data's shape stands and a warning says why fan control will fail: the presets
// and the high-TDP floor curve in the data are sized for the data's count, and
// SetFanCurves refuses any curve not sized for the kernel's — so every curve
// write fails closed, and with it any power limit that needs the floor.
// Release still works, which is the direction that is always safe.
func (f *fanController) Shape() driver.FanShape {
	f.once.Do(func() {
		f.shape = f.data
		if _, n, err := FanCurveShape(); err == nil {
			switch {
			case f.data.Points == 0:
				f.shape.Points = n
			case n != f.data.Points:
				slog.Warn("fan curve point count differs from device data; custom fan curves will be refused",
					"kernel", n, "device_data", f.data.Points)
			}
		}
		f.shape.Labels = fanLabels(f.data.Labels, FanKernelLabels())
	})
	return f.shape
}

// fanLabels resolves each fan's display name: the device data's where it
// names one, else the kernel's (fan1_label "cpu_fan" → "CPU fan"), else
// "Fan N". The kernel decides how many fans there are; with no kernel answer
// the data's list stands alone.
func fanLabels(declared, kernel []string) []string {
	if len(kernel) == 0 {
		return slices.Clone(declared)
	}
	out := make([]string, len(kernel))
	for i, k := range kernel {
		switch {
		case i < len(declared) && declared[i] != "":
			out[i] = declared[i]
		case k != "":
			out[i] = prettyFanLabel(k)
		default:
			out[i] = fmt.Sprintf("Fan %d", i+1)
		}
	}
	return out
}

// prettyFanLabel turns a kernel fan label into display text: underscores to
// spaces, known acronyms upper-cased, the first letter capitalised.
func prettyFanLabel(k string) string {
	words := strings.Fields(strings.ReplaceAll(k, "_", " "))
	for i, w := range words {
		switch strings.ToLower(w) {
		case "cpu", "gpu", "apu", "vrm", "pch", "ssd":
			words[i] = strings.ToUpper(w)
		}
	}
	out := strings.Join(words, " ")
	if out == "" {
		return k
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

func (*fanController) ReadRPM() ([]int, error) { return ReadFanRPMs() }

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
func (*fanController) ReadMode() (int, error) {
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

func (*fanController) ApplyCurve(pts []api.FanCurvePoint) error { return SetFanCurves(pts) }

func (*fanController) Release() error { return ResetAllFanCurves() }

// LiveCurve returns the curve programmed into the curve registers whether or
// not it is active — the interface's contract, since the registers survive a
// release on the Z13. "The curve in force" is a composition the caller makes
// from this plus ReadMode (the no-daemon CLI's liveFanCurve helper is exactly
// that composition).
func (*fanController) LiveCurve() ([]api.FanCurvePoint, error) {
	curves, err := ReadFanCurves()
	if err != nil {
		return nil, err
	}
	return curves[0], nil
}

// NewPowerLimiter returns the PPT driver: asus-armoury when the kernel exposes
// the limits there, asus-nb-wmi otherwise (see tdp.go). Its envelope comes from
// device data — the single source of the Z13's safe maximum, stock rows and fan
// floor — with the active interface's own ranges laid over it.
//
// The envelope's Limits are completed here, so even the device-data-only
// envelope (DeviceEnvelope) names each limit: device data's list and wording,
// else every limit in this driver's words (pptLimitText).
func NewPowerLimiter(env driver.PowerEnvelope) driver.PowerLimiter {
	env.Limits = completeLimits(env.Limits)
	return powerLimiter{env: env}
}

type powerLimiter struct{ env driver.PowerEnvelope }

func (powerLimiter) Read() (api.TDPState, error) { return ReadAllPPT() }
func (powerLimiter) Apply(s api.TDPState) error  { return SetTDPState(s) }

// DeviceEnvelope is the device data alone (driver.DeviceEnvelope). Envelope
// reads armoury's bounds, and every armoury PPT read evaluates the AC adapter's
// _PSR to choose its AC or battery table — a live ACPI call.
func (l powerLimiter) DeviceEnvelope() driver.PowerEnvelope { return l.env }

// Envelope is read per call, as the interface is: armoury's bounds belong to
// the power source in use now. With no PPT interface at all the device data
// stands alone, and the write itself then fails, which is the honest answer.
func (l powerLimiter) Envelope() driver.PowerEnvelope {
	b, err := activePPT()
	if err != nil {
		return l.env
	}
	return b.withBounds().envelope(l.env)
}

// ThrottlePolicyPath is asus-nb-wmi's throttle_thermal_policy, which asus-wmi's
// throttle_thermal_policy_write() notifies — and every profile write and every
// pwm_enable=2 on the fan-curve device goes through that call, which is why a
// fan release resets the power limits. It exists only under
// CONFIG_ASUS_WMI_DEPRECATED_ATTRS, and reading it logs no deprecation notice
// (its show function, unlike the ppt_* ones, never calls
// asus_wmi_show_deprecated). Verified on a GZ302EA, kernel 7.2: a redundant
// pwm_enable=2 raises POLLPRI on it, and armoury PPT writes do not. The
// registry adds it to the profile driver's policy paths when the profile
// handler is asus-wmi.
func ThrottlePolicyPath() string { return pptBasePath + "/throttle_thermal_policy" }

// safeToggles is the allowlist of asus-armoury attributes this driver will
// offer as switches, in the order to offer them. Attribute names are kernel
// ABI, so the list belongs in code rather than device data — and it has to
// exist at all because armoury on other ASUS models also exposes
// gpu_mux_mode, dgpu_disable, egpu_enable, apu_mem, cores_* and mini_led_mode,
// which can leave a machine with a black screen or a pending reboot it did not
// ask for. Enumerating "everything the kernel lists" would have offered those
// as a row like any other. An attribute joins this list only once offering it
// as a casual switch has been judged safe; the kernel then supplies its legal
// values and writability, and device data its wording.
var safeToggles = []string{"boot_sound", "panel_overdrive"}

// toggleValuePath is an armoury attribute's value file.
func toggleValuePath(id string) string { return sysFirmwareAttrDir + "/" + id + "/current_value" }

// NewToggles returns the asus-armoury firmware-toggles driver. declared is the
// device data's entries — wording, and the list to fall back on when the
// attributes cannot be read — and hidden names allowlisted attributes the
// device data withholds. It errors on an id outside the allowlist: device data
// naming an attribute this driver will not offer is a data bug surfaced at
// assembly, not a control that silently does nothing. Nothing is read here;
// the kernel's description is read on the first List.
func NewToggles(declared []driver.ToggleSpec, hidden []string) (driver.Toggles, error) {
	h := make(map[string]bool, len(hidden))
	for _, id := range hidden {
		if !slices.Contains(safeToggles, id) {
			return nil, fmt.Errorf("hidden toggle id %q is not one this driver offers", id)
		}
		h[id] = true
	}
	for _, s := range declared {
		if !slices.Contains(safeToggles, s.ID) {
			return nil, fmt.Errorf("unknown toggle id %q", s.ID)
		}
	}
	out := make([]driver.ToggleSpec, len(declared))
	copy(out, declared)
	return &toggles{declared: out, hidden: h}, nil
}

// FindTogglePath returns the sysfs path a toggle id writes to, or "" for an id
// this driver does not offer. For dry-run display: a dry run's job is to spell
// out the exact write, and the path is this driver's own knowledge.
func FindTogglePath(id string) string {
	if !slices.Contains(safeToggles, id) {
		return ""
	}
	return toggleValuePath(id)
}

// SafeToggleIDs is the allowlist, for voltaire setup's permission grants: a
// grant for exactly what the daemon may write, rather than a glob over every
// armoury attribute.
func SafeToggleIDs() []string { return append([]string(nil), safeToggles...) }

type toggles struct {
	declared []driver.ToggleSpec
	hidden   map[string]bool

	once sync.Once
	list []driver.ToggleSpec
}

// List is the toggles this device offers: the allowlisted attributes the kernel
// exposes as writable 0/1 switches, in device-data order, worded by device
// data where it says and by the kernel's display_name where it does not.
func (t *toggles) List() []driver.ToggleSpec {
	t.once.Do(func() { t.list = enumerateToggles(t.declared, t.hidden) })
	out := make([]driver.ToggleSpec, len(t.list))
	copy(out, t.list)
	return out
}

// enumerateToggles reads the kernel's description of each allowlisted
// attribute. Only static attribute metadata is read — type, possible_values,
// display_name and the file mode — never current_value, which is a live WMI
// call. With no attribute directory at all (asus-armoury not loaded) the
// declared list stands as it always has, so a value read then fails and the
// row shows as unknown rather than vanishing.
func enumerateToggles(declared []driver.ToggleSpec, hidden map[string]bool) []driver.ToggleSpec {
	if _, err := os.Stat(sysFirmwareAttrDir); err != nil {
		var out []driver.ToggleSpec
		for _, s := range declared {
			if !hidden[s.ID] {
				out = append(out, s)
			}
		}
		return out
	}

	byID := make(map[string]driver.ToggleSpec, len(declared))
	order := make([]string, 0, len(safeToggles))
	for _, s := range declared {
		byID[s.ID] = s
		order = append(order, s.ID)
	}
	for _, id := range safeToggles {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}

	var out []driver.ToggleSpec
	for _, id := range order {
		if hidden[id] {
			continue
		}
		spec, ok := describeToggle(id, byID[id])
		if !ok {
			if _, declaredHere := byID[id]; declaredHere {
				slog.Info("firmware toggle not offered by the kernel; not showing it", "id", id)
			}
			continue
		}
		out = append(out, spec)
	}
	return out
}

// describeToggle fills spec from the kernel's attribute directory, or reports
// false when the attribute is absent, read-only, or not a 0/1 switch (an
// enumerated toggle has no renderer yet; one that is shown as a switch and
// sent 0 or 1 would be writing something that means neither).
func describeToggle(id string, spec driver.ToggleSpec) (driver.ToggleSpec, bool) {
	dir := sysFirmwareAttrDir + "/" + id
	fi, err := os.Stat(dir + "/current_value")
	if err != nil || fi.Mode().Perm()&0o222 == 0 {
		return spec, false
	}
	if typ := readSysfsTrimmed(dir + "/type"); typ != "" && typ != "enumeration" {
		return spec, false
	}
	values := parsePossibleValues(readSysfsTrimmed(dir + "/possible_values"))
	if len(values) > 0 && !slices.Equal(values, []int{0, 1}) {
		return spec, false
	}
	spec.ID, spec.Kind, spec.Values = id, driver.ToggleBool, values
	if spec.Label == "" {
		spec.Label = readSysfsTrimmed(dir + "/display_name")
	}
	if spec.Label == "" {
		spec.Label = id
	}
	return spec, true
}

// parsePossibleValues parses firmware-attributes' possible_values ("0;1"),
// sorted; nil when absent or not all integers.
func parsePossibleValues(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, f := range strings.Split(s, ";") {
		v, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

func (t *toggles) Get(id string) (int, error) {
	if !slices.Contains(safeToggles, id) {
		return 0, driver.ErrUnsupported
	}
	return readIntFile(toggleValuePath(id))
}

func (t *toggles) Set(id string, value int) error {
	if !slices.Contains(safeToggles, id) {
		return driver.ErrUnsupported
	}
	return os.WriteFile(toggleValuePath(id), []byte(strconv.Itoa(value)+"\n"), 0o644)
}

// PendingReboot reports whether a changed firmware setting is waiting on a
// restart. asus-armoury exposes this once for the whole interface, as a plain
// file beside the attribute directories rather than as an attribute of its own —
// so it is read directly here rather than through the allowlist.
func (t *toggles) PendingReboot() (bool, error) {
	v, err := readIntFile(sysFirmwareAttrDir + "/pending_reboot")
	if err != nil {
		return false, err
	}
	return v != 0, nil
}
