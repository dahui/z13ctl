package asus

// export.go — small exported windows onto internals that dry-run output needs:
// describing exactly what this driver would write is driver knowledge, but the
// presentation lives in internal/cli. Nothing here performs I/O.

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
