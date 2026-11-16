package asus

// sysfs_fake_test.go — a temporary directory tree standing in for the sysfs
// nodes this package reads and writes, so the hardware-facing helpers can be
// exercised without a Z13 attached.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeSysfs is a temp-dir sysfs tree with the package path vars pointed at it.
type fakeSysfs struct {
	root      string
	hwmon     string // asus_custom_fan_curve device
	hwmonRead string // asus device (RPM + pwm_enable)
	ppt       string
	firmware  string
}

// newFakeSysfs builds the tree and redirects every path var for the test's
// lifetime, restoring the originals on cleanup.
func newFakeSysfs(t *testing.T) *fakeSysfs {
	t.Helper()
	root := t.TempDir()

	f := &fakeSysfs{
		root:      root,
		hwmon:     root + "/hwmon/hwmon0",
		hwmonRead: root + "/hwmon/hwmon1",
		ppt:       root + "/asus-nb-wmi",
		firmware:  root + "/firmware-attributes",
	}
	for _, d := range []string{f.hwmon, f.hwmonRead, f.ppt,
		f.firmware + "/boot_sound", f.firmware + "/panel_overdrive"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", d, err)
		}
	}

	f.writeFile(t, f.hwmon+"/name", hwmonNameCurves)
	f.writeFile(t, f.hwmonRead+"/name", hwmonNameReadings)

	// The five asus-nb-wmi PPT attributes, at the 5 W the kernel caches on module
	// load. Backend selection looks for ppt_pl1_spl, so a tree without them has
	// no power limit interface at all.
	seedLegacyPPT(t, f.ppt)

	swap(t, &sysHwmonDir, root+"/hwmon")
	swap(t, &sysFirmwareAttrDir, f.firmware)
	swap(t, &pptBasePath, f.ppt)
	return f
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
