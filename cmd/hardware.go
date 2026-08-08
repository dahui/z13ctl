package cmd

// hardware.go — how CLI commands reach the assembled device. Validation limits
// (the power envelope, undervolt bounds) come from it on every path; hardware
// I/O goes through it only on the no-daemon fallbacks and the --get reads that
// deliberately bypass daemon state.
//
// The fallback policy mirrors the daemon's assembleDevice: an unrecognized
// machine is assumed to be the Z13, because that is exactly what every earlier
// z13ctl did on non-Z13 hardware — the Z13 drivers all fail soft on absent
// sysfs. The multi-device milestone replaces this with a conservative generic
// device. A failed assembly, by contrast, is a build defect (a device file
// naming a factory this binary does not register) and errors the command.

import (
	"fmt"
	"os"
	"sync"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/device"
)

// cliFallbackDeviceID is the device assumed when no device file matches this
// machine. Kept equal to the daemon's fallbackDeviceID; both go away with the
// generic device.
const cliFallbackDeviceID = "asus-rog-flow-z13-2025"

var (
	hwOnce sync.Once
	hwDev  *device.Device
	hwErr  error
)

// hardware returns this machine's assembled device, assembling it on first
// use. Assembly is cheap and pure — DMI is read, but no driver touches
// hardware until a method runs — so commands may call this before deciding
// whether the daemon will handle the operation.
func hardware() (*device.Device, error) {
	hwOnce.Do(func() {
		c, err := device.MatchConfig()
		if err != nil {
			// Once per process: the note explains the op-by-op sysfs errors a
			// genuinely foreign machine sees next, and is invisible on a Z13.
			fmt.Fprintln(os.Stderr, "note: no device data matches this machine; assuming the ASUS ROG Flow Z13")
			configs, cfgErr := device.Configs()
			if cfgErr != nil {
				hwErr = cfgErr
				return
			}
			found := false
			for _, cand := range configs {
				if cand.Device.ID == cliFallbackDeviceID {
					c, found = cand, true
					break
				}
			}
			if !found {
				hwErr = fmt.Errorf("fallback device %q is not in the embedded device data", cliFallbackDeviceID)
				return
			}
		}
		// Lighting and buttons still live inside internal/daemon (the CLI's
		// lighting commands drive HID directly), so their factories are not
		// registered yet; strip them or assembly fails on the gap.
		c.Lighting = nil
		c.Button = nil
		hwDev, hwErr = device.Assemble(c)
	})
	return hwDev, hwErr
}

// liveFanCurve returns the fan curve currently in force, or nil when there is
// none — no fan control, fans not in custom mode, or an unreadable curve. The
// mode gate matters: the curve registers survive a release on the Z13, so
// LiveCurve alone reports a curve that is not running.
func liveFanCurve(hw *device.Device) []api.FanCurvePoint {
	if hw.Fans == nil {
		return nil
	}
	if mode, err := hw.Fans.ReadMode(); err != nil || mode != 1 {
		return nil
	}
	c, err := hw.Fans.LiveCurve()
	if err != nil {
		return nil
	}
	return c
}
