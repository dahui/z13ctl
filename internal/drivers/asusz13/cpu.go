package asusz13

// cpu.go — CPU-side telemetry from procfs and cpufreq: the cumulative jiffie
// counters utilisation is derived from, the average core clock, and system
// memory usage.
//
// The jiffie counters cross the driver boundary as counters, never as a
// percentage: utilisation is a delta between two readings, which needs the
// previous one, and a driver holds no state — the same rule the RAPL energy
// counter established (see driver.Sample). The daemon's sampler does the
// division.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadCPUJiffies returns the aggregate busy and total jiffie counters from
// /proc/stat's "cpu" line. Both are cumulative since boot.
func ReadCPUJiffies() (busy, total uint64, err error) {
	data, err := os.ReadFile(procStatPath)
	if err != nil {
		return 0, 0, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return parseCPUStatLine(line)
}

// parseCPUStatLine parses the aggregate line:
//
//	cpu  user nice system idle iowait irq softirq steal guest guest_nice
//
// idle+iowait count as idle; everything else is busy.
func parseCPUStatLine(line string) (busy, total uint64, err error) {
	fields := strings.Fields(line)
	// idle and iowait are the 4th and 5th value columns; anything shorter is
	// not the aggregate line this parses.
	if len(fields) < 6 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("malformed /proc/stat aggregate line")
	}
	var jiffies []uint64
	for _, s := range fields[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parsing jiffie %q: %w", s, err)
		}
		jiffies = append(jiffies, v)
	}
	idle := jiffies[3] + jiffies[4]
	for _, v := range jiffies {
		total += v
	}
	return total - idle, total, nil
}

// ReadCPUClockMHz returns the average scaling_cur_freq across all cores, in
// MHz. Cores without a readable cpufreq node are skipped; none readable is an
// error.
func ReadCPUClockMHz() (int, error) {
	entries, err := os.ReadDir(sysCPUDir)
	if err != nil {
		return 0, err
	}
	var sum, count uint64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "cpu") {
			continue
		}
		// cpufreq, cpuidle and friends share the prefix; a core is cpuN.
		if _, err := strconv.Atoi(strings.TrimPrefix(name, "cpu")); err != nil {
			continue
		}
		khz, err := readIntFile(sysCPUDir + "/" + name + "/cpufreq/scaling_cur_freq")
		if err != nil {
			continue
		}
		sum += uint64(khz) / 1000
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("no readable cpufreq nodes")
	}
	return int(sum / count), nil
}

// ReadMemoryMB returns system memory usage in MB, used = MemTotal −
// MemAvailable — the kernel's own estimate of what is genuinely in use, the
// figure free(1) prints as "used".
func ReadMemoryMB() (used, total int, err error) {
	data, err := os.ReadFile(memInfoPath)
	if err != nil {
		return 0, 0, err
	}
	return parseMemInfoMB(string(data))
}

func parseMemInfoMB(data string) (used, total int, err error) {
	var memTotalKB, memAvailKB int
	for _, line := range strings.Split(data, "\n") {
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotalKB = firstInt(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			memAvailKB = firstInt(line)
		}
		if memTotalKB > 0 && memAvailKB > 0 {
			break
		}
	}
	if memTotalKB <= 0 || memAvailKB <= 0 {
		return 0, 0, fmt.Errorf("meminfo missing MemTotal/MemAvailable")
	}
	return (memTotalKB - memAvailKB) / 1024, memTotalKB / 1024, nil
}

// firstInt returns the first integer in a meminfo-style "Key:  12345 kB" line.
func firstInt(line string) int {
	for _, f := range strings.Fields(line) {
		if v, err := strconv.Atoi(f); err == nil {
			return v
		}
	}
	return 0
}
