package asusz13

// fan.go — hwmon sysfs path discovery and I/O helpers for ASUS fan curves.
// Discovers hwmon devices by name attribute (not by number, which is unstable).
//
// The channels, the points per curve and the fans that report a speed are all
// enumerated from hwmon's attribute names, never assumed: the driver writes one
// curve to every curve channel (the FanController contract), so a device whose
// fans need different curves needs a different interface, not a flag here.

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

const (
	// hwmon device names exposed by the asus-wmi kernel driver.
	hwmonNameReadings = "asus"                  // fan RPM + pwm_enable
	hwmonNameCurves   = "asus_custom_fan_curve" // curve points + pwm_enable
)

// fanWriteInt is the pwm_enable write used by setFanMode. It is a var purely so
// tests can simulate the kernel's real failure mode here: accepting the write
// and then leaving the mode unchanged, which is what a concurrent
// platform_profile write produces. Plain files cannot reproduce that.
var fanWriteInt = writeIntFile

// The attribute names the channels are enumerated from. hwmon's own ABI, so
// the shape of the fan hardware is the kernel's answer rather than a constant:
// how many fans report a speed, how many have a curve, and how many points
// each curve holds.
var (
	fanInputRe   = regexp.MustCompile(`^fan(\d+)_input$`)
	curvePointRe = regexp.MustCompile(`^pwm(\d+)_auto_point(\d+)_temp$`)
	pwmEnableRe  = regexp.MustCompile(`^pwm(\d+)_enable$`)
)

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

// channelsMatching returns the sorted, distinct first submatch of re over the
// entries of dir — the hwmon channel numbers carrying that attribute. Listing
// a directory reads no attribute, so this never reaches the EC.
func channelsMatching(dir string, re *regexp.Regexp) []int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// fanInputChannels returns the channels of the fans that report a speed.
func fanInputChannels(dir string) []int { return channelsMatching(dir, fanInputRe) }

// curveModeChannels returns every channel on the curve device with a mode or
// a curve point — what a mode read or write walks. A channel with an enable
// file and no points still has a mode worth verifying.
func curveModeChannels(dir string) []int {
	out := channelsMatching(dir, pwmEnableRe)
	for _, n := range channelsMatching(dir, curvePointRe) {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// curveShape returns the channels that carry a curve and the number of points
// each holds. Every channel must hold the same contiguous 1..n: the driver
// writes one curve to all of them (the FanController contract), so channels
// that disagree are an error rather than a guess at which one to believe.
func curveShape(dir string) (chans []int, points int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	perChan := map[int][]int{}
	for _, e := range entries {
		m := curvePointRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		ch, err1 := strconv.Atoi(m[1])
		pt, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil {
			perChan[ch] = append(perChan[ch], pt)
		}
	}
	if len(perChan) == 0 {
		return nil, 0, fmt.Errorf("hwmon device %q exposes no curve points", hwmonNameCurves)
	}
	for ch, pts := range perChan {
		slices.Sort(pts)
		for i, p := range pts {
			if p != i+1 {
				return nil, 0, fmt.Errorf("fan curve channel %d has a gap at point %d", ch, i+1)
			}
		}
		if points == 0 {
			points = len(pts)
		} else if len(pts) != points {
			return nil, 0, fmt.Errorf("fan curve channels disagree on the point count (%d and %d)", points, len(pts))
		}
		chans = append(chans, ch)
	}
	slices.Sort(chans)
	return chans, points, nil
}

// FanCurveShape returns the kernel's curve channels and points per curve, or
// an error when the curve device is absent or malformed.
func FanCurveShape() (chans []int, points int, err error) {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return nil, 0, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	return curveShape(dir)
}

// ReadFanRPMs reads the current RPM of every fan that reports one, in channel
// order. A fan the kernel does not list is simply not there; one it lists but
// cannot read fails the whole read, since that is an EC that is not answering
// rather than a missing fan.
func ReadFanRPMs() ([]int, error) {
	dir := FindFanReadingsHwmonPath()
	if dir == "" {
		return nil, fmt.Errorf("hwmon device %q not found", hwmonNameReadings)
	}
	chans := fanInputChannels(dir)
	if len(chans) == 0 {
		return nil, fmt.Errorf("hwmon device %q reports no fan speeds", hwmonNameReadings)
	}
	rpms := make([]int, 0, len(chans))
	for _, ch := range chans {
		v, err := readIntFile(dir + "/" + fmt.Sprintf("fan%d_input", ch))
		if err != nil {
			return nil, fmt.Errorf("reading fan%d RPM: %w", ch, err)
		}
		rpms = append(rpms, v)
	}
	return rpms, nil
}

// FanKernelLabels returns the kernel's fan*_label for every fan that reports a
// speed, in ReadFanRPMs order, "" where a fan has none. A label is a static
// string the driver formats, not an EC read.
func FanKernelLabels() []string {
	dir := FindFanReadingsHwmonPath()
	if dir == "" {
		return nil
	}
	chans := fanInputChannels(dir)
	out := make([]string, len(chans))
	for i, ch := range chans {
		if data, err := os.ReadFile(dir + "/" + fmt.Sprintf("fan%d_label", ch)); err == nil {
			out[i] = strings.TrimSpace(string(data))
		}
	}
	return out
}

// fanCurveMode pairs a curve-device channel with its pwm_enable value.
type fanCurveMode struct{ channel, mode int }

// ReadFanCurveModes returns the pwm_enable value for each channel on the curve
// hwmon device, using -1 for a channel whose mode cannot be read. A missing
// channel is not a failure: a SKU that exposes only the CPU curve is a
// supported configuration, and the reconcile watcher must still be able to
// act on the channel it has. An error is returned only when the hwmon device
// itself is absent.
func ReadFanCurveModes() ([]int, error) {
	modes, err := readFanCurveModes()
	if err != nil {
		return nil, err
	}
	out := make([]int, len(modes))
	for i, m := range modes {
		out[i] = m.mode
	}
	return out, nil
}

func readFanCurveModes() ([]fanCurveMode, error) {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return nil, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	var out []fanCurveMode
	for _, ch := range curveModeChannels(dir) {
		v, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_enable", ch))
		if err != nil {
			v = -1
		}
		out = append(out, fanCurveMode{ch, v})
	}
	return out, nil
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
// every channel's pwm_enable on the curve device.
func VerifyFanCurveActive() error {
	modes, err := readFanCurveModes()
	if err != nil {
		return err
	}
	for _, m := range modes {
		if m.mode == -1 || m.mode == 1 {
			continue
		}
		return fmt.Errorf(
			"custom fan curve was written but the kernel is not honouring it (pwm%d_enable = %d, %s): "+
				"a platform_profile change disables custom fan curves in the kernel driver; re-apply the curve",
			m.channel, m.mode, driver.FanModeName(m.mode))
	}
	return nil
}

// ReadFanCurves reads the curve programmed on every curve channel.
func ReadFanCurves() ([][]api.FanCurvePoint, error) {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return nil, fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	chans, n, err := curveShape(dir)
	if err != nil {
		return nil, err
	}
	curves := make([][]api.FanCurvePoint, 0, len(chans))
	for _, ch := range chans {
		points := make([]api.FanCurvePoint, n)
		for i := range n {
			temp, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_auto_point%d_temp", ch, i+1))
			if err != nil {
				return nil, fmt.Errorf("reading fan%d curve point %d temp: %w", ch, i+1, err)
			}
			pwm, err := readIntFile(dir + "/" + fmt.Sprintf("pwm%d_auto_point%d_pwm", ch, i+1))
			if err != nil {
				return nil, fmt.Errorf("reading fan%d curve point %d pwm: %w", ch, i+1, err)
			}
			points[i] = api.FanCurvePoint{Temp: temp, PWM: pwm}
		}
		curves = append(curves, points)
	}
	return curves, nil
}

// SetFanCurves writes the same curve to every curve channel, enables custom
// mode (pwm_enable=1) on each, and verifies that the kernel kept it. The curve
// must hold exactly as many points as the kernel's curves do.
//
// The readback is not paranoia: the driver drops custom curves on any
// platform_profile write without reporting anything to the process that set
// them, so without it every caller reports success for a curve that is no
// longer in effect — including ApplyTDPSafely, whose thermal floor depends on
// this write having stuck.
func SetFanCurves(points []api.FanCurvePoint) error {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	chans, n, err := curveShape(dir)
	if err != nil {
		return err
	}
	if len(points) != n {
		return fmt.Errorf("fan curve must have exactly %d points, got %d", n, len(points))
	}
	for _, ch := range chans {
		for i, p := range points {
			if err := writeIntFile(dir+"/"+fmt.Sprintf("pwm%d_auto_point%d_temp", ch, i+1), p.Temp); err != nil {
				return fmt.Errorf("writing fan%d curve point %d temp: %w", ch, i+1, err)
			}
			if err := writeIntFile(dir+"/"+fmt.Sprintf("pwm%d_auto_point%d_pwm", ch, i+1), p.PWM); err != nil {
				return fmt.Errorf("writing fan%d curve point %d pwm: %w", ch, i+1, err)
			}
		}
	}
	for _, ch := range chans { // enable custom mode on every curve channel
		if err := setFanMode(ch, 1); err != nil {
			return err
		}
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

// setAllFanModes writes pwm_enable for every channel on the curve device.
func setAllFanModes(mode int) error {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return fmt.Errorf("hwmon device %q not found", hwmonNameCurves)
	}
	chans := curveModeChannels(dir)
	if len(chans) == 0 {
		return fmt.Errorf("hwmon device %q exposes no fan channels", hwmonNameCurves)
	}
	for _, ch := range chans {
		if err := setFanMode(ch, mode); err != nil {
			return err
		}
	}
	return nil
}

// ResetAllFanCurves restores firmware auto mode for every fan, and verifies that
// the kernel kept it.
//
// The readback matters for the same reason SetFanCurves has one, in the
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

// verifyFanModeReleased reports whether every fan is on firmware auto.
//
// As in VerifyFanCurveActive, a channel that cannot be read is deliberately not
// a failure — unverifiable is not the same as failed, and hard-failing there
// would break fan control on any SKU that does not expose every channel's
// pwm_enable on the curve device. Mode 0 is also accepted: forced full speed is
// not a curve, so the release has nothing left to undo.
func verifyFanModeReleased() error {
	modes, err := readFanCurveModes()
	if err != nil {
		return err
	}
	for _, m := range modes {
		if m.mode == -1 || m.mode == 0 || m.mode == 2 {
			continue
		}
		return fmt.Errorf(
			"fans were released to firmware auto but the kernel is not honouring it (pwm%d_enable = %d, %s)",
			m.channel, m.mode, driver.FanModeName(m.mode))
	}
	return nil
}

// SetAllFansFullSpeed writes pwm1_enable=0 on the base "asus" hwmon device —
// the only device that accepts mode 0; pwm2_enable there returns EIO on
// writes. Despite the name it does **not** force every fan: measured on the
// GZ302EA (2026-10-09), fan 1 went to 8900 RPM within three seconds while fan 2
// stayed at 0 for the full 30 s. A curve of 255 at every point with
// pwm_enable=1 on the curve device is what drives both to full speed.
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
