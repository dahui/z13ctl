package cli

// sysfs_fake_test.go — a temporary directory tree standing in for the sysfs
// nodes this package reads and writes, so the hardware-facing helpers can be
// exercised without a Z13 attached.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dahui/z13ctl/api"
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
	ac         string // the Mains power supply device
	firmware   string
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

	// The five asus-nb-wmi PPT attributes, holding the kernel's initial 5 W
	// cache. There is no asus-armoury PPT tree unless a test asks for one with
	// withArmouryPPT, so the legacy interface is the one in use by default.
	for _, l := range pptLimits {
		f.writeFile(t, f.ppt+"/"+l.legacy, itoa(TDPMin))
	}

	f.writeFile(t, f.hwmon+"/name", hwmonNameCurves)
	f.writeFile(t, f.hwmonRead+"/name", hwmonNameReadings)
	f.writeFile(t, f.hwmonTemp+"/name", "k10temp")

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
	f.writeFile(t, root+"/power_supply/ucsi-source-psy-USBC000:001/type", "USB")
	f.writeFile(t, root+"/power_supply/ucsi-source-psy-USBC000:001/online", "1")

	// Never shell out to the live power-profiles-daemon from a test.
	origPPD := ppdRunner
	ppdCalls := []string{}
	ppdRunner = func(p string) { ppdCalls = append(ppdCalls, p) }
	f.ppdCalls = &ppdCalls
	t.Cleanup(func() { ppdRunner = origPPD })

	// Emulate the firmware: a fan release re-applies the active profile's own
	// power limits over whatever was written (measured on a GZ302EA, issue #22).
	// The stock table stands in for the firmware's row; balanced when the fake
	// tree names no profile.
	origRelease := fanReleaseHook
	fanReleaseHook = func() { f.firmwareProfileReset(t) }
	t.Cleanup(func() { fanReleaseHook = origRelease })

	swap(t, &sysHwmonDir, root+"/hwmon")
	swap(t, &sysProfileDir, root+"/platform-profile")
	swap(t, &sysProfileACPI, root+"/acpi_platform_profile")
	swap(t, &sysPowerSupplyDir, root+"/power_supply")
	swap(t, &sysFirmwareAttrDir, f.firmware)
	swap(t, &pptBasePath, f.ppt)
	swap(t, &smuDriverPath, f.smu)
	return f
}

// fakeFirmwarePPT is what the fake firmware re-applies. It differs from
// StockProfilePPT on purpose, as the real firmware's limits do: on a GZ302EA
// balanced held 52 W on its own limits and 63–66 W on the table's row. A test
// that ends on the table rather than the firmware's own limits must fail.
var fakeFirmwarePPT = map[string]api.TDPState{
	"quiet":       {PL1SPL: 40, PL2SPPT: 40, FPPT: 40, APUSPPT: 40, PlatformSPPT: 40},
	"balanced":    {PL1SPL: 52, PL2SPPT: 52, FPPT: 52, APUSPPT: 52, PlatformSPPT: 52},
	"performance": {PL1SPL: 70, PL2SPPT: 70, FPPT: 70, APUSPPT: 70, PlatformSPPT: 70},
}

// firmwareProfileReset overwrites the PPT files with the active profile's
// firmware limits, as the firmware does on a fan release or a platform_profile
// write.
func (f *fakeSysfs) firmwareProfileReset(t *testing.T) {
	t.Helper()
	profile := "balanced"
	if data, err := os.ReadFile(FindProfilePath()); err == nil {
		if p := strings.TrimSpace(string(data)); p != "" {
			profile = p
		}
	}
	row, ok := fakeFirmwarePPT[profile]
	if !ok {
		return
	}
	// Both interfaces' files stand for "the limit in force" here, which the
	// real caches do not track; the fake models the firmware, not the caches.
	for _, l := range pptLimits {
		v := *l.field(&row)
		f.writeFile(t, f.ppt+"/"+l.legacy, itoa(v))
		if cur := f.firmware + "/" + l.armoury + "/current_value"; fileExists(cur) {
			f.writeFile(t, cur, itoa(v))
		}
	}
}

// armouryFakeBounds are the GZ302EA's asus-armoury PPT bounds on AC:
// min, default, max.
var armouryFakeBounds = map[string][3]int{
	"ppt_pl1_spl":  {28, 60, 80},
	"ppt_pl2_sppt": {32, 75, 92},
	"ppt_pl3_fppt": {45, 86, 93},
}

// withArmouryPPT adds asus-armoury's three PPT attributes to the fake, each
// current_value at its default as the driver seeds it, so asus-armoury becomes
// the interface in use. The asus-nb-wmi files stay, as they do on a real 7.x
// kernel built with the deprecated attributes.
func (f *fakeSysfs) withArmouryPPT(t *testing.T) {
	t.Helper()
	for attr, b := range armouryFakeBounds {
		dir := f.firmware + "/" + attr
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f.writeFile(t, dir+"/min_value", itoa(b[0]))
		f.writeFile(t, dir+"/default_value", itoa(b[1]))
		f.writeFile(t, dir+"/max_value", itoa(b[2]))
		f.writeFile(t, dir+"/current_value", itoa(b[1]))
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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
	smuProbeResult.Store(0)
	t.Cleanup(func() {
		smuProbeOnce = new(sync.Once)
		smuProbeOK = false
		smuProbeResult.Store(0)
	})
}
