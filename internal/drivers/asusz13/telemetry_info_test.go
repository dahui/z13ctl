package asusz13

// telemetry_info_test.go — a declared telemetry source is offered only where
// its hardware is present, and the chart hints come from the kernel.

import (
	"testing"

	"github.com/dahui/voltaire/v2/internal/driver"
)

var z13Telemetry = driver.TelemetryInfo{
	PowerDraw: "rapl", GPU: "amdgpu", CPUStats: "procfs", NPU: "amdxdna", Net: "procfs", HistorySeconds: 3600,
}

func TestTelemetryInfoDropsSourcesThatAreNotPresent(t *testing.T) {
	f := newFakeSysfs(t) // powercap present; no amdgpu card, no accel device
	swap(t, &procStatPath, f.root+"/proc/stat")
	f.writeFile(t, f.root+"/proc/stat", "cpu  1 2 3 4\n")
	swap(t, &procNetDevPath, f.root+"/absent/net/dev")
	swap(t, &sysCPUDir, f.root+"/cpu")
	f.writeFile(t, f.root+"/cpu/cpu0/cpufreq/cpuinfo_max_freq", "5187500")
	f.writeFile(t, f.root+"/cpu/cpu1/cpufreq/cpuinfo_max_freq", "4000000")

	got := NewTelemetry(z13Telemetry).Info()
	want := driver.TelemetryInfo{PowerDraw: "rapl", CPUStats: "procfs", HistorySeconds: 3600, ClockMaxMHz: 5187}
	if got != want {
		t.Errorf("Info() = %+v\nwant     %+v", got, want)
	}

	// A source the data does not declare is never offered, present or not.
	if got := NewTelemetry(driver.TelemetryInfo{HistorySeconds: 60}).Info(); got.PowerDraw != "" || got.CPUStats != "" {
		t.Errorf("undeclared sources offered: %+v", got)
	}
}

func TestReadPassiveTripC(t *testing.T) {
	f := newFakeSysfs(t)
	z := func(n, typ string, trips ...[2]string) {
		dir := f.root + "/thermal/" + n
		f.writeFile(t, dir+"/type", typ)
		for i, tr := range trips {
			f.writeFile(t, dir+"/trip_point_"+itoa(i)+"_type", tr[0])
			f.writeFile(t, dir+"/trip_point_"+itoa(i)+"_temp", tr[1])
		}
	}
	// This machine's three acpitz zones, plus a non-ACPI zone that must not count.
	z("thermal_zone0", "acpitz", [2]string{"hot", "103000"})
	z("thermal_zone1", "acpitz", [2]string{"critical", "120000"}, [2]string{"passive", "100000"})
	z("thermal_zone2", "acpitz", [2]string{"critical", "110000"})
	z("thermal_zone3", "x86_pkg_temp", [2]string{"passive", "80000"})
	if c, err := ReadPassiveTripC(); err != nil || c != 100 {
		t.Errorf("ReadPassiveTripC = %v, %v; want 100", c, err)
	}
}
