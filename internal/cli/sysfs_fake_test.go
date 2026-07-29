package cli

// sysfs_fake_test.go — a temporary directory tree standing in for the sysfs
// nodes this package reads and writes, so the hardware-facing helpers can be
// exercised without a Z13 attached.

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// fakeSysfs is a temp-dir sysfs tree with the package path vars pointed at it.
type fakeSysfs struct {
	root       string
	hwmon      string // asus_custom_fan_curve device
	hwmonRead  string // asus device (RPM + pwm_enable)
	hwmonTemp  string // k10temp device
	profileDir string // a platform-profile class device
	ppt        string
	smu        string
	battery    string
	firmware   string
	ppdCalls   *[]string // powerprofilesctl profiles the stub recorded
}

// newFakeSysfs builds the tree and redirects every path var for the test's
// lifetime, restoring the originals on cleanup.
func newFakeSysfs(t *testing.T) *fakeSysfs {
	t.Helper()
	root := t.TempDir()

	f := &fakeSysfs{
		root:       root,
		hwmon:      root + "/hwmon/hwmon0",
		hwmonRead:  root + "/hwmon/hwmon1",
		hwmonTemp:  root + "/hwmon/hwmon2",
		profileDir: root + "/platform-profile/platform-profile-0",
		ppt:        root + "/asus-nb-wmi",
		smu:        root + "/ryzen_smu_drv",
		battery:    root + "/power_supply/BAT0",
		firmware:   root + "/firmware-attributes",
	}
	for _, d := range []string{f.hwmon, f.hwmonRead, f.hwmonTemp, f.profileDir, f.ppt, f.smu, f.battery,
		f.firmware + "/boot_sound", f.firmware + "/panel_overdrive"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", d, err)
		}
	}

	f.writeFile(t, f.hwmon+"/name", hwmonNameCurves)
	f.writeFile(t, f.hwmonRead+"/name", hwmonNameReadings)
	f.writeFile(t, f.hwmonTemp+"/name", "k10temp")

	// Never shell out to the live power-profiles-daemon from a test.
	origPPD := ppdRunner
	ppdCalls := []string{}
	ppdRunner = func(p string) { ppdCalls = append(ppdCalls, p) }
	f.ppdCalls = &ppdCalls
	t.Cleanup(func() { ppdRunner = origPPD })

	swap(t, &sysHwmonDir, root+"/hwmon")
	swap(t, &sysProfileDir, root+"/platform-profile")
	swap(t, &sysProfileACPI, root+"/acpi_platform_profile")
	swap(t, &sysPowerSupplyDir, root+"/power_supply")
	swap(t, &sysFirmwareAttrDir, f.firmware)
	swap(t, &pptBasePath, f.ppt)
	swap(t, &smuDriverPath, f.smu)
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

func (f *fakeSysfs) readInt(t *testing.T, path string) int {
	t.Helper()
	v, err := readIntFile(path)
	if err != nil {
		t.Fatalf("readIntFile(%s) = %v", path, err)
	}
	return v
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

// seedFanCurveFiles pre-creates the 8 curve points and pwm_enable for both fans
// so read paths have something to find.
func (f *fakeSysfs) seedFanCurveFiles(t *testing.T, temp, pwm int) {
	t.Helper()
	for _, fan := range fanNames {
		for i := 1; i <= fanCurvePoints; i++ {
			f.writeFile(t, f.hwmon+"/pwm"+itoa(fan.index)+"_auto_point"+itoa(i)+"_temp", itoa(temp+i))
			f.writeFile(t, f.hwmon+"/pwm"+itoa(fan.index)+"_auto_point"+itoa(i)+"_pwm", itoa(pwm+i))
		}
		f.writeFile(t, f.hwmon+"/pwm"+itoa(fan.index)+"_enable", "2")
		f.writeFile(t, f.hwmonRead+"/pwm"+itoa(fan.index)+"_enable", "2")
		f.writeFile(t, f.hwmonRead+"/fan"+itoa(fan.index)+"_input", itoa(3000+fan.index))
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// fakeSMU emulates the ryzen_smu mailbox: a command write is answered by the
// configured response code on the following read, which plain files cannot do.
type fakeSMU struct {
	mu       sync.Mutex
	response uint32
	args     []byte
	writes   int
	failRead bool
}

// install swaps in the fake's I/O for the duration of the test and resets the
// cached SMUProbeUndervolt result so each test observes its own fake.
func (s *fakeSMU) install(t *testing.T) {
	t.Helper()
	origR, origW := smuReadFile, smuWriteFile
	smuWriteFile = func(path string, data []byte, _ os.FileMode) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.writes++
		if filepath.Base(path) == "smu_args" {
			s.args = append([]byte(nil), data...)
		}
		return nil
	}
	smuReadFile = func(path string) ([]byte, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.failRead {
			return nil, os.ErrPermission
		}
		if filepath.Base(path) == "smu_args" {
			if s.args == nil {
				return make([]byte, 24), nil
			}
			return s.args, nil
		}
		buf := make([]byte, 4)
		buf[0] = byte(s.response)
		buf[1] = byte(s.response >> 8)
		buf[2] = byte(s.response >> 16)
		buf[3] = byte(s.response >> 24)
		return buf, nil
	}
	resetSMUProbe(t)
	t.Cleanup(func() { smuReadFile, smuWriteFile = origR, origW })
}

// resetSMUProbe clears the cached SMUProbeUndervolt result. The probe is a
// sync.Once in production; tests need each case to re-probe.
func resetSMUProbe(t *testing.T) {
	t.Helper()
	smuProbeOnce = new(sync.Once)
	smuProbeOK = false
	t.Cleanup(func() {
		smuProbeOnce = new(sync.Once)
		smuProbeOK = false
	})
}
