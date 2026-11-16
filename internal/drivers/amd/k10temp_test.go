package amd

import "testing"

func TestReadAPUTemperatureConvertsMillidegrees(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.hwmonTemp+"/temp1_input", "62500")

	got, err := ReadAPUTemperature()
	if err != nil {
		t.Fatalf("ReadAPUTemperature() = %v, want nil", err)
	}
	if got != 62 {
		t.Errorf("ReadAPUTemperature() = %d, want 62 (62500 millidegrees)", got)
	}
}

func TestReadAPUTemperatureErrorsWithoutK10temp(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	if _, err := ReadAPUTemperature(); err == nil {
		t.Error("ReadAPUTemperature() = nil, want an error when k10temp is absent")
	}
}
