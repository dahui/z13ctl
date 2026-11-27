package amd

// undervolter.go — the ryzen-smu-co Undervolter: the Curve Optimizer offset,
// bounded by device data, through the SMU helpers in smu.go and undervolt.go.

import (
	"fmt"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// NewUndervolter returns the ryzen_smu Curve Optimizer driver with bounds from
// device data, named by text where device data gives wording (empty fields
// take coText's). All the destructive-probe caveats on SMUProbeUndervolt apply.
func NewUndervolter(lo, hi int, text driver.UndervoltText) driver.Undervolter {
	return undervolter{lo: lo, hi: hi, text: text}
}

type undervolter struct {
	lo, hi int
	text   driver.UndervoltText
}

// coText is this driver's wording for the offset. Curve Optimizer counts are
// steps of the CPU's voltage-frequency curve, not millivolts, so there is no
// unit to show.
var coText = driver.UndervoltText{
	Label:       "CPU Curve Optimizer",
	ShortLabel:  "CO",
	Description: "All-core Curve Optimizer offset: negative values lower the CPU's voltage curve. Unstable values may cause crashes.",
}

// Text is device data's wording with this driver's filling the gaps.
func (u undervolter) Text() driver.UndervoltText {
	t := u.text
	if t.Label == "" {
		t.Label = coText.Label
	}
	if t.ShortLabel == "" {
		t.ShortLabel = coText.ShortLabel
	}
	if t.Unit == "" {
		t.Unit = coText.Unit
	}
	if t.Description == "" {
		t.Description = coText.Description
	}
	return t
}

// Present is the stat-only check: the ryzen_smu sysfs interface exists. It
// says nothing about whether CO commands work on this platform — that is
// ProbeAvailable's (destructive) question.
func (undervolter) Present() bool         { return SMUAvailable() }
func (undervolter) ProbeAvailable() bool  { return smuProbeUndervolt() }
func (undervolter) Available() bool       { return SMUUndervoltAvailable() }
func (u undervolter) Range() (lo, hi int) { return u.lo, u.hi }

// Apply validates against the device data's bounds before anything else: even
// the availability probe inside SetCurveOptimizer is a hardware write, and a
// rejected value must not reach it.
func (u undervolter) Apply(cpuCO int) error {
	if cpuCO < u.lo || cpuCO > u.hi {
		return fmt.Errorf("CPU undervolt %d out of range %d to %d", cpuCO, u.lo, u.hi)
	}
	return setCurveOptimizer(cpuCO)
}

func (undervolter) Reset() error { return resetCurveOptimizer() }

// Command names this CPU's Curve Optimizer message for the dry run.
func (undervolter) Command() (string, error) { return CurveOptimizerCommand() }
