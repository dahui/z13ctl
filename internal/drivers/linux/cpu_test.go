package linux

import (
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
