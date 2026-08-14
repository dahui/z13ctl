package asusz13

// gpu.go — iGPU telemetry from the amdgpu driver: edge temperature (hwmon),
// busy percentage, system clock, GFX-domain power and memory clock
// (gpu_metrics), and the VRAM carveout of unified memory.
//
// Sources and offsets follow z13ctl-plus (github.com/aic0d3r/z13ctl-plus,
// Apache-2.0), verified against this machine's amdgpu: gpu_metrics revision
// 3.0, 264 bytes. Everything here is a plain sysfs read; discovery is by
// driver name, uncached, exactly as FindFanHwmonPath discovers hwmon — the
// per-second cost is a directory listing.

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

// ReadGPUTempC returns the iGPU edge temperature in °C, from the amdgpu
// hwmon device.
func ReadGPUTempC() (int, error) {
	dir := FindFanHwmonPath("amdgpu")
	if dir == "" {
		return 0, fmt.Errorf("amdgpu hwmon not found")
	}
	milli, err := readIntFile(dir + "/temp1_input")
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

// ReadGPUClockMHz returns the current iGPU system clock (sclk) in MHz:
// freq1_input where the driver offers it, else the pp_dpm_sclk P-state list,
// whose active entry is marked with '*'.
func ReadGPUClockMHz() (int, error) {
	dev := findGPUDevicePath()
	if dev == "" {
		return 0, fmt.Errorf("amdgpu device not found")
	}
	if v, err := readIntFile(dev + "/freq1_input"); err == nil {
		return v, nil
	}
	data, err := os.ReadFile(dev + "/pp_dpm_sclk")
	if err != nil {
		return 0, err
	}
	return parseDpmClockMHz(string(data))
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

// checkGPUMetricsHeader validates the table's size header and 3.0 revision;
// both parsers below refuse anything else rather than reading a moved offset.
func checkGPUMetricsHeader(data []byte, need int) error {
	if len(data) < need {
		return fmt.Errorf("gpu_metrics table too short: %d", len(data))
	}
	if data[2] != 3 || data[3] != 0 {
		return fmt.Errorf("unsupported gpu_metrics revision %d.%d", data[2], data[3])
	}
	if size := int(binary.LittleEndian.Uint16(data[:2])); size < need || size > len(data) {
		return fmt.Errorf("invalid gpu_metrics table size: %d", size)
	}
	return nil
}

// parseGPUMetricsGFXPowerW extracts average_gfx_power: a uint32 in mW at byte
// offset 124 of gpu_metrics_v3_0.
func parseGPUMetricsGFXPowerW(data []byte) (float64, error) {
	const off = 124
	if err := checkGPUMetricsHeader(data, off+4); err != nil {
		return 0, err
	}
	mw := binary.LittleEndian.Uint32(data[off : off+4])
	if mw == 0xffffffff {
		return 0, fmt.Errorf("gpu_metrics GFX power unavailable")
	}
	return float64(mw) / 1000, nil
}

// parseGPUMetricsUClkMHz extracts average_uclk_frequency: a uint16 in MHz at
// byte offset 186 of gpu_metrics_v3_0.
func parseGPUMetricsUClkMHz(data []byte) (int, error) {
	const off = 186
	if err := checkGPUMetricsHeader(data, off+2); err != nil {
		return 0, err
	}
	mhz := int(binary.LittleEndian.Uint16(data[off : off+2]))
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
