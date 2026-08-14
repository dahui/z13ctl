package asusz13

import (
	"encoding/binary"
	"os"
	"testing"
)

// metricsTable builds a gpu_metrics_v3_0 table with the given GFX power (mW)
// and UCLK (MHz) at their documented offsets.
func metricsTable(t *testing.T, gfxMW uint32, uclkMHz uint16) []byte {
	t.Helper()
	data := make([]byte, 264)
	binary.LittleEndian.PutUint16(data[0:], 264) // structure_size
	data[2], data[3] = 3, 0                      // format revision 3.0
	binary.LittleEndian.PutUint32(data[124:], gfxMW)
	binary.LittleEndian.PutUint16(data[186:], uclkMHz)
	return data
}

func TestParseGPUMetrics(t *testing.T) {
	good := metricsTable(t, 3200, 1000)
	if w, err := parseGPUMetricsGFXPowerW(good); err != nil || w != 3.2 {
		t.Errorf("power = %v, %v; want 3.2", w, err)
	}
	if mhz, err := parseGPUMetricsUClkMHz(good); err != nil || mhz != 1000 {
		t.Errorf("uclk = %v, %v; want 1000", mhz, err)
	}

	// Sentinel values mean "unavailable", never a huge or zero reading.
	if _, err := parseGPUMetricsGFXPowerW(metricsTable(t, 0xffffffff, 1000)); err == nil {
		t.Error("GFX power sentinel accepted")
	}
	if _, err := parseGPUMetricsUClkMHz(metricsTable(t, 0, 0)); err == nil {
		t.Error("UCLK zero accepted")
	}

	// A future table revision must be refused rather than read at offsets
	// that may have moved.
	wrong := metricsTable(t, 3200, 1000)
	wrong[2] = 4
	if _, err := parseGPUMetricsGFXPowerW(wrong); err == nil {
		t.Error("unknown revision accepted")
	}
	if _, err := parseGPUMetricsGFXPowerW([]byte{1, 2}); err == nil {
		t.Error("short table accepted")
	}
}

func TestParseDpmClockMHz(t *testing.T) {
	got, err := parseDpmClockMHz("0: 600Mhz\n1: 638Mhz *\n2: 2900Mhz\n")
	if err != nil || got != 638 {
		t.Errorf("got %v, %v; want 638", got, err)
	}
	if _, err := parseDpmClockMHz("0: 600Mhz\n1: 638Mhz\n"); err == nil {
		t.Error("list without an active P-state accepted")
	}
}

func TestFindGPUDevicePath(t *testing.T) {
	dir := t.TempDir()
	orig := sysDrmDir
	sysDrmDir = dir
	t.Cleanup(func() { sysDrmDir = orig })

	// card0: not amdgpu. card1: amdgpu. card1-DP-1: a connector, skipped.
	driverDir := dir + "/drivers/amdgpu"
	otherDir := dir + "/drivers/i915"
	for _, d := range []string{dir + "/card0/device", dir + "/card1/device", dir + "/card1-DP-1/device", driverDir, otherDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(otherDir, dir+"/card0/device/driver"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, dir+"/card1/device/driver"); err != nil {
		t.Fatal(err)
	}

	got := findGPUDevicePath()
	if got != dir+"/card1/device" {
		t.Errorf("findGPUDevicePath() = %q, want card1's device dir", got)
	}
}
