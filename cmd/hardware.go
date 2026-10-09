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
	"strconv"
	"strings"
	"sync"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
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
		hwDev, hwErr = device.Assemble(c)
	})
	return hwDev, hwErr
}

// envOf returns hw's power envelope, or a zero envelope when the device has no
// power limit control. For dry-run display, which wants the numbers a real run
// would use but must not fail where the real path merely prints zeros.
func envOf(hw *device.Device) driver.PowerEnvelope {
	if hw == nil || hw.Power == nil {
		return driver.PowerEnvelope{}
	}
	return hw.Power.Envelope()
}

// powerEnvFor returns the power envelope to validate and describe a CLI
// request against. With a daemon running it is the daemon's (device-get): on
// asus-armoury a read of the kernel's bounds is a live ACPI call, and the daemon
// serves a cached envelope rather than make one while the EC is not answering —
// the CLI reading the bounds itself would walk into the stalled EC the daemon is
// avoiding. With no daemon there is no latch to honour, and the driver is asked.
func powerEnvFor(hw *device.Device) driver.PowerEnvelope {
	if handled, info, err := api.SendDeviceGet(); handled && err == nil && info != nil && info.Power != nil {
		return envFromInfo(info.Power)
	}
	return envOf(hw)
}

// envFromInfo is the envelope a device-get document describes.
func envFromInfo(p *api.PowerInfo) driver.PowerEnvelope {
	env := driver.PowerEnvelope{
		TDPMin:          p.TDPMin,
		TDPMaxSafe:      p.TDPMaxSafe,
		TDPMaxForced:    p.TDPMaxForced,
		Interface:       p.Interface,
		FloorCurve:      append([]api.FanCurvePoint(nil), p.FloorCurve...),
		StockProfilePPT: p.StockProfilePPT,
	}
	if p.PL2 != nil {
		env.PL2 = driver.PowerRange{Min: p.PL2.Min, Max: p.PL2.Max}
	}
	if p.PL3 != nil {
		env.PL3 = driver.PowerRange{Min: p.PL3.Min, Max: p.PL3.Max}
	}
	return env
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

// hwmonPWMMax is the hwmon pwm ABI's ceiling — the interface's range, not a
// device property — and is used only when a device declares no fan shape.
const hwmonPWMMax = 255

// fanPWMMax is the device's PWM ceiling, for turning a PWM into a percentage
// in output. It comes from the fan shape the device data declares.
func fanPWMMax(hw *device.Device) int {
	if hw != nil && hw.Fans != nil {
		if m := hw.Fans.Shape().PWMMax; m > 0 {
			return m
		}
	}
	return hwmonPWMMax
}

// pwmPercent is pwm as a rounded percentage of pwmMax — rounded, as the GUI
// rounds, so a floor of 127 reads as the 50% it was chosen to be.
func pwmPercent(pwm, pwmMax int) int {
	return (pwm*100 + pwmMax/2) / pwmMax
}

// liveReadings are the values status and fancurve --get show that, on the
// device, come from the embedded controller: fan RPM over asus-wmi, battery
// and AC through ACPI (_BST, _PSR). Temperature rides along so a report is
// sourced consistently.
type liveReadings struct {
	TempC     int // 0 = not available
	RPM       []int
	OnAC      bool
	ACKnown   bool
	Capacity  int // percent; 0 = not available
	Limit     int // charge limit percent; 0 = not available
	ViaDaemon bool
}

// readLive takes the EC-backed readings from the daemon when one is running,
// and reads hardware only when none is.
//
// The daemon gates every EC read on its wedged-EC latch: a read into an
// embedded controller that has stopped answering after a resume blocks in
// ACPI holding the global lock, and the machine hard-locks (PR #26 — the
// trace's lock holder was a *reader*). A CLI that read sysfs itself while a
// daemon was up would walk around that gate, and status --watch would do it
// every second. So with a daemon running, whatever get-state omits is shown
// as unavailable rather than read here.
//
// up says whether a daemon answered at all; st is its get-state reply, nil
// when it answered with an error. A daemon that is up is guarding the EC
// whether or not this reply was usable, so hardware is read only when !up.
func readLive(hw *device.Device, st *api.State, up bool) liveReadings {
	if up && st == nil {
		return liveReadings{ViaDaemon: true}
	}
	if st != nil {
		r := liveReadings{
			TempC:     st.Temperature,
			RPM:       st.RPM,
			OnAC:      st.OnAC,
			ACKnown:   st.SourceKnown,
			Capacity:  st.BatteryLevel,
			Limit:     st.Battery,
			ViaDaemon: true,
		}
		// A daemon older than the RPM slice reports fan 1 alone.
		if len(r.RPM) == 0 && st.FanRPM > 0 {
			r.RPM = []int{st.FanRPM}
		}
		return r
	}

	var r liveReadings
	if hw.Telemetry != nil {
		if s, err := hw.Telemetry.Sample(); err == nil {
			r.TempC = s.TempC
		}
	}
	if hw.Fans != nil {
		if rpms, err := hw.Fans.ReadRPM(); err == nil {
			r.RPM = rpms
		}
	}
	if hw.Battery != nil {
		if bs, err := hw.Battery.Status(); err == nil {
			r.OnAC, r.ACKnown, r.Capacity = bs.OnAC, bs.ACKnown, bs.Capacity
		}
		if limit, err := hw.Battery.ChargeLimit(); err == nil {
			r.Limit = limit
		}
	}
	return r
}

// daemonState asks the running daemon for get-state. up is false only when no
// daemon answered; st is nil when one did but the reply was an error.
func daemonState() (st *api.State, up bool) {
	handled, st, err := api.SendGetState()
	if !handled {
		return nil, false
	}
	if err != nil {
		return nil, true
	}
	return st, true
}

// formatRPM shows every fan the device reports, in its order.
func formatRPM(rpms []int) string {
	if len(rpms) == 0 {
		return "N/A"
	}
	parts := make([]string, len(rpms))
	for i, v := range rpms {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, " / ") + " RPM"
}

// firmwareProfiles is the firmware profiles the device offers, nil when it has
// no profile control. Reading it lists the handler's choices — a cached
// kernel value, never an EC call.
func firmwareProfiles(hw *device.Device) []string {
	if hw == nil || hw.Profiles == nil {
		return nil
	}
	return hw.Profiles.Names()
}

// firmwareProfilesOrNone is firmwareProfiles for output: the local device's
// list, or a single "none" when there is no device or no profile control.
func firmwareProfilesOrNone() []string {
	hw, err := hardware()
	if names := firmwareProfiles(hw); err == nil && len(names) > 0 {
		return names
	}
	return []string{"none"}
}

// landingProfileName names the firmware profile a reset lands on, for output
// after the daemon performed it: the device's default, read from the same
// device data the daemon uses.
func landingProfileName() string {
	if hw, err := hardware(); err == nil {
		if d := defaultProfile(hw); d != "" {
			return d
		}
	}
	return "default"
}

// defaultProfile is the device's reset landing profile, "" with no profile
// control or no default.
func defaultProfile(hw *device.Device) string {
	if hw == nil || hw.Profiles == nil {
		return ""
	}
	return hw.Profiles.Default()
}
