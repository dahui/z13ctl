package asusz13

// gpu.go — iGPU telemetry from the amdgpu driver: edge temperature (hwmon),
// busy percentage, system clock, GFX-domain power and memory clock
// (gpu_metrics), and the VRAM carveout of unified memory.
//
// Every reading comes from one card: the first DRM card whose driver is
// amdgpu, and the amdgpu hwmon *under that card's device* — not the first
// amdgpu hwmon on the system, which on a machine with two AMD GPUs can belong
// to the other one. Channels are chosen by their kernel label (temp "edge",
// freq "sclk"), not by number. Everything is a plain sysfs read; discovery is
// uncached, so the per-second cost is a directory listing.
//
// gpu_metrics offsets come from the kernel's struct definitions
// (drivers/gpu/drm/amd/include/kgd_pp_interface.h), computed with offsetof
// rather than counted by hand; see gpuMetricsLayouts.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// findGPUDevicePath returns the /sys/class/drm/card*/device directory whose
// driver is amdgpu, or "" when no such card exists.
func findGPUDevicePath() string {
	entries, err := os.ReadDir(sysDrmDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue // cardN-M are output connectors, not the card
		}
		devPath := sysDrmDir + "/" + name + "/device"
		link, err := filepath.EvalSymlinks(devPath + "/driver")
		if err != nil {
			continue
		}
		if filepath.Base(link) == "amdgpu" {
			return devPath
		}
	}
	return ""
}

// gpuHwmonDir returns the amdgpu hwmon directory under the card's device
// directory dev, or "".
func gpuHwmonDir(dev string) string {
	entries, err := os.ReadDir(dev + "/hwmon")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		dir := dev + "/hwmon/" + e.Name()
		if name, err := os.ReadFile(dir + "/name"); err == nil && strings.TrimSpace(string(name)) == "amdgpu" {
			return dir
		}
	}
	return ""
}

// hwmonChannel returns the <kind>N prefix (e.g. "temp1") of the channel in dir
// whose label is want, falling back to channel 1 when no channel is labelled —
// older kernels label nothing, and channel 1 is the one they mean.
func hwmonChannel(dir, kind, want string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return kind + "1"
	}
	labelled := false
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, kind) || !strings.HasSuffix(name, "_label") {
			continue
		}
		labelled = true
		if data, err := os.ReadFile(dir + "/" + name); err == nil && strings.TrimSpace(string(data)) == want {
			return strings.TrimSuffix(name, "_label")
		}
	}
	if labelled {
		return "" // labelled channels exist and none is the one asked for
	}
	return kind + "1"
}

// ReadGPUTempC returns the iGPU edge temperature in °C, from the card's own
// amdgpu hwmon.
func ReadGPUTempC() (int, error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, fmt.Errorf("amdgpu device not found")
	}
	dir := gpuHwmonDir(dev)
	if dir == "" {
		return 0, fmt.Errorf("amdgpu hwmon not found")
	}
	ch := hwmonChannel(dir, "temp", "edge")
	if ch == "" {
		return 0, fmt.Errorf("amdgpu hwmon has no edge temperature")
	}
	milli, err := readIntFile(dir + "/" + ch + "_input")
	if err != nil {
		return 0, err
	}
	return milli / 1000, nil
}

// ReadGPUBusyPct returns the iGPU busy percentage (0–100).
func ReadGPUBusyPct() (int, error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, fmt.Errorf("amdgpu device not found")
	}
	return readIntFile(dev + "/gpu_busy_percent")
}

// ReadGPUClockMHz returns the current iGPU system clock (sclk) in MHz: the
// hwmon freq channel labelled "sclk" where the driver offers one — in Hz,
// under the card's hwmon directory, never beside the device's other files —
// else the pp_dpm_sclk P-state list, whose active entry is marked with '*'.
//
// The hwmon read used to look for device/freq1_input, which does not exist,
// so the fallback was the only path that ever ran.
func ReadGPUClockMHz() (int, error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, fmt.Errorf("amdgpu device not found")
	}
	if dir := gpuHwmonDir(dev); dir != "" {
		if ch := hwmonChannel(dir, "freq", "sclk"); ch != "" {
			if hz, err := readIntFile(dir + "/" + ch + "_input"); err == nil && hz > 0 {
				return hz / 1_000_000, nil
			}
		}
	}
	data, err := os.ReadFile(dev + "/pp_dpm_sclk")
	if err != nil {
		return 0, err
	}
	return parseDpmClockMHz(string(data))
}

// ReadGPUMaxClockMHz returns the iGPU's highest sclk P-state in MHz — the top
// of pp_dpm_sclk, a static list the driver formats.
func ReadGPUMaxClockMHz() (int, error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, fmt.Errorf("amdgpu device not found")
	}
	data, err := os.ReadFile(dev + "/pp_dpm_sclk")
	if err != nil {
		return 0, err
	}
	return parseDpmMaxClockMHz(string(data))
}

// ReadVRAMMB returns the iGPU VRAM usage in MB. On unified-memory hardware
// "VRAM" is the carveout of system RAM reserved for the iGPU: total is the
// carveout size, used how much of it is in use.
func ReadVRAMMB() (used, total int, err error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, 0, fmt.Errorf("amdgpu device not found")
	}
	u, err := readIntFile(dev + "/mem_info_vram_used")
	if err != nil {
		return 0, 0, err
	}
	t, err := readIntFile(dev + "/mem_info_vram_total")
	if err != nil {
		return 0, 0, err
	}
	return u / (1024 * 1024), t / (1024 * 1024), nil
}

// ReadGPUMetrics reads the gpu_metrics table once and returns the two live
// figures it carries that nothing else in sysfs does: the time-filtered GFX
// domain power and the memory (UCLK) clock. One read for both — the table is
// a single snapshot, and reading it twice could hand back two different
// moments.
func ReadGPUMetrics() (gfxPowerW float64, uclkMHz int, err error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, 0, fmt.Errorf("amdgpu device not found")
	}
	data, err := os.ReadFile(dev + "/gpu_metrics")
	if err != nil {
		return 0, 0, err
	}
	gfxPowerW, powerErr := parseGPUMetricsGFXPowerW(data)
	uclkMHz, uclkErr := parseGPUMetricsUClkMHz(data)
	if powerErr != nil && uclkErr != nil {
		return 0, 0, powerErr
	}
	return gfxPowerW, uclkMHz, nil
}

// gpuMetricsLayout is where one gpu_metrics revision keeps the two figures
// read here. gfxPowerSize 0 means the revision has no GFX-only power to read.
type gpuMetricsLayout struct {
	gfxPowerOff, gfxPowerSize int // average_gfx_power, mW
	uclkOff                   int // average_uclk_frequency, uint16 MHz
}

// gpuMetricsLayouts maps (format, content) revision to field offsets, from
// offsetof over the kernel's struct gpu_metrics_vF_C. These are the APU
// layouts; the 1.x dGPU tables are not read.
//
// GFX power is read only where it means the GFX rail. The field exists in
// every 2.x struct, but its source varies by *chip* within one revision —
// Rembrandt and Phoenix (2.1) fill it from Power[0], VDDCR_VDD, the rail the
// CPU cores share with graphics; Renoir (2.2) leaves it unset — so charting it
// as GPU power would be wrong on exactly the machines it would appear on. 2.4
// (Van Gogh: Power[2], VDDCR_GFX) and 3.0 ("time filtered GFX power") are the
// GFX rail. The memory clock is MemclkFrequency in MHz on every producer.
var gpuMetricsLayouts = map[[2]byte]gpuMetricsLayout{
	{2, 0}: {uclkOff: 72},
	{2, 1}: {uclkOff: 68},
	{2, 2}: {uclkOff: 68},
	{2, 3}: {uclkOff: 68},
	{2, 4}: {gfxPowerOff: 46, gfxPowerSize: 2, uclkOff: 68},
	{3, 0}: {gfxPowerOff: 124, gfxPowerSize: 4, uclkOff: 186},
}

// gpuMetricsLayoutFor validates the table's header and returns its revision's
// layout; an unknown revision is refused rather than read at a guessed offset.
func gpuMetricsLayoutFor(data []byte) (gpuMetricsLayout, error) {
	if len(data) < 4 {
		return gpuMetricsLayout{}, fmt.Errorf("gpu_metrics table too short: %d", len(data))
	}
	l, ok := gpuMetricsLayouts[[2]byte{data[2], data[3]}]
	if !ok {
		return gpuMetricsLayout{}, fmt.Errorf("unsupported gpu_metrics revision %d.%d", data[2], data[3])
	}
	if size := int(binary.LittleEndian.Uint16(data[:2])); size > len(data) {
		return gpuMetricsLayout{}, fmt.Errorf("invalid gpu_metrics table size: %d", size)
	}
	return l, nil
}

// fieldIn checks that a field at off..off+n lies inside the table's declared
// size.
func fieldIn(data []byte, off, n int) error {
	if size := int(binary.LittleEndian.Uint16(data[:2])); off+n > size || off+n > len(data) {
		return fmt.Errorf("gpu_metrics table too short for offset %d", off)
	}
	return nil
}

// parseGPUMetricsGFXPowerW extracts average_gfx_power in watts. The driver
// fills an unset field with all ones (smu_cmn_init_soft_gpu_metrics), which
// reads as unavailable.
func parseGPUMetricsGFXPowerW(data []byte) (float64, error) {
	l, err := gpuMetricsLayoutFor(data)
	if err != nil {
		return 0, err
	}
	if l.gfxPowerSize == 0 {
		return 0, fmt.Errorf("gpu_metrics %d.%d carries no GFX-only power", data[2], data[3])
	}
	if err := fieldIn(data, l.gfxPowerOff, l.gfxPowerSize); err != nil {
		return 0, err
	}
	var mw uint32
	if l.gfxPowerSize == 4 {
		mw = binary.LittleEndian.Uint32(data[l.gfxPowerOff:])
		if mw == 0xffffffff {
			return 0, fmt.Errorf("gpu_metrics GFX power unavailable")
		}
	} else {
		mw = uint32(binary.LittleEndian.Uint16(data[l.gfxPowerOff:]))
		if mw == 0xffff {
			return 0, fmt.Errorf("gpu_metrics GFX power unavailable")
		}
	}
	return float64(mw) / 1000, nil
}

// parseGPUMetricsUClkMHz extracts average_uclk_frequency in MHz.
func parseGPUMetricsUClkMHz(data []byte) (int, error) {
	l, err := gpuMetricsLayoutFor(data)
	if err != nil {
		return 0, err
	}
	if err := fieldIn(data, l.uclkOff, 2); err != nil {
		return 0, err
	}
	mhz := int(binary.LittleEndian.Uint16(data[l.uclkOff:]))
	if mhz == 0 || mhz == 0xffff {
		return 0, fmt.Errorf("gpu_metrics UCLK unavailable")
	}
	return mhz, nil
}

// parseDpmClockMHz parses an amdgpu pp_dpm_* P-state list and returns the
// active clock — the line marked with '*':
//
//	0: 600Mhz
//	1: 638Mhz *
//	2: 2900Mhz
func parseDpmClockMHz(data string) (int, error) {
	for _, line := range strings.Split(data, "\n") {
		if !strings.Contains(line, "*") {
			continue
		}
		rest := line
		if i := strings.Index(rest, ":"); i >= 0 {
			rest = rest[i+1:]
		}
		if j := strings.Index(strings.ToLower(rest), "mhz"); j >= 0 {
			v, err := strconv.Atoi(strings.TrimSpace(rest[:j]))
			if err == nil && v > 0 {
				return v, nil
			}
		}
	}
	return 0, fmt.Errorf("no active P-state in pp_dpm list")
}

// parseDpmMaxClockMHz returns the highest clock in an amdgpu pp_dpm_* list.
func parseDpmMaxClockMHz(data string) (int, error) {
	best := 0
	for _, line := range strings.Split(data, "\n") {
		rest := line
		if i := strings.Index(rest, ":"); i >= 0 {
			rest = rest[i+1:]
		}
		if j := strings.Index(strings.ToLower(rest), "mhz"); j >= 0 {
			if v, err := strconv.Atoi(strings.TrimSpace(rest[:j])); err == nil && v > best {
				best = v
			}
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("no clocks in pp_dpm list")
	}
	return best, nil
}
