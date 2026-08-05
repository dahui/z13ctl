package cli

// fan_sysfs_test.go — hwmon and platform-profile helpers exercised against the
// fake sysfs tree from sysfs_fake_test.go.

import (
	"os"
	"strings"
	"testing"

	"github.com/dahui/z13ctl/api"
)

func TestFindFanHwmonPathByName(t *testing.T) {
	f := newFakeSysfs(t)

	if got, want := FindFanCurveHwmonPath(), f.hwmon; got != want {
		t.Errorf("FindFanCurveHwmonPath() = %q, want %q", got, want)
	}
	if got, want := FindFanReadingsHwmonPath(), f.hwmonRead; got != want {
		t.Errorf("FindFanReadingsHwmonPath() = %q, want %q", got, want)
	}
	if got := FindFanHwmonPath("nonexistent"); got != "" {
		t.Errorf("FindFanHwmonPath(nonexistent) = %q, want \"\"", got)
	}
}

func TestFindFanHwmonPathMissingDir(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir()+"/absent")
	if got := FindFanHwmonPath("asus"); got != "" {
		t.Errorf("FindFanHwmonPath() = %q, want \"\" when hwmon dir is absent", got)
	}
}

func TestSetBothFanCurvesWritesBothFansAndEnablesCustom(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)
	seedReadingsSentinel(t, f)

	points := make([]api.FanCurvePoint, fanCurvePoints)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 50 + i*20}
	}
	if err := SetBothFanCurves(points); err != nil {
		t.Fatalf("SetBothFanCurves() = %v, want nil", err)
	}

	for _, fan := range fanNames {
		for i, p := range points {
			gotTemp := f.readInt(t, f.hwmon+"/pwm"+itoa(fan.index)+"_auto_point"+itoa(i+1)+"_temp")
			gotPWM := f.readInt(t, f.hwmon+"/pwm"+itoa(fan.index)+"_auto_point"+itoa(i+1)+"_pwm")
			if gotTemp != p.Temp || gotPWM != p.PWM {
				t.Errorf("fan%d point %d = %d:%d, want %d:%d", fan.index, i+1, gotTemp, gotPWM, p.Temp, p.PWM)
			}
		}
		// Custom mode must be enabled on the curve device...
		if got := f.readInt(t, f.hwmon+"/pwm"+itoa(fan.index)+"_enable"); got != 1 {
			t.Errorf("curve device fan%d pwm_enable = %d, want 1 (custom)", fan.index, got)
		}
		// ...and the base "asus" device must be left untouched. Its
		// pwm1_enable_store rejects mode 1 on the Z13 (fan_type SPEC83) and, where
		// it is accepted, clears custom_fan_curves[*].enabled for every fan —
		// undoing the curve that was just enabled. See issue #15.
		if got := f.readInt(t, f.hwmonRead+"/pwm"+itoa(fan.index)+"_enable"); got != readingsSentinel {
			t.Errorf("readings device fan%d pwm_enable = %d, want %d (untouched)", fan.index, got, readingsSentinel)
		}
	}
}

// readingsSentinel is written to the base "asus" device's pwm_enable files so a
// test can tell "left alone" from "written with the same value".
const readingsSentinel = 9

// seedReadingsSentinel marks both readings-device pwm_enable files.
func seedReadingsSentinel(t *testing.T, f *fakeSysfs) {
	t.Helper()
	for _, fan := range fanNames {
		f.writeFile(t, f.hwmonRead+"/pwm"+itoa(fan.index)+"_enable", itoa(readingsSentinel))
	}
}

func TestSetBothFanCurvesRejectsWrongPointCount(t *testing.T) {
	newFakeSysfs(t)
	err := SetBothFanCurves([]api.FanCurvePoint{{Temp: 30, PWM: 100}})
	if err == nil {
		t.Fatal("SetBothFanCurves() with 1 point = nil, want an error")
	}
}

func TestSetBothFanCurvesErrorsWhenHwmonMissing(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	points := make([]api.FanCurvePoint, fanCurvePoints)
	if err := SetBothFanCurves(points); err == nil {
		t.Error("SetBothFanCurves() = nil, want an error when the hwmon device is absent")
	}
}

func TestResetAllFanCurvesSetsAutoOnCurveDeviceOnly(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)
	seedReadingsSentinel(t, f)

	// Put both fans in custom mode first so the reset is observable.
	if err := setAllFanModes(1); err != nil {
		t.Fatalf("setAllFanModes(1) = %v", err)
	}
	if err := ResetAllFanCurves(); err != nil {
		t.Fatalf("ResetAllFanCurves() = %v, want nil", err)
	}
	for _, fan := range fanNames {
		if got := f.readInt(t, f.hwmon+"/pwm"+itoa(fan.index)+"_enable"); got != 2 {
			t.Errorf("curve device fan%d pwm_enable = %d, want 2 (auto)", fan.index, got)
		}
		if got := f.readInt(t, f.hwmonRead+"/pwm"+itoa(fan.index)+"_enable"); got != readingsSentinel {
			t.Errorf("readings device fan%d pwm_enable = %d, want %d (untouched)", fan.index, got, readingsSentinel)
		}
	}
}

// TestSetFanModeLeavesReadingsDeviceAlone is the direct guard on the mode-1/2
// path: nothing but the curve device may be written, because the base device's
// store handler clears the driver's custom-curve enabled flag as a side effect.
func TestSetFanModeLeavesReadingsDeviceAlone(t *testing.T) {
	for _, mode := range []int{1, 2} {
		f := newFakeSysfs(t)
		f.seedFanCurveFiles(t, 40, 100)
		seedReadingsSentinel(t, f)

		if err := setFanMode(1, mode); err != nil {
			t.Fatalf("setFanMode(1, %d) = %v, want nil", mode, err)
		}
		if got := f.readInt(t, f.hwmon+"/pwm1_enable"); got != mode {
			t.Errorf("curve device pwm1_enable = %d, want %d", got, mode)
		}
		if got := f.readInt(t, f.hwmonRead+"/pwm1_enable"); got != readingsSentinel {
			t.Errorf("readings device pwm1_enable = %d, want %d (untouched)", got, readingsSentinel)
		}
	}
}

// TestSetBothFanCurvesFailsWhenCurveDoesNotStick is the regression test for
// issue #15. The kernel accepts the pwm_enable write and then leaves the mode at
// auto — which is what a concurrent platform_profile write produces — and the
// caller must find out rather than being told the curve was applied.
func TestSetBothFanCurvesFailsWhenCurveDoesNotStick(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100) // seeds pwm_enable = 2 (auto)

	orig := fanWriteInt
	fanWriteInt = func(string, int) error { return nil } // accepted, no effect
	t.Cleanup(func() { fanWriteInt = orig })

	points := make([]api.FanCurvePoint, fanCurvePoints)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 50 + i*20}
	}
	err := SetBothFanCurves(points)
	if err == nil {
		t.Fatal("SetBothFanCurves() = nil, want an error when the kernel does not honour the curve")
	}
	if !strings.Contains(err.Error(), "pwm1_enable") {
		t.Errorf("error %q does not name the attribute that proved it", err)
	}
	if got := f.readInt(t, f.hwmon+"/pwm1_enable"); got != 2 {
		t.Fatalf("test setup: curve device pwm1_enable = %d, want 2", got)
	}
}

func TestVerifyFanCurveActive(t *testing.T) {
	cases := []struct {
		name    string
		modes   [fanCount]string // "" means do not create the file
		wantErr bool
	}{
		{"both custom", [fanCount]string{"1", "1"}, false},
		{"fan1 dropped to auto", [fanCount]string{"2", "1"}, true},
		{"fan2 dropped to auto", [fanCount]string{"1", "2"}, true},
		{"full speed is not custom", [fanCount]string{"0", "1"}, true},
		// Unverifiable is not the same as failed: a SKU exposing only the CPU
		// curve must keep working, floor and all.
		{"fan2 missing", [fanCount]string{"1", ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSysfs(t)
			for i, fan := range fanNames {
				if tc.modes[i] == "" {
					continue
				}
				f.writeFile(t, f.hwmon+"/pwm"+itoa(fan.index)+"_enable", tc.modes[i])
			}
			err := VerifyFanCurveActive()
			if (err != nil) != tc.wantErr {
				t.Errorf("VerifyFanCurveActive() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestVerifyFanCurveActiveErrorsWhenHwmonMissing(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	if err := VerifyFanCurveActive(); err == nil {
		t.Error("VerifyFanCurveActive() = nil, want an error when the hwmon device is absent")
	}
}

// TestSetAllFansFullSpeedUsesReadingsDeviceOnly pins the hardware quirk
// documented on SetAllFansFullSpeed: only the base "asus" device accepts
// pwm_enable=0, and only pwm1_enable is functional.
func TestSetAllFansFullSpeedUsesReadingsDeviceOnly(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)

	if err := SetAllFansFullSpeed(); err != nil {
		t.Fatalf("SetAllFansFullSpeed() = %v, want nil", err)
	}
	if got := f.readInt(t, f.hwmonRead+"/pwm1_enable"); got != 0 {
		t.Errorf("readings pwm1_enable = %d, want 0 (full-speed)", got)
	}
	// The curve device must be left alone — it rejects mode 0 with EINVAL.
	if got := f.readInt(t, f.hwmon+"/pwm1_enable"); got == 0 {
		t.Error("curve device pwm1_enable was set to 0; that device rejects full-speed mode")
	}
}

func TestSetFanModeFullSpeedErrorsWithoutReadingsDevice(t *testing.T) {
	root := t.TempDir()
	swap(t, &sysHwmonDir, root)
	if err := setFanMode(1, 0); err == nil {
		t.Error("setFanMode(1, 0) = nil, want an error when the readings device is absent")
	}
}

func TestReadBothFanCurvesRoundTrip(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)

	points := make([]api.FanCurvePoint, fanCurvePoints)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 60 + i*10}
	}
	if err := SetBothFanCurves(points); err != nil {
		t.Fatalf("SetBothFanCurves() = %v", err)
	}
	curves, err := ReadBothFanCurves()
	if err != nil {
		t.Fatalf("ReadBothFanCurves() = %v, want nil", err)
	}
	for fi := range curves {
		for i, p := range curves[fi] {
			if p != points[i] {
				t.Errorf("fan%d point %d = %+v, want %+v", fi+1, i+1, p, points[i])
			}
		}
	}
}

func TestReadBothFanRPMAndModes(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)

	rpms, err := ReadBothFanRPM()
	if err != nil {
		t.Fatalf("ReadBothFanRPM() = %v, want nil", err)
	}
	if rpms[0] != 3001 || rpms[1] != 3002 {
		t.Errorf("ReadBothFanRPM() = %v, want [3001 3002]", rpms)
	}

	modes, err := ReadBothFanModes()
	if err != nil {
		t.Fatalf("ReadBothFanModes() = %v, want nil", err)
	}
	if modes[0] != 2 || modes[1] != 2 {
		t.Errorf("ReadBothFanModes() = %v, want [2 2]", modes)
	}
}

func TestFanReadersErrorWhenDeviceMissing(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	if _, err := ReadBothFanRPM(); err == nil {
		t.Error("ReadBothFanRPM() = nil, want an error")
	}
	if _, err := ReadBothFanModes(); err == nil {
		t.Error("ReadBothFanModes() = nil, want an error")
	}
	if _, err := ReadBothFanCurves(); err == nil {
		t.Error("ReadBothFanCurves() = nil, want an error")
	}
}

func TestHighTDPFanCurveRespectsMinimumPWM(t *testing.T) {
	curve := HighTDPFanCurve()
	if len(curve) != fanCurvePoints {
		t.Fatalf("HighTDPFanCurve() has %d points, want %d", len(curve), fanCurvePoints)
	}
	for i, p := range curve {
		if p.PWM < HighTDPMinPWM {
			t.Errorf("point %d PWM = %d, below the %d floor this curve exists to enforce", i+1, p.PWM, HighTDPMinPWM)
		}
		if i > 0 {
			if p.Temp <= curve[i-1].Temp {
				t.Errorf("point %d temp %d is not above point %d temp %d", i+1, p.Temp, i, curve[i-1].Temp)
			}
			if p.PWM < curve[i-1].PWM {
				t.Errorf("point %d PWM %d is below point %d PWM %d", i+1, p.PWM, i, curve[i-1].PWM)
			}
		}
	}
	// The curve must survive its own validator.
	if _, err := ParseFanCurve(formatCurve(curve)); err != nil {
		t.Errorf("HighTDPFanCurve() is rejected by ParseFanCurve: %v", err)
	}
}

func formatCurve(points []api.FanCurvePoint) string {
	out := ""
	for i, p := range points {
		if i > 0 {
			out += ","
		}
		out += itoa(p.Temp) + ":" + itoa(p.PWM)
	}
	return out
}

func TestFindProfilePathPrefersDeviceSupportingQuiet(t *testing.T) {
	f := newFakeSysfs(t)
	// A non-ASUS device (no "quiet") plus the ASUS one; ASUS must win.
	f.withProfileDevice(t, "platform-profile-1", "low-power balanced performance", "balanced")
	asus := f.withProfileDevice(t, "platform-profile-2", "quiet balanced performance", "balanced")

	if got, want := FindProfilePath(), asus+"/profile"; got != want {
		t.Errorf("FindProfilePath() = %q, want the quiet-capable device %q", got, want)
	}
}

func TestFindProfilePathFallsBackToACPI(t *testing.T) {
	newFakeSysfs(t)
	swap(t, &sysProfileDir, t.TempDir()+"/absent")
	if got := FindProfilePath(); got != sysProfileACPI {
		t.Errorf("FindProfilePath() = %q, want the ACPI fallback %q", got, sysProfileACPI)
	}
}

// TestSetProfileMapsQuietPerDevice covers the mapping that exists because
// amd-pmf advertises "low-power" where asus-wmi advertises "quiet".
func TestSetProfileMapsQuietPerDevice(t *testing.T) {
	f := newFakeSysfs(t)
	pmf := f.withProfileDevice(t, "platform-profile-1", "low-power balanced performance", "balanced")
	asus := f.withProfileDevice(t, "platform-profile-2", "quiet balanced performance", "balanced")

	if err := SetProfile("quiet"); err != nil {
		t.Fatalf("SetProfile(quiet) = %v, want nil", err)
	}
	if got := readTrimmed(t, asus+"/profile"); got != "quiet" {
		t.Errorf("asus device profile = %q, want \"quiet\"", got)
	}
	if got := readTrimmed(t, pmf+"/profile"); got != "low-power" {
		t.Errorf("amd-pmf device profile = %q, want \"low-power\" (it does not support \"quiet\")", got)
	}
	if got := *f.ppdCalls; len(got) != 1 || got[0] != "power-saver" {
		t.Errorf("powerprofilesctl calls = %v, want [power-saver]", got)
	}
}

func TestSetProfileWritesUniversalNamesUnmapped(t *testing.T) {
	f := newFakeSysfs(t)
	pmf := f.withProfileDevice(t, "platform-profile-1", "low-power balanced performance", "balanced")
	asus := f.withProfileDevice(t, "platform-profile-2", "quiet balanced performance", "balanced")

	if err := SetProfile("performance"); err != nil {
		t.Fatalf("SetProfile(performance) = %v, want nil", err)
	}
	for _, p := range []string{asus, pmf} {
		if got := readTrimmed(t, p+"/profile"); got != "performance" {
			t.Errorf("%s profile = %q, want \"performance\"", p, got)
		}
	}
}

func TestBootSoundAndPanelOverdriveRoundTrip(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, FindBootSoundPath(), "0")
	f.writeFile(t, FindPanelOverdrivePath(), "0")

	if err := SetBootSound(1); err != nil {
		t.Fatalf("SetBootSound(1) = %v, want nil", err)
	}
	if got := f.readInt(t, FindBootSoundPath()); got != 1 {
		t.Errorf("boot_sound = %d, want 1", got)
	}
	if err := SetPanelOverdrive(1); err != nil {
		t.Fatalf("SetPanelOverdrive(1) = %v, want nil", err)
	}
	if got := f.readInt(t, FindPanelOverdrivePath()); got != 1 {
		t.Errorf("panel_overdrive = %d, want 1", got)
	}
}

func TestBatteryPathsGlobBATStar(t *testing.T) {
	f := newFakeSysfs(t)
	// Only BAT1 exists — the glob must find it rather than defaulting to BAT0.
	if err := os.MkdirAll(f.root+"/power_supply/BAT1", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.RemoveAll(f.battery); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	f.writeFile(t, f.root+"/power_supply/BAT1/charge_control_end_threshold", "80")
	f.writeFile(t, f.root+"/power_supply/BAT1/capacity", "55")

	if got, want := FindBatteryThresholdPath(), f.root+"/power_supply/BAT1/charge_control_end_threshold"; got != want {
		t.Errorf("FindBatteryThresholdPath() = %q, want %q", got, want)
	}
	if got, want := FindBatteryCapacityPath(), f.root+"/power_supply/BAT1/capacity"; got != want {
		t.Errorf("FindBatteryCapacityPath() = %q, want %q", got, want)
	}
}

func TestReadAPUTemperatureConvertsMillidegrees(t *testing.T) {
	f := newFakeSysfs(t)
	f.writeFile(t, f.hwmonTemp+"/temp1_input", "62500")

	got, err := ReadAPUTemperature()
	if err != nil {
		t.Fatalf("ReadAPUTemperature() = %v, want nil", err)
	}
	if got != 62 {
		t.Errorf("ReadAPUTemperature() = %d, want 62 (62500 millidegrees)", got)
	}
}

func TestReadAPUTemperatureErrorsWithoutK10temp(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	if _, err := ReadAPUTemperature(); err == nil {
		t.Error("ReadAPUTemperature() = nil, want an error when k10temp is absent")
	}
}

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", path, err)
	}
	return strings.TrimSpace(string(data))
}

// TestSetProfileWritesPrimaryWhenNoClassDevice is the regression guard for a
// silent no-op: when no device under sysProfileDir exposes a profile file,
// FindProfilePath falls back to the ACPI alias, which the write loop never
// visits. SetProfile then returned nil having written nothing at all — and
// still told power-profiles-daemon the switch had happened.
func TestSetProfileWritesPrimaryWhenNoClassDevice(t *testing.T) {
	f := newFakeSysfs(t)

	// An empty platform-profile directory forces the ACPI-alias fallback.
	acpi := f.root + "/acpi_platform_profile"
	swap(t, &sysProfileDir, f.root+"/empty-platform-profile")
	if err := os.MkdirAll(f.root+"/empty-platform-profile", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f.writeFile(t, acpi, "balanced")

	if err := SetProfile("performance"); err != nil {
		t.Fatalf("SetProfile() = %v, want nil", err)
	}

	data, err := os.ReadFile(acpi)
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", acpi, err)
	}
	if got := strings.TrimSpace(string(data)); got != "performance" {
		t.Errorf("ACPI alias = %q, want %q — SetProfile reported success without writing", got, "performance")
	}
}

// TestSetProfileReportsPrimaryWriteFailure: with nothing writable, the caller
// must see an error rather than a false success.
func TestSetProfileReportsPrimaryWriteFailure(t *testing.T) {
	f := newFakeSysfs(t)

	swap(t, &sysProfileDir, f.root+"/missing-platform-profile")
	swap(t, &sysProfileACPI, f.root+"/missing-dir/platform_profile")

	if err := SetProfile("quiet"); err == nil {
		t.Error("SetProfile() = nil, want an error when no profile file can be written")
	}
	if calls := *f.ppdCalls; len(calls) != 0 {
		t.Errorf("powerprofilesctl was called %v after a failed write, want no calls", calls)
	}
}
