package amd

// k10temp.go — the processor die temperature from k10temp, AMD's CPU thermal
// hwmon driver (Tctl). An Intel machine would read coretemp instead, which is
// why this lives with the AMD drivers rather than the generic ones.

import (
	"fmt"

	"github.com/dahui/voltaire/v2/internal/drivers/internal/sysfsio"
)

// FindAPUTemperaturePath returns the sysfs path for the APU temperature sensor.
// Uses the k10temp hwmon device (AMD Ryzen thermal sensor, label "Tctl").
func FindAPUTemperaturePath() string {
	dir := sysfsio.FindHwmon(sysHwmonDir, "k10temp")
	if dir == "" {
		return ""
	}
	return dir + "/temp1_input"
}

// ReadAPUTemperature reads the current APU die temperature in degrees Celsius.
// The k10temp driver reports millidegrees; this function converts to whole degrees.
func ReadAPUTemperature() (int, error) {
	path := FindAPUTemperaturePath()
	if path == "" {
		return 0, fmt.Errorf("k10temp hwmon device not found")
	}
	milli, err := readIntFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading APU temperature: %w", err)
	}
	return milli / 1000, nil
}
