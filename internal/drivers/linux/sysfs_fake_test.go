package linux

// sysfs_fake_test.go — a temporary directory tree standing in for the sysfs
// nodes this package reads and writes: platform-profile, power_supply,
// powercap and the thermal zones.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeSysfs is a temp-dir sysfs tree with the package path vars pointed at it.
type fakeSysfs struct {
	root       string
	profileDir string // a platform-profile class device
	battery    string
	ac         string // the Mains power supply device
	powercap   string
	ppdCalls   *[]string // powerprofilesctl profiles the stub recorded
}

// setACOnline flips the mains adapter between plugged and unplugged.
func (f *fakeSysfs) setACOnline(t *testing.T, online bool) {
	t.Helper()
	v := "0"
	if online {
		v = "1"
	}
	f.writeFile(t, f.ac+"/online", v)
}

// newFakeSysfs builds the tree and redirects every path var for the test's
// lifetime, restoring the originals on cleanup.
func newFakeSysfs(t *testing.T) *fakeSysfs {
	t.Helper()
	root := t.TempDir()

	f := &fakeSysfs{
		root:       root,
		profileDir: root + "/platform-profile/platform-profile-0",
		battery:    root + "/power_supply/BAT0",
		powercap:   root + "/powercap/intel-rapl:0",
	}
	for _, d := range []string{f.profileDir, f.battery, f.powercap, root + "/powercap/intel-rapl:0:0"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", d, err)
		}
	}

	// The mains adapter, plus the two decoys that also carry an "online" file on
	// a real Z13: the detachable keyboard's HID battery and a USB-C PD source.
	// Any helper that globs */online instead of filtering on type picks these up
	// and reports mains power whenever the cover is attached.
	f.ac = root + "/power_supply/AC0"
	f.writeFile(t, f.ac+"/type", "Mains")
	f.writeFile(t, f.ac+"/online", "1")
	f.writeFile(t, f.battery+"/type", "Battery")
	f.writeFile(t, root+"/power_supply/hid-0018:04F3:43C7.0008-battery-7/type", "Battery")
	f.writeFile(t, root+"/power_supply/hid-0018:04F3:43C7.0008-battery-7/online", "1")
	// As the real one reports: a peripheral's pack, not the system's.
	f.writeFile(t, root+"/power_supply/hid-0018:04F3:43C7.0008-battery-7/scope", "Device")
	f.writeFile(t, root+"/power_supply/ucsi-source-psy-USBC000:001/type", "USB")
	f.writeFile(t, root+"/power_supply/ucsi-source-psy-USBC000:001/scope", "System")
	f.writeFile(t, root+"/power_supply/ucsi-source-psy-USBC000:001/online", "1")

	// The Z13's pack is the *energy* kind: power_now in microwatts, no
	// current_now at all. A reader written only for the charge form finds
	// nothing here, which is the point of building it this way. capacity is on
	// every pack; the full-charge attributes (energy_full and friends) are
	// deliberately NOT here — the health test's "nothing readable" case needs
	// their absence, so tests that want them write their own.
	f.writeFile(t, f.battery+"/power_now", "12500000")
	f.writeFile(t, f.battery+"/voltage_now", "16124000")
	f.writeFile(t, f.battery+"/status", "Discharging")
	f.writeFile(t, f.battery+"/capacity", "81")

	// powercap: the package domain plus a sub-domain, whose energy is a *part*
	// of the package's — a reader that summed them would double-count.
	f.writeFile(t, f.powercap+"/name", "package-0")
	f.writeFile(t, f.powercap+"/energy_uj", "1000000")
	f.writeFile(t, f.powercap+"/max_energy_range_uj", "262143328850")
	f.writeFile(t, root+"/powercap/intel-rapl:0:0/name", "core")
	f.writeFile(t, root+"/powercap/intel-rapl:0:0/energy_uj", "400000")

	// Never shell out to the live power-profiles-daemon from a test.
	origPPD := ppdRunner
	ppdCalls := []string{}
	ppdRunner = func(p string) { ppdCalls = append(ppdCalls, p) }
	f.ppdCalls = &ppdCalls
	t.Cleanup(func() { ppdRunner = origPPD })

	swap(t, &sysProfileDir, root+"/platform-profile")
	swap(t, &sysProfileACPI, root+"/acpi_platform_profile")
	swap(t, &sysPowerSupplyDir, root+"/power_supply")
	swap(t, &sysPowercapDir, root+"/powercap")
	// Read-only roots too, so nothing a test asserts depends on the thermal
	// zones of the machine running it.
	swap(t, &sysThermalDir, root+"/thermal")
	return f
}

// swap points a path var at v and restores it when the test ends.
func swap(t *testing.T, target *string, v string) {
	t.Helper()
	orig := *target
	*target = v
	t.Cleanup(func() { *target = orig })
}

func (f *fakeSysfs) writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
}

// withProfileDevice adds a platform-profile class device with the given
// choices, returning its base directory.
func (f *fakeSysfs) withProfileDevice(t *testing.T, name, choices, current string) string {
	t.Helper()
	base := f.root + "/platform-profile/" + name
	f.writeFile(t, base+"/choices", choices)
	f.writeFile(t, base+"/profile", current)
	return base
}

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", path, err)
	}
	return strings.TrimSpace(string(data))
}

func itoa(i int) string { return strconv.Itoa(i) }
