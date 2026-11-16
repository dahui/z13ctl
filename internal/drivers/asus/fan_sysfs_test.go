package asus

// fan_sysfs_test.go — hwmon and platform-profile helpers exercised against the
// fake sysfs tree from sysfs_fake_test.go.

import (
	"os"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
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

func TestSetFanCurvesWritesBothFansAndEnablesCustom(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)
	seedReadingsSentinel(t, f)

	points := make([]api.FanCurvePoint, fakeCurvePoints)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 50 + i*20}
	}
	if err := SetFanCurves(points); err != nil {
		t.Fatalf("SetFanCurves() = %v, want nil", err)
	}

	for _, fan := range fakeFanChannels {
		for i, p := range points {
			gotTemp := f.readInt(t, f.hwmon+"/pwm"+itoa(fan)+"_auto_point"+itoa(i+1)+"_temp")
			gotPWM := f.readInt(t, f.hwmon+"/pwm"+itoa(fan)+"_auto_point"+itoa(i+1)+"_pwm")
			if gotTemp != p.Temp || gotPWM != p.PWM {
				t.Errorf("fan%d point %d = %d:%d, want %d:%d", fan, i+1, gotTemp, gotPWM, p.Temp, p.PWM)
			}
		}
		// Custom mode must be enabled on the curve device...
		if got := f.readInt(t, f.hwmon+"/pwm"+itoa(fan)+"_enable"); got != 1 {
			t.Errorf("curve device fan%d pwm_enable = %d, want 1 (custom)", fan, got)
		}
		// ...and the base "asus" device must be left untouched. Its
		// pwm1_enable_store rejects mode 1 on the Z13 (fan_type SPEC83) and, where
		// it is accepted, clears custom_fan_curves[*].enabled for every fan —
		// undoing the curve that was just enabled. See issue #15.
		if got := f.readInt(t, f.hwmonRead+"/pwm"+itoa(fan)+"_enable"); got != readingsSentinel {
			t.Errorf("readings device fan%d pwm_enable = %d, want %d (untouched)", fan, got, readingsSentinel)
		}
	}
}

// readingsSentinel is written to the base "asus" device's pwm_enable files so a
// test can tell "left alone" from "written with the same value".
const readingsSentinel = 9

// seedReadingsSentinel marks both readings-device pwm_enable files.
func seedReadingsSentinel(t *testing.T, f *fakeSysfs) {
	t.Helper()
	for _, fan := range fakeFanChannels {
		f.writeFile(t, f.hwmonRead+"/pwm"+itoa(fan)+"_enable", itoa(readingsSentinel))
	}
}

func TestSetFanCurvesRejectsWrongPointCount(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)
	err := SetFanCurves([]api.FanCurvePoint{{Temp: 30, PWM: 100}})
	if err == nil || !strings.Contains(err.Error(), "exactly 8 points") {
		t.Fatalf("SetFanCurves() with 1 point = %v, want the kernel's count named", err)
	}
}

func TestSetFanCurvesErrorsWhenHwmonMissing(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	points := make([]api.FanCurvePoint, fakeCurvePoints)
	if err := SetFanCurves(points); err == nil {
		t.Error("SetFanCurves() = nil, want an error when the hwmon device is absent")
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
	for _, fan := range fakeFanChannels {
		if got := f.readInt(t, f.hwmon+"/pwm"+itoa(fan)+"_enable"); got != 2 {
			t.Errorf("curve device fan%d pwm_enable = %d, want 2 (auto)", fan, got)
		}
		if got := f.readInt(t, f.hwmonRead+"/pwm"+itoa(fan)+"_enable"); got != readingsSentinel {
			t.Errorf("readings device fan%d pwm_enable = %d, want %d (untouched)", fan, got, readingsSentinel)
		}
	}
}

// TestResetAllFanCurvesFailsWhenReleaseDoesNotStick is the mirror of
// TestSetFanCurvesFailsWhenCurveDoesNotStick. A release the driver silently
// ignores used to be indistinguishable from success, which on the sleep path is
// the difference between a quiet suspend and a machine that runs its fans all
// night — firmware auto is what lets the EC stop them through s2idle.
func TestResetAllFanCurvesFailsWhenReleaseDoesNotStick(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)

	// Custom mode, then a kernel that accepts the write and changes nothing.
	if err := setAllFanModes(1); err != nil {
		t.Fatalf("setAllFanModes(1) = %v", err)
	}
	orig := fanWriteInt
	fanWriteInt = func(string, int) error { return nil }
	t.Cleanup(func() { fanWriteInt = orig })

	err := ResetAllFanCurves()
	if err == nil {
		t.Fatal("ResetAllFanCurves() = nil, want an error when the kernel does not honour the release")
	}
	if !strings.Contains(err.Error(), "pwm1_enable") {
		t.Errorf("error %q does not name the attribute that proved it", err)
	}
}

// TestVerifyFanModeReleased covers the readback's tolerances. Mode 0 counts as
// released because forced full speed is not a curve, and an unreadable channel is
// deliberately not a failure — the same stance VerifyFanCurveActive takes, so
// that a SKU without pwm2_enable on the curve device keeps working.
func TestVerifyFanModeReleased(t *testing.T) {
	cases := []struct {
		name    string
		modes   []int
		wantErr bool
	}{
		{name: "both auto", modes: []int{2, 2}},
		{name: "forced full speed is not a curve", modes: []int{0, 0}},
		{name: "unreadable is not a failure", modes: []int{-1, -1}},
		{name: "still custom on fan1", modes: []int{1, 2}, wantErr: true},
		{name: "still custom on fan2", modes: []int{2, 1}, wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeSysfs(t)
			f.seedFanCurveFiles(t, 40, 100)
			for i, m := range tt.modes {
				if m == -1 {
					if err := os.Remove(f.hwmon + "/pwm" + itoa(fakeFanChannels[i]) + "_enable"); err != nil {
						t.Fatalf("removing pwm%d_enable: %v", fakeFanChannels[i], err)
					}
					continue
				}
				f.writeFile(t, f.hwmon+"/pwm"+itoa(fakeFanChannels[i])+"_enable", itoa(m)+"\n")
			}
			err := verifyFanModeReleased()
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyFanModeReleased() = %v, wantErr %v", err, tt.wantErr)
			}
		})
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

// TestSetFanCurvesFailsWhenCurveDoesNotStick is the regression test for
// issue #15. The kernel accepts the pwm_enable write and then leaves the mode at
// auto — which is what a concurrent platform_profile write produces — and the
// caller must find out rather than being told the curve was applied.
func TestSetFanCurvesFailsWhenCurveDoesNotStick(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100) // seeds pwm_enable = 2 (auto)

	orig := fanWriteInt
	fanWriteInt = func(string, int) error { return nil } // accepted, no effect
	t.Cleanup(func() { fanWriteInt = orig })

	points := make([]api.FanCurvePoint, fakeCurvePoints)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 50 + i*20}
	}
	err := SetFanCurves(points)
	if err == nil {
		t.Fatal("SetFanCurves() = nil, want an error when the kernel does not honour the curve")
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
		modes   []string // "" means do not create the file
		wantErr bool
	}{
		{"both custom", []string{"1", "1"}, false},
		{"fan1 dropped to auto", []string{"2", "1"}, true},
		{"fan2 dropped to auto", []string{"1", "2"}, true},
		{"full speed is not custom", []string{"0", "1"}, true},
		// Unverifiable is not the same as failed: a SKU exposing only the CPU
		// curve must keep working, floor and all.
		{"fan2 missing", []string{"1", ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSysfs(t)
			for i, fan := range fakeFanChannels {
				if tc.modes[i] == "" {
					continue
				}
				f.writeFile(t, f.hwmon+"/pwm"+itoa(fan)+"_enable", tc.modes[i])
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

func TestReadFanCurvesRoundTrip(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)

	points := make([]api.FanCurvePoint, fakeCurvePoints)
	for i := range points {
		points[i] = api.FanCurvePoint{Temp: 30 + i*5, PWM: 60 + i*10}
	}
	if err := SetFanCurves(points); err != nil {
		t.Fatalf("SetFanCurves() = %v", err)
	}
	curves, err := ReadFanCurves()
	if err != nil {
		t.Fatalf("ReadFanCurves() = %v, want nil", err)
	}
	for fi := range curves {
		for i, p := range curves[fi] {
			if p != points[i] {
				t.Errorf("fan%d point %d = %+v, want %+v", fi+1, i+1, p, points[i])
			}
		}
	}
}

func TestReadFanRPMsAndModes(t *testing.T) {
	f := newFakeSysfs(t)
	f.seedFanCurveFiles(t, 40, 100)

	rpms, err := ReadFanRPMs()
	if err != nil {
		t.Fatalf("ReadFanRPMs() = %v, want nil", err)
	}
	if rpms[0] != 3001 || rpms[1] != 3002 {
		t.Errorf("ReadFanRPMs() = %v, want [3001 3002]", rpms)
	}

	modes, err := ReadFanCurveModes()
	if err != nil {
		t.Fatalf("ReadFanCurveModes() = %v, want nil", err)
	}
	if modes[0] != 2 || modes[1] != 2 {
		t.Errorf("ReadFanCurveModes() = %v, want [2 2]", modes)
	}
}

func TestFanReadersErrorWhenDeviceMissing(t *testing.T) {
	swap(t, &sysHwmonDir, t.TempDir())
	if _, err := ReadFanRPMs(); err == nil {
		t.Error("ReadFanRPMs() = nil, want an error")
	}
	if _, err := ReadFanCurveModes(); err == nil {
		t.Error("ReadFanCurveModes() = nil, want an error")
	}
	if _, err := ReadFanCurves(); err == nil {
		t.Error("ReadFanCurves() = nil, want an error")
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
