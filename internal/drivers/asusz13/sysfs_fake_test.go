package asusz13

// sysfs_fake_test.go — a temporary directory tree standing in for the sysfs
// nodes this package reads and writes, so the hardware-facing helpers can be
// exercised without a Z13 attached.

import (
	"encoding/binary"
	"fmt"
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
	ac         string // the Mains power supply device
	firmware   string
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
		hwmon:      root + "/hwmon/hwmon0",
		hwmonRead:  root + "/hwmon/hwmon1",
		hwmonTemp:  root + "/hwmon/hwmon2",
		profileDir: root + "/platform-profile/platform-profile-0",
		ppt:        root + "/asus-nb-wmi",
		smu:        root + "/ryzen_smu_drv",
		battery:    root + "/power_supply/BAT0",
		firmware:   root + "/firmware-attributes",
		powercap:   root + "/powercap/intel-rapl:0",
	}
	for _, d := range []string{f.hwmon, f.hwmonRead, f.hwmonTemp, f.profileDir, f.ppt, f.smu, f.battery,
		f.firmware + "/boot_sound", f.firmware + "/panel_overdrive", f.powercap,
		root + "/powercap/intel-rapl:0:0"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", d, err)
		}
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

	// The five asus-nb-wmi PPT attributes, at the 5 W the kernel caches on module
	// load. Backend selection looks for ppt_pl1_spl, so a tree without them has
	// no power limit interface at all.
	seedLegacyPPT(t, f.ppt)

	swap(t, &sysHwmonDir, root+"/hwmon")
	swap(t, &sysProfileDir, root+"/platform-profile")
	swap(t, &sysProfileACPI, root+"/acpi_platform_profile")
	swap(t, &sysPowerSupplyDir, root+"/power_supply")
	swap(t, &sysFirmwareAttrDir, f.firmware)
	swap(t, &sysPowercapDir, root+"/powercap")
	swap(t, &pptBasePath, f.ppt)
	swap(t, &smuDriverPath, f.smu)
	// Read-only roots too, so nothing a test asserts depends on the GPU, NPU
	// or thermal zones of the machine running it.
	swap(t, &sysDrmDir, root+"/drm")
	swap(t, &sysAccelDir, root+"/accel")
	swap(t, &sysThermalDir, root+"/thermal")
	// The Z13's CPU, so the Curve Optimizer command is the one the hardware
	// takes; withCPU replaces it.
	swap(t, &procCPUInfoPath, root+"/cpuinfo")
	f.withCPU(t, "AuthenticAMD", 0x1A, 112)
	return f
}

// withCPU writes a two-processor cpuinfo naming the given CPU.
func (f *fakeSysfs) withCPU(t *testing.T, vendor string, family, model int) {
	t.Helper()
	block := fmt.Sprintf("processor\t: %%d\nvendor_id\t: %s\ncpu family\t: %d\nmodel\t\t: %d\nmodel name\t: test\n\n",
		vendor, family, model)
	f.writeFile(t, procCPUInfoPath, fmt.Sprintf(block, 0)+fmt.Sprintf(block, 1))
}

// seedLegacyPPT writes the five asus-nb-wmi PPT attributes into dir at 5 W.
func seedLegacyPPT(t *testing.T, dir string) {
	t.Helper()
	for _, l := range pptLimits {
		if err := os.WriteFile(dir+"/"+l.legacy, []byte("5\n"), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", l.legacy, err)
		}
	}
}

// armouryFakeBounds are the GZ302EA's asus-armoury PPT bounds on AC (min, max,
// default), as 7.x kernels report them.
var armouryFakeBounds = map[string][3]int{
	"ppt_pl1_spl":  {28, 80, 60},
	"ppt_pl2_sppt": {32, 92, 75},
	"ppt_pl3_fppt": {45, 93, 86},
}

// withArmouryPPT adds asus-armoury's three PPT attributes, each at its default,
// which makes armoury the interface in use. The asus-nb-wmi files stay, so a
// test can prove they are never touched.
func (f *fakeSysfs) withArmouryPPT(t *testing.T) {
	t.Helper()
	for name, b := range armouryFakeBounds {
		dir := f.firmware + "/" + name
		f.writeFile(t, dir+"/min_value", strconv.Itoa(b[0]))
		f.writeFile(t, dir+"/max_value", strconv.Itoa(b[1]))
		f.writeFile(t, dir+"/default_value", strconv.Itoa(b[2]))
		f.writeFile(t, dir+"/current_value", strconv.Itoa(b[2]))
	}
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
	for _, fan := range fakeFanChannels {
		for i := 1; i <= fakeCurvePoints; i++ {
			f.writeFile(t, f.hwmon+"/pwm"+itoa(fan)+"_auto_point"+itoa(i)+"_temp", itoa(temp+i))
			f.writeFile(t, f.hwmon+"/pwm"+itoa(fan)+"_auto_point"+itoa(i)+"_pwm", itoa(pwm+i))
		}
		f.writeFile(t, f.hwmon+"/pwm"+itoa(fan)+"_enable", "2")
		f.writeFile(t, f.hwmonRead+"/pwm"+itoa(fan)+"_enable", "2")
		f.writeFile(t, f.hwmonRead+"/fan"+itoa(fan)+"_input", itoa(3000+fan))
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// The fake's fan hardware: the Z13's two channels and eight-point curves. Only
// the fake knows these numbers — the driver enumerates them.
var fakeFanChannels = []int{1, 2}

const fakeCurvePoints = 8

// fakeSMU emulates the ryzen_smu mailbox: a command write is answered by the
// configured response code on the following read, which plain files cannot do.
type fakeSMU struct {
	mu       sync.Mutex
	response uint32
	args     []byte
	writes   int
	failRead bool
	// cmds is every command written, keyed by mailbox file.
	cmds map[string][]uint32
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
		if name := filepath.Base(path); name == "smu_args" {
			s.args = append([]byte(nil), data...)
		} else if len(data) == 4 {
			if s.cmds == nil {
				s.cmds = map[string][]uint32{}
			}
			s.cmds[name] = append(s.cmds[name], binary.LittleEndian.Uint32(data))
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
