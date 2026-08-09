package asusz13

// fan.go — hwmon sysfs path discovery and I/O helpers for ASUS fan curves.
// Discovers hwmon devices by name attribute (not by number, which is unstable).
//
// The 2025 ROG Flow Z13 has an APU with two physical fans but no discrete GPU.
// Both fans cool the same chip, so the same curve is always applied to both.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

const (
	// hwmon device names exposed by the asus-wmi kernel driver.
	hwmonNameReadings = "asus"                  // fan RPM + pwm_enable
	hwmonNameCurves   = "asus_custom_fan_curve" // 8-point curves + pwm_enable

	fanCurvePoints = 8
	fanCount       = 2 // fan 1 (pwm1) and fan 2 (pwm2)
)

// fanWriteInt is the pwm_enable write used by setFanMode. It is a var purely so
// tests can simulate the kernel's real failure mode here: accepting the write
// and then leaving the mode unchanged, which is what a concurrent
// platform_profile write produces. Plain files cannot reproduce that.
var fanWriteInt = writeIntFile

// fanNames maps internal fan names to their hwmon index (1 or 2).
var fanNames = [fanCount]struct {
	name  string
	index int
}{
	{"fan1", 1},
	{"fan2", 2},
}

// FindFanHwmonPath returns the sysfs hwmon directory whose name attribute
// matches the given value. Returns "" if not found. hwmon numbers are
// unstable across reboots, so discovery by name is required.
func FindFanHwmonPath(name string) string {
	entries, err := os.ReadDir(sysHwmonDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		p := sysHwmonDir + "/" + e.Name() + "/name"
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == name {
			return sysHwmonDir + "/" + e.Name()
		}
	}
	return ""
}

// FindFanReadingsHwmonPath returns the hwmon dir for fan RPM and mode readings.
func FindFanReadingsHwmonPath() string {
	return FindFanHwmonPath(hwmonNameReadings)
}

// FindFanCurveHwmonPath returns the hwmon dir for custom fan curve points.
func FindFanCurveHwmonPath() string {
	return FindFanHwmonPath(hwmonNameCurves)
}

// ReadBothFanRPM reads the current RPM for both fans.
// Returns [2]int with fan1 and fan2 RPM values.
func ReadBothFanRPM() ([fanCount]int, error) {
	dir := FindFanReadingsHwmonPath()
	if dir == "" {
		return [fanCount]int{}, fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
	}
	var rpms [fanCount]int
	for i, f := range fanNames {
		v, err := readIntFile(dir + "/" + fmt.Sprintf("fan%d_input", f.index))
		if err != nil {
			return rpms, fmt.Errorf("reading fan%d RPM: %w", f.index, err)
		}
		rpms[i] = v
	}
	return rpms, nil
}

// ReadFanCurveModes returns the pwm_enable value for each fan on the curve
// hwmon device, using -1 for a channel that cannot be read. Unlike
// ReadBothFanModes it does not fail the whole read because one channel is
// missing: a SKU that exposes only the CPU curve is a supported configuration,
// and the reconcile watcher must still be able to act on the channel it has.
// An error is returned only when the hwmon device itself is absent.
func ReadFanCurveModes() ([fanCount]int, error) {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return [fanCount]int{}, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	var modes [fanCount]int
	for i, f := range fanNames {
		v, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_enable", f.index))
		if err != nil {
			modes[i] = -1
			continue
		}
		modes[i] = v
	}
	return modes, nil
}

// VerifyFanCurveActive reports whether the kernel is actually honouring the
// custom curve on every readable fan channel.
//
// fan_curve_enable_show() returns the driver's cached data->enabled flag, so
// this file is ground truth: anything other than 1 means the curve was dropped.
// The usual cause is a platform_profile write — throttle_thermal_policy_write()
// ends by clearing custom_fan_curves[*].enabled for every fan, and
// fan_curve_write() then returns early on !enabled — which is what made a
// curve set by z13ctl silently stop working minutes later (issue #15).
//
// A channel that cannot be read is deliberately not a failure: unverifiable is
// not the same as failed, and hard-failing there would make fan control (and
// with it the high-TDP floor) unavailable on any SKU that does not expose
// pwm2_enable on the curve device.
func VerifyFanCurveActive() error {
	modes, err := ReadFanCurveModes()
	if err != nil {
		return err
	}
	for i, m := range modes {
		if m == -1 || m == 1 {
			continue
		}
		return fmt.Errorf(
			"custom fan curve was written but the kernel is not honouring it (pwm%d_enable = %d, %s): "+
				"a platform_profile change disables custom fan curves in the kernel driver; re-apply the curve",
			fanNames[i].index, m, driver.FanModeName(m))
	}
	return nil
}

// ReadBothFanModes reads the pwm_enable value for both fans from the curve
// hwmon device. Returns 0 (full-speed), 1 (custom), or 2 (auto/firmware).
func ReadBothFanModes() ([fanCount]int, error) {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return [fanCount]int{}, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	var modes [fanCount]int
	for i, f := range fanNames {
		v, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_enable", f.index))
		if err != nil {
			return modes, fmt.Errorf("reading fan%d mode: %w", f.index, err)
		}
		modes[i] = v
	}
	return modes, nil
}

// ReadBothFanCurves reads the 8-point fan curve for both fans.
func ReadBothFanCurves() ([fanCount][]api.FanCurvePoint, error) {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return [fanCount][]api.FanCurvePoint{}, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	var curves [fanCount][]api.FanCurvePoint
	for fi, f := range fanNames {
		points := make([]api.FanCurvePoint, fanCurvePoints)
		for i := range fanCurvePoints {
			temp, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_auto_point%d_temp", f.index, i+1))
			if err != nil {
				return curves, fmt.Errorf("reading fan%d curve point %d temp: %w", f.index, i+1, err)
			}
			pwm, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_auto_point%d_pwm", f.index, i+1))
			if err != nil {
				return curves, fmt.Errorf("reading fan%d curve point %d pwm: %w", f.index, i+1, err)
			}
			points[i] = api.FanCurvePoint{Temp: temp, PWM: pwm}
		}
		curves[fi] = points
	}
	return curves, nil
}

// SetBothFanCurves writes the same 8-point fan curve to both fans, enables
// custom mode (pwm_enable=1) on both, and verifies that the kernel kept it.
//
// The readback is not paranoia: the driver drops custom curves on any
// platform_profile write without reporting anything to the process that set
// them, so without it every caller reports success for a curve that is no
// longer in effect — including ApplyTDPSafely, whose thermal floor depends on
// this write having stuck.
func SetBothFanCurves(points []api.FanCurvePoint) error {
	if len(points) != fanCurvePoints {
		return fmt.Errorf("fan curve must have exactly %d points, got %d", fanCurvePoints, len(points))
	}
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	for _, f := range fanNames {
		for i, p := range points {
			if err := writeIntFile(dir+"/"+fmt.Sprintf("pwm%d_auto_point%d_temp", f.index, i+1), p.Temp); err != nil {
				return fmt.Errorf("writing fan%d curve point %d temp: %w", f.index, i+1, err)
			}
			if err := writeIntFile(dir+"/"+fmt.Sprintf("pwm%d_auto_point%d_pwm", f.index, i+1), p.PWM); err != nil {
				return fmt.Errorf("writing fan%d curve point %d pwm: %w", f.index, i+1, err)
			}
		}
	}
	if err := setAllFanModes(1); err != nil { // enable custom mode on both
		return err
	}
	return VerifyFanCurveActive()
}

// setFanMode writes pwm_enable for a single fan (by index).
//
// Mode 0 (full-speed) is only supported by the base "asus" hwmon device; the
// "asus_custom_fan_curve" device rejects it with EINVAL.
//
// Modes 1 (custom) and 2 (auto) go to the curve device *only*. The base device
// must not be written for them: on the Z13 its fan_type is SPEC83, whose
// pwm1_enable_store accepts just 0 (full-speed) and 2 (auto) and answers mode 1
// with EINVAL — and, worse, it clears custom_fan_curves[*].enabled for every fan
// before returning. On any kernel or SKU that accepts the write, syncing the
// mode there would disable the very curve this function has just enabled.
// z13ctl did exactly that through v1.2.1; it was inert only because the Z13
// rejects it (issue #15).
func setFanMode(idx, mode int) error {
	file := fmt.Sprintf("pwm%d_enable", idx)

	if mode == 0 {
		// Full-speed: only the base "asus" hwmon device supports pwm_enable=0.
		readDir := FindFanReadingsHwmonPath()
		if readDir == "" {
			return fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
		}
		return fanWriteInt(readDir+"/"+file, mode)
	}

	curveDir := FindFanCurveHwmonPath()
	if curveDir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	if err := fanWriteInt(curveDir+"/"+file, mode); err != nil {
		return fmt.Errorf("setting fan mode on %s: %w", hwmonNameCurves, err)
	}
	return nil
}

// setAllFanModes writes pwm_enable for both fans.
func setAllFanModes(mode int) error {
	for _, f := range fanNames {
		if err := setFanMode(f.index, mode); err != nil {
			return err
		}
	}
	return nil
}

// ResetAllFanCurves restores firmware auto mode for both fans, and verifies that
// the kernel kept it.
//
// The readback matters for the same reason SetBothFanCurves has one, in the
// mirror image: a release the driver silently ignores is indistinguishable from
// success, and the sleep hook (internal/daemon/resume.go) needs the difference.
// Firmware auto is what lets the EC stop the fans through s2idle, so a release
// that did not take is the difference between a quiet suspend and a machine that
// runs its fans all night.
func ResetAllFanCurves() error {
	if err := setAllFanModes(2); err != nil { // auto/firmware
		return err
	}
	return verifyFanModeReleased()
}

// verifyFanModeReleased reports whether both fans are on firmware auto.
//
// As in VerifyFanCurveActive, a channel that cannot be read is deliberately not
// a failure — unverifiable is not the same as failed, and hard-failing there
// would break fan control on any SKU that does not expose pwm2_enable on the
// curve device. Mode 0 is also accepted: forced full speed is not a curve, so
// the release has nothing left to undo.
func verifyFanModeReleased() error {
	modes, err := ReadFanCurveModes()
	if err != nil {
		return err
	}
	for i, m := range modes {
		if m == -1 || m == 0 || m == 2 {
			continue
		}
		return fmt.Errorf(
			"fans were released to firmware auto but the kernel is not honouring it (pwm%d_enable = %d, %s)",
			fanNames[i].index, m, driver.FanModeName(m))
	}
	return nil
}

// SetAllFansFullSpeed forces both fans to maximum speed.
// Only the base "asus" hwmon device supports pwm_enable=0, and only pwm1_enable
// is functional — pwm2_enable returns EIO on writes. Writing pwm1_enable=0
// is sufficient to force both physical fans to full speed.
//
// Nothing calls this. High-TDP cooling uses the envelope's floor curve (a 50%
// PWM floor with pwm_enable=1) via the safety engine's ApplyTDPSafely; full
// speed was an earlier approach that the docs, the --dry-run output, and
// CLAUDE.md all went on describing long after the code stopped doing it. Kept
// because it is a real, tested hardware capability — but it is not the
// high-TDP path, and callers should not assume so.
func SetAllFansFullSpeed() error {
	readDir := FindFanReadingsHwmonPath()
	if readDir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
	}
	return writeIntFile(readDir+"/pwm1_enable", 0)
}

// readIntFile reads a sysfs file and parses its content as an integer.
func readIntFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// writeIntFile writes an integer value to a sysfs file.
func writeIntFile(path string, value int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(value)+"\n"), 0o644)
}
