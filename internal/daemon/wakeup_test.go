package daemon

// wakeup_test.go — the wake report's arithmetic, and the readers against a fake
// /sys/class/wakeup, /sys/power and /proc/interrupts. Not parallel: the readers'
// roots are package vars.

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestDiffWakeupsNamesTheSourcesThatWoke(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)
	before := wakeSnapshot{
		At: t0,
		Sources: map[string]wakeCounts{
			"PNP0C09:00":  {Events: 10, Wakeups: 2},
			"ACPI0003:00": {Events: 4, Wakeups: 0},
			"1-3":         {Events: 1, Wakeups: 0},
		},
		Suspends: 5, Failed: 1,
	}
	after := wakeSnapshot{
		At: t0.Add(90 * time.Second),
		Sources: map[string]wakeCounts{
			"PNP0C09:00":  {Events: 13, Wakeups: 3},
			"ACPI0003:00": {Events: 6, Wakeups: 0},
			"1-3":         {Events: 3, Wakeups: 2},
			// Registered during the suspend: everything it counted is new.
			"rtc0": {Events: 1, Wakeups: 1},
		},
		Suspends: 6, Failed: 1,
	}
	before.ACPI = map[string]uint64{"sci": 160, "gpe0A": 160}
	after.ACPI = map[string]uint64{"sci": 163, "gpe0A": 163, "gpe03": 1}
	r := diffWakeups(before, after, wakeExtras{IRQ: "9 acpi"})
	if want := []string{"sci(+3)", "gpe0A(+3)", "gpe03(+1)"}; !slices.Equal(r.ACPIEvents, want) {
		t.Errorf("ACPIEvents = %q, want %q", r.ACPIEvents, want)
	}
	if r.Slept != 90*time.Second {
		t.Errorf("Slept = %v, want 90s", r.Slept)
	}
	want := []string{"1-3(+2)", "PNP0C09:00(+1)", "rtc0(+1)"}
	if !slices.Equal(r.WokenBy, want) {
		t.Errorf("WokenBy = %q, want %q (by count, then name)", r.WokenBy, want)
	}
	if r.Activity != nil {
		t.Errorf("Activity = %q, want none when a wakeup was counted", r.Activity)
	}
	if r.SuspendsOK != 1 || r.SuspendsBad != 0 || r.IRQ != "9 acpi" {
		t.Errorf("report = %+v", r)
	}
}

// With no wakeup counted, the busiest event counters are the only lead.
func TestDiffWakeupsFallsBackToActivity(t *testing.T) {
	before := wakeSnapshot{Sources: map[string]wakeCounts{"a": {Events: 1}, "b": {Events: 1}}}
	after := wakeSnapshot{Sources: map[string]wakeCounts{
		"a": {Events: 2}, "b": {Events: 9}, "c": {Events: 3}, "d": {Events: 4}, "e": {Events: 5}, "f": {Events: 6},
	}, Failed: 2}
	r := diffWakeups(before, after, wakeExtras{})
	want := []string{"b(+8)", "f(+6)", "e(+5)", "d(+4)"}
	if !slices.Equal(r.Activity, want) {
		t.Errorf("Activity = %q, want the top %d: %q", r.Activity, maxActivity, want)
	}
	if r.SuspendsBad != 2 {
		t.Errorf("SuspendsBad = %d, want 2", r.SuspendsBad)
	}
}

// A counter that went backwards (a source re-registered across the suspend)
// must not wrap round to a huge delta.
func TestDiffWakeupsFloorsARestartedCounter(t *testing.T) {
	before := wakeSnapshot{Sources: map[string]wakeCounts{"x": {Events: 50, Wakeups: 5}}, Suspends: 3}
	after := wakeSnapshot{Sources: map[string]wakeCounts{"x": {Events: 2, Wakeups: 1}}, Suspends: 1}
	r := diffWakeups(before, after, wakeExtras{})
	if r.WokenBy != nil || r.Activity != nil || r.SuspendsOK != 0 {
		t.Errorf("report = %+v, want nothing counted", r)
	}
}

// fakeWakeTree points the readers at a temp tree and restores them afterwards.
func fakeWakeTree(t *testing.T) (class, power string) {
	t.Helper()
	root := t.TempDir()
	class, power = root+"/class/wakeup", root+"/power"
	interrupts := root + "/interrupts"
	oldClass, oldPower, oldIRQ, oldACPI := wakeupClassDir, sysPowerDir, procInterruptsPath, acpiInterruptsDir
	wakeupClassDir, sysPowerDir, procInterruptsPath, acpiInterruptsDir = class, power, interrupts, root+"/acpi"
	t.Cleanup(func() {
		wakeupClassDir, sysPowerDir, procInterruptsPath, acpiInterruptsDir = oldClass, oldPower, oldIRQ, oldACPI
	})
	write(t, interrupts, "            CPU0       CPU1\n"+
		"   1:          0          0  IR-IO-APIC    1-edge      i8042\n"+
		"   9:         12          3  IR-IO-APIC    9-fasteoi   acpi\n"+
		"  45:          0       4410  IR-PCI-MSIX-0000:c4:00.3    0-edge      xhci_hcd\n"+
		" NMI:          0          0   Non-maskable interrupts\n")
	return class, power
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func source(t *testing.T, class, n, name, events, wakeups string) {
	t.Helper()
	write(t, class+"/"+n+"/name", name+"\n")
	write(t, class+"/"+n+"/event_count", events+"\n")
	write(t, class+"/"+n+"/wakeup_count", wakeups+"\n")
}

func TestSnapshotWakeupsReadsTheClass(t *testing.T) {
	class, power := fakeWakeTree(t)
	source(t, class, "wakeup0", "PNP0C09:00", "7", "1")
	source(t, class, "wakeup1", "alarmtimer", "2", "0")
	source(t, class, "wakeup2", "alarmtimer", "3", "1")
	source(t, class, "wakeup3", "device:44", "1", "0")
	if err := os.Symlink("../../../devices/pci0000:00/0000:00:14.0", class+"/wakeup3/device"); err != nil {
		t.Fatal(err)
	}
	write(t, power+"/suspend_stats/success", "4\n")
	write(t, power+"/suspend_stats/fail", "1\n")
	write(t, filepath.Dir(class)+"/../acpi/sci", "     167\n")
	write(t, filepath.Dir(class)+"/../acpi/gpe0A", "     167  EN     enabled      unmasked\n")
	write(t, filepath.Dir(class)+"/../acpi/gpe03", "       0  EN     disabled     unmasked\n")

	s := snapshotWakeups()
	if got := s.Sources["PNP0C09:00"]; got != (wakeCounts{Events: 7, Wakeups: 1}) {
		t.Errorf("PNP0C09:00 = %+v", got)
	}
	if got := s.Sources["alarmtimer"]; got != (wakeCounts{Events: 5, Wakeups: 1}) {
		t.Errorf("alarmtimer = %+v, want the two same-named sources summed", got)
	}
	if _, ok := s.Sources["device:44[0000:00:14.0]"]; !ok {
		t.Errorf("sources = %v, want the opaque name labelled with its device", s.Sources)
	}
	if want := map[string]uint64{"sci": 167, "gpe0A": 167}; !maps.Equal(s.ACPI, want) {
		t.Errorf("ACPI = %v, want %v (zero counts left out)", s.ACPI, want)
	}
	if s.Suspends != 4 || s.Failed != 1 {
		t.Errorf("suspend totals = %d/%d, want 4/1", s.Suspends, s.Failed)
	}
	if s.At.IsZero() || s.At != s.At.Round(0) {
		t.Error("At must be wall-clock time with no monotonic reading")
	}
}

func TestReadWakeExtras(t *testing.T) {
	_, power := fakeWakeTree(t)
	write(t, power+"/pm_wakeup_irq", "9\n")
	write(t, power+"/suspend_stats/last_hw_sleep", "1500000\n")
	write(t, power+"/suspend_stats/last_failed_dev", "i2c-ELAN9008:00\n")
	write(t, power+"/suspend_stats/last_failed_step", "suspend\n")
	write(t, power+"/suspend_stats/last_failed_errno", "-16\n")

	x := readWakeExtras(false)
	if x.IRQ != "9 acpi" {
		t.Errorf("IRQ = %q, want the handler from /proc/interrupts", x.IRQ)
	}
	if x.HWSleep != 1500*time.Millisecond {
		t.Errorf("HWSleep = %v, want 1.5s", x.HWSleep)
	}
	if x.FailedDev != "" {
		t.Error("a stale last_failed_dev was reported for a suspend that did not fail")
	}
	if x = readWakeExtras(true); x.FailedDev != "i2c-ELAN9008:00" || x.FailedStep != "suspend" || x.FailedErr != "-16" {
		t.Errorf("failure fields = %+v", x)
	}
}

func TestIRQHandler(t *testing.T) {
	fakeWakeTree(t)
	for irq, want := range map[string]string{"9": "acpi", "45": "xhci_hcd", "1": "i8042", "77": ""} {
		if got := irqHandler(irq); got != want {
			t.Errorf("irqHandler(%s) = %q, want %q", irq, got, want)
		}
	}
}

// No wakeup class at all (a container, an old kernel) still yields a usable
// snapshot rather than a failure.
func TestSnapshotWakeupsWithoutTheClass(t *testing.T) {
	fakeWakeTree(t)
	s := snapshotWakeups()
	if len(s.Sources) != 0 || s.At.IsZero() {
		t.Errorf("snapshot = %+v", s)
	}
}
