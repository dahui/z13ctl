package asusz13

import (
	"encoding/binary"
	"os"
	"testing"
)

func TestParseCPUStatLine(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	busy, total, err := parseCPUStatLine("cpu  100 0 50 800 40 5 5 0")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1000 {
		t.Errorf("total = %d, want 1000", total)
	}
	// idle and iowait total 840, leaving 160 busy.
	if busy != 160 {
		t.Errorf("busy = %d, want 160", busy)
	}

	for _, bad := range []string{"", "cpu0 1 2 3 4 5 6", "cpu 1 2 3", "cpu a b c d e f"} {
		if _, _, err := parseCPUStatLine(bad); err == nil {
			t.Errorf("parseCPUStatLine(%q) accepted", bad)
		}
	}
}

func TestParseMemInfoMB(t *testing.T) {
	used, total, err := parseMemInfoMB("MemTotal:       65536000 kB\nMemFree:         1000000 kB\nMemAvailable:   32768000 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if total != 64000 {
		t.Errorf("total = %d, want 64000", total)
	}
	if used != 32000 {
		t.Errorf("used = %d, want 32000", used)
	}
	if _, _, err := parseMemInfoMB("MemFree: 12 kB\n"); err == nil {
		t.Error("meminfo without MemTotal/MemAvailable accepted")
	}
}

func TestReadCPUClockMHz(t *testing.T) {
	dir := t.TempDir()
	orig := sysCPUDir
	sysCPUDir = dir
	t.Cleanup(func() { sysCPUDir = orig })

	// Two cores with readable clocks, one without, plus non-core entries the
	// prefix must not match.
	for name, khz := range map[string]string{"cpu0": "3200000", "cpu1": "1600000"} {
		if err := os.MkdirAll(dir+"/"+name+"/cpufreq", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+name+"/cpufreq/scaling_cur_freq", []byte(khz+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"cpu2", "cpufreq", "cpuidle"} {
		if err := os.MkdirAll(dir+"/"+name, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ReadCPUClockMHz()
	if err != nil {
		t.Fatal(err)
	}
	if got != 2400 {
		t.Errorf("ReadCPUClockMHz() = %d, want 2400 (average of 3200 and 1600)", got)
	}
}

// npuSensorRecord builds one 168-byte amdxdna sensor record.
func npuSensorRecord(typ byte, input uint32, units string, unitm int8) []byte {
	rec := make([]byte, amdxdnaSensorSize)
	binary.LittleEndian.PutUint32(rec[amdxdnaSensorInputOff:], input)
	copy(rec[amdxdnaSensorUnitsOff:], units)
	rec[amdxdnaSensorUnitmOff] = byte(unitm)
	rec[amdxdnaSensorTypeOff] = typ
	return rec
}

func TestDecodeNPUSensors(t *testing.T) {
	// Power in mW plus two utilisation columns to average.
	payload := append(npuSensorRecord(amdxdnaSensorPower, 1850, "mW", 0),
		append(npuSensorRecord(amdxdnaSensorColumnUtilization, 30, "%", 0),
			npuSensorRecord(amdxdnaSensorColumnUtilization, 10, "%", 0)...)...)

	powerW, util := decodeNPUSensors(payload)
	if powerW != 1.85 {
		t.Errorf("powerW = %v, want 1.85 (mW scaled)", powerW)
	}
	if util != 20 {
		t.Errorf("util = %d, want 20 (average of 30 and 10)", util)
	}

	// Watts-denominated power passes through unscaled.
	powerW, _ = decodeNPUSensors(npuSensorRecord(amdxdnaSensorPower, 2, "W", 0))
	if powerW != 2 {
		t.Errorf("powerW = %v, want 2", powerW)
	}
}

func TestDecodeNPUClockMHz(t *testing.T) {
	md := make([]byte, amdxdnaClockMDSize)
	copy(md, "MP-NPU Clock\x00")
	binary.LittleEndian.PutUint32(md[amdxdnaClockFreqOff:], 1267)
	got, err := decodeNPUClockMHz(md)
	if err != nil || got != 1267 {
		t.Errorf("got %v, %v; want 1267", got, err)
	}
	if _, err := decodeNPUClockMHz([]byte{1, 2, 3}); err == nil {
		t.Error("short clock metadata accepted")
	}
}
