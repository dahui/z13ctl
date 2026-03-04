package cli

// fan.go — hwmon sysfs path discovery and I/O helpers for ASUS fan curves.
// Discovers hwmon devices by name attribute (not by number, which is unstable).

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
)

// FanIndex returns the hwmon fan index (1 or 2) for the given fan name.
func FanIndex(fan string) (int, error) {
	switch strings.ToLower(fan) {
	case "cpu":
		return 1, nil
	case "gpu":
		return 2, nil
	default:
		return 0, fmt.Errorf("unknown fan %q: must be cpu or gpu", fan)
	}
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

// ReadFanRPM reads the current RPM for the given fan ("cpu" or "gpu").
func ReadFanRPM(fan string) (int, error) {
	idx, err := FanIndex(fan)
	if err != nil {
		return 0, err
	}
	dir := FindFanReadingsHwmonPath()
	if dir == "" {
		return 0, fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
	}
	return readIntFile(dir + "/" + fmt.Sprintf("fan%d_input", idx))
}

// ReadFanMode reads the pwm_enable value for the given fan from the curve
// hwmon device. Returns 0 (full-speed), 1 (custom), or 2 (auto/firmware).
func ReadFanMode(fan string) (int, error) {
	idx, err := FanIndex(fan)
	if err != nil {
		return 0, err
	}
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return 0, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	return readIntFile(dir + "/" + fmt.Sprintf("pwm%d_enable", idx))
}

// ReadFanCurve reads the 8-point fan curve for the given fan.
func ReadFanCurve(fan string) ([]api.FanCurvePoint, error) {
	idx, err := FanIndex(fan)
	if err != nil {
		return nil, err
	}
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return nil, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	points := make([]api.FanCurvePoint, fanCurvePoints)
	for i := range fanCurvePoints {
		temp, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_auto_point%d_temp", idx, i+1))
		if err != nil {
			return nil, fmt.Errorf("reading curve point %d temp: %w", i+1, err)
		}
		pwm, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_auto_point%d_pwm", idx, i+1))
		if err != nil {
			return nil, fmt.Errorf("reading curve point %d pwm: %w", i+1, err)
		}
		points[i] = api.FanCurvePoint{Temp: temp, PWM: pwm}
	}
	return points, nil
}

// SetFanCurve writes an 8-point fan curve and enables custom mode (pwm_enable=1).
func SetFanCurve(fan string, points []api.FanCurvePoint) error {
	idx, err := FanIndex(fan)
	if err != nil {
		return err
	}
	if len(points) != fanCurvePoints {
		return fmt.Errorf("fan curve must have exactly %d points, got %d", fanCurvePoints, len(points))
	}
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	for i, p := range points {
		if err := writeIntFile(dir+"/"+fmt.Sprintf("pwm%d_auto_point%d_temp", idx, i+1), p.Temp); err != nil {
			return fmt.Errorf("writing curve point %d temp: %w", i+1, err)
		}
		if err := writeIntFile(dir+"/"+fmt.Sprintf("pwm%d_auto_point%d_pwm", idx, i+1), p.PWM); err != nil {
			return fmt.Errorf("writing curve point %d pwm: %w", i+1, err)
		}
	}
	return SetFanMode(fan, 1) // enable custom mode
}

// SetFanMode writes pwm_enable for the given fan on both hwmon devices
// (the kernel requires both to agree). mode: 0=full-speed, 1=custom, 2=auto.
func SetFanMode(fan string, mode int) error {
	idx, err := FanIndex(fan)
	if err != nil {
		return err
	}
	file := fmt.Sprintf("pwm%d_enable", idx)

	// Write to the curve hwmon device (primary).
	curveDir := FindFanCurveHwmonPath()
	if curveDir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	if err := writeIntFile(curveDir+"/"+file, mode); err != nil {
		return fmt.Errorf("setting fan mode on %s: %w", hwmonNameCurves, err)
	}

	// Write to the readings hwmon device (secondary). Errors are non-fatal
	// since the curve device is the primary control.
	readDir := FindFanReadingsHwmonPath()
	if readDir != "" {
		_ = writeIntFile(readDir+"/"+file, mode)
	}
	return nil
}

// ResetFanCurve restores firmware auto mode for the given fan.
func ResetFanCurve(fan string) error {
	return SetFanMode(fan, 2) // auto/firmware
}

// ResetAllFanCurves restores firmware auto mode for both CPU and GPU fans.
func ResetAllFanCurves() error {
	if err := ResetFanCurve("cpu"); err != nil {
		return err
	}
	return ResetFanCurve("gpu")
}

// SetAllFansFullSpeed sets pwm_enable=0 for both CPU and GPU fans.
// Used by the TDP >75W safety mechanism.
func SetAllFansFullSpeed() error {
	if err := SetFanMode("cpu", 0); err != nil {
		return err
	}
	return SetFanMode("gpu", 0)
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
