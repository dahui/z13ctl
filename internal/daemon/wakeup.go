package daemon

// wakeup.go — names what woke the machine, in the daemon's own log (issue #24).
//
// A machine that cycles through several sleep/wake rounds before it stays asleep
// could be woken by the EC answering our pre-sleep fan and PPT writes, by the
// cover keyboard or lightbar answering the lighting write over USB, or by
// something z13ctl never touches. Each points at a different fix, and without a
// way to reproduce it the only evidence is what the reporter's kernel counted.
// The kernel keeps that count per wakeup source, so the sleep hook snapshots it
// just before handing the suspend to logind and the resume hook reports what
// moved.
//
// Every file read here is the kernel's in-memory bookkeeping and world-readable:
// no ACPI method is evaluated and the EC is never asked anything, so this is
// safe while d.ecWedged is set. Deliberately not /sys/power/wakeup_count, whose
// read blocks while wakeup events are in progress.

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Roots of what this file reads, as vars so tests can point them at a fake tree.
var (
	wakeupClassDir     = "/sys/class/wakeup"
	sysPowerDir        = "/sys/power"
	procInterruptsPath = "/proc/interrupts"
	acpiInterruptsDir  = "/sys/firmware/acpi/interrupts"
)

// wakeCounts is one wakeup source's counters. wakeup_count rises only for an
// event that aborted a suspend or ended one, which is the question being asked;
// event_count rises for every event, asleep or not.
type wakeCounts struct {
	Events  uint64
	Wakeups uint64
}

// wakeSnapshot is the kernel's wakeup bookkeeping at one moment.
type wakeSnapshot struct {
	// At is wall-clock time with the monotonic reading stripped: Go's monotonic
	// clock does not advance during s2idle, so a monotonic difference would
	// report every suspend as a few milliseconds long.
	At       time.Time
	Sources  map[string]wakeCounts
	Suspends uint64 // suspend_stats/success
	Failed   uint64 // suspend_stats/fail
	// ACPI holds the SCI count and each GPE's, from /sys/firmware/acpi/interrupts.
	// The EC signals through one GPE (the kernel logs "ACPI: EC: GPE=0x.." at
	// boot; 0x0a on the GZ302EA), so a wake whose only ACPI activity is that GPE
	// is the EC announcing something. Reading these reads the GPE status
	// register; it evaluates no AML and never waits on the EC.
	ACPI map[string]uint64
}

// wakeExtras is what only the resume side reads: the state the kernel kept from
// the suspend that just ended.
type wakeExtras struct {
	IRQ        string        // pm_wakeup_irq with its /proc/interrupts handler; "" if none recorded
	HWSleep    time.Duration // suspend_stats/last_hw_sleep; how long the hardware was really asleep
	FailedDev  string
	FailedStep string
	FailedErr  string
}

// wakeReport is one suspend's outcome, as diffWakeups worked it out.
type wakeReport struct {
	Slept       time.Duration
	WokenBy     []string // sources whose wakeup_count rose, "name(+n)"
	Activity    []string // when nothing counted a wakeup: the busiest event_count deltas
	SuspendsOK  uint64
	SuspendsBad uint64
	ACPIEvents  []string // "sci(+n)" and each GPE that fired, "gpe0A(+n)"
	wakeExtras
}

// maxActivity bounds the event_count fallback list. Every suspend moves a few
// counters (the RTC, the AC adapter, the EC); the first handful are the
// candidates and the rest is noise in a log line.
const maxActivity = 4

// snapshotWakeups reads every wakeup source's counters and the suspend totals.
// A missing class directory yields an empty map rather than an error, so the
// report still says how long the machine slept.
func snapshotWakeups() wakeSnapshot {
	s := wakeSnapshot{At: time.Now().Round(0), Sources: map[string]wakeCounts{}}
	dirs, _ := filepath.Glob(wakeupClassDir + "/wakeup*")
	for _, dir := range dirs {
		name := readTrimmed(dir + "/name")
		if name == "" {
			continue
		}
		// Many sources carry an opaque name ("device:44" is the ACPI companion
		// of a PCI port), while the device link says which device it is.
		if link, err := os.Readlink(dir + "/device"); err == nil {
			if dev := filepath.Base(link); dev != name && dev != "." {
				name += "[" + dev + "]"
			}
		}
		c := wakeCounts{
			Events:  readUint(dir + "/event_count"),
			Wakeups: readUint(dir + "/wakeup_count"),
		}
		// Names are not unique (two "alarmtimer" sources are common), so sum them
		// rather than letting the second overwrite the first.
		prev := s.Sources[name]
		s.Sources[name] = wakeCounts{Events: prev.Events + c.Events, Wakeups: prev.Wakeups + c.Wakeups}
	}
	s.Suspends = readUint(sysPowerDir + "/suspend_stats/success")
	s.Failed = readUint(sysPowerDir + "/suspend_stats/fail")
	s.ACPI = map[string]uint64{}
	files, _ := filepath.Glob(acpiInterruptsDir + "/gpe[0-9A-F][0-9A-F]")
	for _, f := range append(files, acpiInterruptsDir+"/sci") {
		if n := readFirstUint(f); n > 0 {
			s.ACPI[filepath.Base(f)] = n
		}
	}
	return s
}

// readWakeExtras reads what the kernel recorded about the suspend that just
// ended. The failure fields are read only when failed is true: they describe
// the last failure ever, which may be days old.
func readWakeExtras(failed bool) wakeExtras {
	var x wakeExtras
	if irq := readTrimmed(sysPowerDir + "/pm_wakeup_irq"); irq != "" {
		x.IRQ = irq
		if h := irqHandler(irq); h != "" {
			x.IRQ = irq + " " + h
		}
	}
	if us := readUint(sysPowerDir + "/suspend_stats/last_hw_sleep"); us > 0 {
		x.HWSleep = time.Duration(us) * time.Microsecond
	}
	if failed {
		x.FailedDev = readTrimmed(sysPowerDir + "/suspend_stats/last_failed_dev")
		x.FailedStep = readTrimmed(sysPowerDir + "/suspend_stats/last_failed_step")
		x.FailedErr = readTrimmed(sysPowerDir + "/suspend_stats/last_failed_errno")
	}
	return x
}

// diffWakeups compares the snapshot taken before the suspend with one taken
// after it. Pure, so the arithmetic is tested without a sysfs tree.
func diffWakeups(before, after wakeSnapshot, x wakeExtras) wakeReport {
	r := wakeReport{
		Slept:       after.At.Sub(before.At),
		SuspendsOK:  sub(after.Suspends, before.Suspends),
		SuspendsBad: sub(after.Failed, before.Failed),
		wakeExtras:  x,
	}
	type delta struct {
		name string
		n    uint64
	}
	var woke, busy []delta
	for name, a := range after.Sources {
		b := before.Sources[name]
		if n := sub(a.Wakeups, b.Wakeups); n > 0 {
			woke = append(woke, delta{name, n})
		}
		if n := sub(a.Events, b.Events); n > 0 {
			busy = append(busy, delta{name, n})
		}
	}
	byCount := func(ds []delta) {
		sort.Slice(ds, func(i, j int) bool {
			if ds[i].n != ds[j].n {
				return ds[i].n > ds[j].n
			}
			return ds[i].name < ds[j].name
		})
	}
	var acpi []delta
	for name, n := range after.ACPI {
		if d := sub(n, before.ACPI[name]); d > 0 {
			acpi = append(acpi, delta{name, d})
		}
	}
	sort.Slice(acpi, func(i, j int) bool { return acpi[i].name > acpi[j].name }) // "sci" before "gpeNN"
	for _, d := range acpi {
		r.ACPIEvents = append(r.ACPIEvents, fmt.Sprintf("%s(+%d)", d.name, d.n))
	}
	byCount(woke)
	for _, d := range woke {
		r.WokenBy = append(r.WokenBy, fmt.Sprintf("%s(+%d)", d.name, d.n))
	}
	if len(woke) == 0 {
		byCount(busy)
		for i, d := range busy {
			if i == maxActivity {
				break
			}
			r.Activity = append(r.Activity, fmt.Sprintf("%s(+%d)", d.name, d.n))
		}
	}
	return r
}

// log writes the report as one line, at Warn when a suspend failed outright.
func (r wakeReport) log() {
	attrs := []any{"slept", r.Slept.Round(100 * time.Millisecond)}
	switch {
	case len(r.WokenBy) > 0:
		attrs = append(attrs, "woken_by", strings.Join(r.WokenBy, " "))
	case len(r.Activity) > 0:
		attrs = append(attrs, "woken_by", "not counted", "activity", strings.Join(r.Activity, " "))
	default:
		attrs = append(attrs, "woken_by", "not counted")
	}
	if r.IRQ != "" {
		attrs = append(attrs, "wake_irq", r.IRQ)
	}
	if len(r.ACPIEvents) > 0 {
		attrs = append(attrs, "acpi_events", strings.Join(r.ACPIEvents, " "))
	}
	// On s2idle the hardware-sleep figure is the one that says whether the
	// machine was really asleep: a long suspend with almost none of it in
	// hardware sleep is a machine that stayed busy behind a dark screen.
	attrs = append(attrs, "hw_sleep", r.HWSleep.Round(100*time.Millisecond),
		"suspends_ok", r.SuspendsOK, "suspends_failed", r.SuspendsBad)
	if r.SuspendsBad > 0 {
		attrs = append(attrs, "failed_dev", r.FailedDev, "failed_step", r.FailedStep, "failed_errno", r.FailedErr)
		slog.Warn("resume: wake report; a suspend failed", attrs...)
		return
	}
	slog.Info("resume: wake report", attrs...)
}

// irqHandler returns the handler names /proc/interrupts lists for irq, e.g.
// "acpi" or "xhci_hcd", or "" if the line is not found.
func irqHandler(irq string) string {
	f, err := os.Open(procInterruptsPath)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	prefix := irq + ":"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || fields[0] != prefix {
			continue
		}
		// After the label come one count per CPU, then the chip, the hardware
		// IRQ and trigger, and last the handler names.
		rest := fields[1:]
		for len(rest) > 0 {
			if _, err := strconv.ParseUint(rest[0], 10, 64); err != nil {
				break
			}
			rest = rest[1:]
		}
		if len(rest) < 3 {
			return strings.Join(rest, " ")
		}
		return strings.Join(rest[2:], " ")
	}
	return ""
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readFirstUint reads a file whose first field is a count, as the ACPI
// interrupt files are ("     167  EN     enabled      unmasked").
func readFirstUint(path string) uint64 {
	fields := strings.Fields(readTrimmed(path))
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.ParseUint(fields[0], 10, 64)
	return n
}

func readUint(path string) uint64 {
	n, _ := strconv.ParseUint(readTrimmed(path), 10, 64)
	return n
}

// sub is a-b floored at zero. The counters only grow, but a wakeup source that
// was unregistered and re-registered across the suspend restarts from zero.
func sub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// sciCount is the system-wide SCI count: every EC event arrives as one. The
// sleep hook logs how many arrive while it holds the suspend after a release.
func sciCount() uint64 { return readFirstUint(acpiInterruptsDir + "/sci") }
