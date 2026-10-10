package asusz13

// export.go — small exported windows onto internals that dry-run output needs:
// describing exactly what this driver would write is driver knowledge, but the
// presentation lives in internal/cli. Nothing here performs I/O.

// ProfileNameForDevice returns the profile string actually written to one
// platform-profile device — some devices name "quiet" differently (e.g.
// "low-power"), and dry-run output must show the mapped name SetProfile
// writes, not the ASUS-side name it was asked for.
func ProfileNameForDevice(deviceDir, asusProfile string) string {
	return profileNameForDevice(deviceDir, asusProfile)
}

// SysProfileDir returns the platform-profile class directory currently in use
// (a var internally, so the fake sysfs tree can redirect it).
func SysProfileDir() string { return sysProfileDir }

// FanPWMIndices returns the hwmon pwm channels of the curve device, in write
// order — what a dry run needs to spell out per-fan attribute paths. Empty
// when the curve device is absent.
func FanPWMIndices() []int {
	dir := FindFanCurveHwmonPath()
	if dir == "" {
		return nil
	}
	return curveModeChannels(dir)
}

// FanHwmonNames returns the hwmon device names the fan driver finds its curve
// and its readings by, for the grants voltaire setup writes.
func FanHwmonNames() (curves, readings string) { return hwmonNameCurves, hwmonNameReadings }

// EncodeCOValue returns the SMU argument a Curve Optimizer offset encodes to,
// so a dry run can print the exact payload SetCurveOptimizer would send.
func EncodeCOValue(offset int) uint32 { return encodeCOValue(offset) }
