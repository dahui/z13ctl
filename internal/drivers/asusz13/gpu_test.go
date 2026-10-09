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

// v2Table builds a gpu_metrics_v2_<content> table at the offsets offsetof gave
// over the kernel's structs (gfx power 46, uclk 68; sizes per revision).
func v2Table(content byte, size int, gfxMW, uclkMHz uint16) []byte {
	data := make([]byte, size)
	binary.LittleEndian.PutUint16(data[0:], uint16(size))
	data[2], data[3] = 2, content
	binary.LittleEndian.PutUint16(data[46:], gfxMW)
	binary.LittleEndian.PutUint16(data[68:], uclkMHz)
	return data
}

func TestGPUMetricsRevisions(t *testing.T) {
	// 2.1 (Rembrandt, Phoenix): the memory clock reads, but "gfx power" is the
	// shared VDD rail there, so it is refused rather than charted as GPU power.
	phx := v2Table(1, 120, 9000, 2800)
	if mhz, err := parseGPUMetricsUClkMHz(phx); err != nil || mhz != 2800 {
		t.Errorf("2.1 uclk = %v, %v; want 2800", mhz, err)
	}
	if _, err := parseGPUMetricsGFXPowerW(phx); err == nil {
		t.Error("2.1 gfx power accepted; it is VDDCR_VDD on those chips")
	}

	// 2.4 (Van Gogh): GFX rail, uint16 mW.
	vgh := v2Table(4, 168, 4500, 2750)
	if w, err := parseGPUMetricsGFXPowerW(vgh); err != nil || w != 4.5 {
		t.Errorf("2.4 gfx power = %v, %v; want 4.5", w, err)
	}

	// Renoir (2.2) leaves the field at the driver's all-ones fill.
	rn := v2Table(2, 128, 0xffff, 0xffff)
	if _, err := parseGPUMetricsUClkMHz(rn); err == nil {
		t.Error("unset uclk (0xffff) accepted")
	}

	// A table whose declared size stops short of the field is not read past it.
	short := v2Table(1, 120, 0, 2800)
	binary.LittleEndian.PutUint16(short[0:], 60)
	if _, err := parseGPUMetricsUClkMHz(short); err == nil {
		t.Error("field beyond the declared structure size accepted")
	}
}

// gpuCard builds an amdgpu card with its own hwmon under device/hwmon, as the
// kernel lays it out, returning the hwmon directory.
func gpuCard(t *testing.T) (dev, hwmon string) {
	t.Helper()
	dir := t.TempDir()
	swap(t, &sysDrmDir, dir)
	dev = dir + "/card1/device"
	hwmon = dev + "/hwmon/hwmon5"
	for _, d := range []string{hwmon, dir + "/drivers/amdgpu"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(dir+"/drivers/amdgpu", dev+"/driver"); err != nil {
		t.Fatal(err)
	}
	write := func(p, v string) {
		if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(hwmon+"/name", "amdgpu")
	write(dev+"/pp_dpm_sclk", "0: 600Mhz \n1: 642Mhz *\n2: 2900Mhz ")
	return dev, hwmon
}

func TestGPUReadsComeFromTheCardsOwnHwmon(t *testing.T) {
	_, hw := gpuCard(t)
	write := func(name, v string) {
		if err := os.WriteFile(hw+"/"+name, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Labelled channels: edge is temp2 here, sclk is freq1 in Hz.
	write("temp1_label", "junction")
	write("temp1_input", "90000")
	write("temp2_label", "edge")
	write("temp2_input", "47000")
	write("freq1_label", "sclk")
	write("freq1_input", "1638000000")

	if c, err := ReadGPUTempC(); err != nil || c != 47 {
		t.Errorf("ReadGPUTempC = %v, %v; want the edge channel's 47", c, err)
	}
	if mhz, err := ReadGPUClockMHz(); err != nil || mhz != 1638 {
		t.Errorf("ReadGPUClockMHz = %v, %v; want 1638 from freq1_input in Hz", mhz, err)
	}
	if mhz, err := ReadGPUMaxClockMHz(); err != nil || mhz != 2900 {
		t.Errorf("ReadGPUMaxClockMHz = %v, %v; want 2900", mhz, err)
	}
}

func TestGPUClockFallsBackToTheDPMList(t *testing.T) {
	_, hw := gpuCard(t)
	if err := os.WriteFile(hw+"/temp1_input", []byte("51000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if mhz, err := ReadGPUClockMHz(); err != nil || mhz != 642 {
		t.Errorf("ReadGPUClockMHz = %v, %v; want pp_dpm_sclk's active 642", mhz, err)
	}
	// No labels at all: channel 1 is the one an older kernel means.
	if c, err := ReadGPUTempC(); err != nil || c != 51 {
		t.Errorf("unlabelled ReadGPUTempC = %v, %v; want 51", c, err)
	}
}

// The NPU is the accel device bound to amdxdna — its ioctls are amdxdna's — not
// whichever accel device AMD made.
func TestFindNPUDevicePathMatchesTheDriver(t *testing.T) {
	dir := t.TempDir()
	swap(t, &sysAccelDir, dir+"/class")
	swap(t, &devAccelDir, "/dev/accel")
	for _, n := range []struct{ node, driver, vendor string }{
		{"accel0", "other_amd_accel", "0x1022"},
		{"accel1", "amdxdna", "0x1022"},
	} {
		dev := dir + "/class/" + n.node + "/device"
		drv := dir + "/drivers/" + n.driver
		for _, d := range []string{dev, drv} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(dev+"/vendor", []byte(n.vendor+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(drv, dev+"/driver"); err != nil {
			t.Fatal(err)
		}
	}
	if got := findNPUDevicePath(); got != "/dev/accel/accel1" {
		t.Errorf("findNPUDevicePath() = %q, want the amdxdna-bound accel1", got)
	}
}
