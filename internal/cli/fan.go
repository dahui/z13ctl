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
	hwmonDir = "/sys/class/hwmon"

	// hwmon device names exposed by the asus-wmi kernel driver.
	hwmonNameReadings = "asus"                  // fan RPM + pwm_enable
	hwmonNameCurves   = "asus_custom_fan_curve" // 8-point curves + pwm_enable

	fanCurvePoints = 8
	fanCount       = 2 // fan 1 (pwm1) and fan 2 (pwm2)
)

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
	entries, err := os.ReadDir(hwmonDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		p := hwmonDir + "/" + e.Name() + "/name"
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == name {
			return hwmonDir + "/" + e.Name()
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

// SetBothFanCurves writes the same 8-point fan curve to both fans and enables
// custom mode (pwm_enable=1) on both.
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
	return setAllFanModes(1) // enable custom mode on both
}

// setFanMode writes pwm_enable for a single fan (by index).
// Mode 0 (full-speed) is only supported by the base "asus" hwmon device;
// the "asus_custom_fan_curve" device rejects it with EINVAL.
// Modes 1 (custom) and 2 (auto) are written to the curve device first,
// then synced to the readings device.
func setFanMode(idx, mode int) error {
	file := fmt.Sprintf("pwm%d_enable", idx)

	if mode == 0 {
		// Full-speed: only the base "asus" hwmon device supports pwm_enable=0.
		readDir := FindFanReadingsHwmonPath()
		if readDir == "" {
			return fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
		}
		return writeIntFile(readDir+"/"+file, mode)
	}

	// Custom (1) or auto (2): write to curve device first, then sync readings.
	curveDir := FindFanCurveHwmonPath()
	if curveDir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	if err := writeIntFile(curveDir+"/"+file, mode); err != nil {
		return fmt.Errorf("setting fan mode on %s: %w", hwmonNameCurves, err)
	}
	readDir := FindFanReadingsHwmonPath()
	if readDir != "" {
		_ = writeIntFile(readDir+"/"+file, mode)
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
func SetAllFansFullSpeed() error {
	readDir := FindFanReadingsHwmonPath()
	if readDir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
	}
	return writeIntFile(readDir+"/pwm1_enable", 0)
}

// ParseFanCurve parses a fan curve string "temp:pwm,temp:pwm,..." into a
// slice of FanCurvePoint. Requires exactly 8 points. Temps must be
// monotonically increasing (0–120°C). PWM values must be monotonically
// non-decreasing (0–255).
func ParseFanCurve(s string) ([]api.FanCurvePoint, error) {
	parts := strings.Split(s, ",")
	if len(parts) != fanCurvePoints {
		return nil, fmt.Errorf("fan curve must have exactly %d points, got %d", fanCurvePoints, len(parts))
	}
	points := make([]api.FanCurvePoint, fanCurvePoints)
	for i, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid curve point %q: expected temp:pwm", part)
		}
		temp, err := strconv.Atoi(strings.TrimSpace(kv[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid temp in point %d: %w", i+1, err)
		}
		pwm, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid pwm in point %d: %w", i+1, err)
		}
		if temp < 0 || temp > 120 {
			return nil, fmt.Errorf("temp %d in point %d out of range 0–120", temp, i+1)
		}
		if pwm < 0 || pwm > 255 {
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
