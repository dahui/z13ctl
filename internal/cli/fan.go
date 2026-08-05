package cli

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

	"github.com/dahui/z13ctl/api"
)

const (
	// hwmon device names exposed by the asus-wmi kernel driver.
	hwmonNameReadings = "asus"                  // fan RPM + pwm_enable
	hwmonNameCurves   = "asus_custom_fan_curve" // 8-point curves + pwm_enable

	fanCurvePoints = 8
	fanCount       = 2 // fan 1 (pwm1) and fan 2 (pwm2)

	// HighTDPMinPWM is the minimum PWM value (50% of 255) enforced when
	// sustained TDP exceeds TDPMaxSafe. Users can set fans higher but not lower.
	//
	// This was 204 (80%) up to v1.2.1, which users found unreasonably loud to
	// live with — loud enough that the realistic alternative was not running the
	// high TDP at all. The protection that matters is the *ramp*, not the floor:
	// a machine sustaining more than TDPMaxSafe watts sits well above 60°C, where
	// HighTDPFanCurve is far above this minimum anyway. The floor only sets how
	// the fans behave while the APU is still cool.
	HighTDPMinPWM = 127
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
			fanNames[i].index, m, FanModeName(m))
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

// ResetAllFanCurves restores firmware auto mode for both fans.
func ResetAllFanCurves() error {
	return setAllFanModes(2) // auto/firmware
}

// SetAllFansFullSpeed forces both fans to maximum speed.
// Only the base "asus" hwmon device supports pwm_enable=0, and only pwm1_enable
// is functional — pwm2_enable returns EIO on writes. Writing pwm1_enable=0
// is sufficient to force both physical fans to full speed.
//
// Nothing calls this. High-TDP cooling uses HighTDPFanCurve (a 50% PWM floor
// with pwm_enable=1) via ApplyTDPSafely; full speed was an earlier approach that
// the docs, the --dry-run output, and CLAUDE.md all went on describing long
// after the code stopped doing it. Kept because it is a real, tested hardware
// capability — but it is not the high-TDP path, and callers should not assume so.
func SetAllFansFullSpeed() error {
	readDir := FindFanReadingsHwmonPath()
	if readDir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
	}
	return writeIntFile(readDir+"/pwm1_enable", 0)
}

// HighTDPFanCurve returns an 8-point fan curve with a minimum PWM of 50%,
// suitable for sustained TDP above 75W. Users can replace this with a custom
// curve as long as all PWM values stay at or above HighTDPMinPWM.
//
// The floor holds only while the APU is cool; from 60°C the curve climbs hard
// and reaches 100% at 80°C, which is the range a machine actually sustaining
// more than TDPMaxSafe watts lives in. Keeping the top of the ramp is what
// makes the lower floor safe.
func HighTDPFanCurve() []api.FanCurvePoint {
	return []api.FanCurvePoint{
		{Temp: 30, PWM: HighTDPMinPWM},
		{Temp: 40, PWM: HighTDPMinPWM},
		{Temp: 50, PWM: 140},
		{Temp: 60, PWM: 165},
		{Temp: 65, PWM: 190},
		{Temp: 70, PWM: 215},
		{Temp: 75, PWM: 235},
		{Temp: 80, PWM: 255},
	}
}

// ParseFanCurve parses a fan curve string "temp:pwm,temp:pwm,..." into a
// slice of FanCurvePoint. Requires exactly 8 points. Temps must be
// monotonically increasing (0–120°C). PWM values must be monotonically
// non-decreasing (0–255). PWM values may use a % suffix for percentage
// (0–100%), which is converted to PWM (e.g. 80% = 204). Both formats
// can be mixed in the same curve string.
func ParseFanCurve(s string) ([]api.FanCurvePoint, error) {
	parts := strings.Split(s, ",")
	if len(parts) != fanCurvePoints {
		return nil, fmt.Errorf("fan curve must have exactly %d points, got %d", fanCurvePoints, len(parts))
	}
	points := make([]api.FanCurvePoint, fanCurvePoints)
	for i, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid curve point %q: expected temp:pwm or temp:pct%%", part)
		}
		temp, err := strconv.Atoi(strings.TrimSpace(kv[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid temp in point %d: %w", i+1, err)
		}
		pwmStr := strings.TrimSpace(kv[1])
		isPercent := strings.HasSuffix(pwmStr, "%")
		if isPercent {
			pwmStr = strings.TrimSuffix(pwmStr, "%")
		}
		pwm, err := strconv.Atoi(pwmStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pwm in point %d: %w", i+1, err)
		}
		if temp < 0 || temp > 120 {
			return nil, fmt.Errorf("temp %d in point %d out of range 0–120", temp, i+1)
		}
		if isPercent {
			if pwm < 0 || pwm > 100 {
				return nil, fmt.Errorf("percentage %d in point %d out of range 0–100", pwm, i+1)
			}
			pwm = pwm * 255 / 100
		} else if pwm < 0 || pwm > 255 {
			return nil, fmt.Errorf("pwm %d in point %d out of range 0–255", pwm, i+1)
		}
		if i > 0 && temp <= points[i-1].Temp {
			return nil, fmt.Errorf("temps must be monotonically increasing: point %d (%d) <= point %d (%d)", i+1, temp, i, points[i-1].Temp)
		}
		if i > 0 && pwm < points[i-1].PWM {
			return nil, fmt.Errorf("pwm values must be non-decreasing: point %d (%d) < point %d (%d)", i+1, pwm, i, points[i-1].PWM)
		}
		points[i] = api.FanCurvePoint{Temp: temp, PWM: pwm}
	}
	return points, nil
}

// FanModeName returns a human-readable name for a pwm_enable value.
func FanModeName(mode int) string {
	switch mode {
	case 0:
		return "full-speed"
	case 1:
		return "custom"
	case 2:
		return "auto"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
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
