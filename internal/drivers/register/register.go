// Package register wires the compiled-in drivers into the device registry:
// each device-file method name (`asus-nb-wmi`, `platform-profile`, …) to the
// constructor that builds it. It is imported for its side effect alone:
//
//	import _ "github.com/dahui/voltaire/v2/internal/drivers/register"
//
// The wiring lives here rather than in the driver packages for two reasons.
// Driver packages do not import internal/device — some of their tests import
// it to assemble a real device's envelope, and a registration import would be
// a cycle. And this is the one place that may join drivers from different
// packages: where one method's driver needs a reading another package owns
// (a charger only the vendor firmware can name, a policy attribute only the
// vendor driver notifies), the factory here passes it in, so no driver package
// imports another. It also keeps registration explicit: a binary that wants
// the drivers says so.
package register

import (
	"errors"

	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/amd"
	"github.com/dahui/voltaire/v2/internal/drivers/asus"
	"github.com/dahui/voltaire/v2/internal/drivers/aurahid"
	"github.com/dahui/voltaire/v2/internal/drivers/evdevkey"
	"github.com/dahui/voltaire/v2/internal/drivers/hidrawbtn"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
	"github.com/dahui/voltaire/v2/internal/drivers/linux"
)

func init() {
	device.RegisterFans("asus-nb-wmi", func(c device.FansConfig) (driver.FanController, error) {
		return asus.NewFanController(c.Shape()), nil
	})
	device.RegisterPower("asus-nb-wmi-ppt", func(c device.PowerConfig) (driver.PowerLimiter, error) {
		return asus.NewPowerLimiter(c.Envelope()), nil
	})
	// The generic AMD power method: limits straight to the SMU, for machines
	// with no vendor interface (the OXP). Its one stock row names the firmware
	// profile it runs under (Config.Validate requires exactly one).
	device.RegisterPower("amd-smu", func(c device.PowerConfig) (driver.PowerLimiter, error) {
		profile := ""
		for name := range c.StockPPT {
			profile = name
		}
		return amd.NewSMUPower(c.Envelope(), c.EnvelopeBattery(), profile, linux.OnACPower), nil
	})
	device.RegisterProfiles("platform-profile", func(c device.ProfilesConfig) (driver.ProfileController, error) {
		// asus-wmi notifies its own policy attribute on every profile write
		// and every fan release; the generic driver watches it when told to.
		var extra []string
		if c.Handler == "asus-wmi" {
			extra = append(extra, asus.ThrottlePolicyPath())
		}
		return linux.NewProfileController(c.Names, c.Handler, c.Default, c.Labels, extra...), nil
	})
	device.RegisterToggles("asus-armoury", func(c device.TogglesConfig) (driver.Toggles, error) {
		var specs []driver.ToggleSpec
		var hidden []string
		for _, e := range c.Entries {
			if e.Hidden {
				hidden = append(hidden, e.ID)
				continue
			}
			specs = append(specs, driver.ToggleSpec{
				ID: e.ID, Label: e.Label, Description: e.Description, Kind: driver.ToggleBool,
			})
		}
		return asus.NewToggles(specs, hidden)
	})
	device.RegisterBattery("power-supply", func(c device.BatteryConfig) (driver.Battery, error) {
		// charger = "asus-armoury" is the one firmware source; anything else
		// reads the kind from power_supply (Config.Validate refuses others).
		var charger linux.ChargerFunc
		if c.Charger == "asus-armoury" {
			charger = asus.ChargerFromArmoury
		}
		return linux.NewBattery(c.Caps(), charger), nil
	})
	device.RegisterUndervolt("ryzen-smu-co", func(c device.UndervoltConfig) (driver.Undervolter, error) {
		return amd.NewUndervolter(c.Min, c.Max, c.Text()), nil
	})
	// Not an ASUS interface at all — cpufreq's boost switch is generic Linux,
	// and any future device driver can register the same method name against
	// its own config. It lives here because this is the only binary that drives
	// hardware today; when a second device wants it, the factory moves to a
	// shared package rather than being copied.
	device.RegisterCPUBoost("cpufreq", func(device.CPUConfig) (driver.CPUBoost, error) {
		return linux.NewCPUBoost(), nil
	})
	device.RegisterTelemetry("hwmon-rapl", func(c device.TelemetryConfig) (driver.Telemetry, error) {
		return linux.NewTelemetry(c.Info(), linux.TelemetrySources{
			Temp: amd.ReadAPUTemperature,
			GPU:  amd.GPUSource{},
			NPU:  amd.NPUSource{},
		}), nil
	})
	// Lighting and buttons live in their own driver packages — the Aura HID
	// protocol and a watched evdev key are not Z13 sysfs concerns — but the Z13
	// is what registers them, since a registration is a per-binary statement of
	// which hardware this build drives.
	device.RegisterLighting("aura-hid", func(c device.LightingConfig) (driver.Lighting, error) {
		cfg := aurahid.Config{Modes: c.Modes, Speeds: c.Speeds}
		for _, z := range c.Zones {
			v, p, err := z.USBIDs()
			if err != nil {
				return nil, err
			}
			cfg.Zones = append(cfg.Zones, aurahid.Zone{Name: z.Name, Label: z.Label, Vendor: v, Product: p, Byte: uint8(z.Zone)})
		}
		return aurahid.New(cfg)
	})
	device.RegisterButtons("evdev-key", func(c device.ButtonConfig) (driver.Buttons, error) {
		return withUSBNode(evdevkey.New(c.Device, c.Keycode, c.ID, c.KeyboardNode != ""), c)
	})
	device.RegisterButtons("evdev-chord", func(c device.ButtonConfig) (driver.Buttons, error) {
		return withUSBNode(evdevkey.NewChord(c.Device, c.Keys, c.ID, c.KeyboardNode != ""), c)
	})
	device.RegisterInputPlumberButtons(func() driver.Buttons { return inputplumber.NewButtons() })
	device.RegisterButtons("hidraw-report", func(c device.ButtonConfig) (driver.Buttons, error) {
		v, p, err := c.USBIDs()
		if err != nil || c.Interface == nil {
			return nil, errors.New("hidraw-report needs usb and interface")
		}
		match := make([]hidrawbtn.ByteMatch, 0, len(c.Match))
		for _, m := range c.Match {
			if len(m) != 2 {
				return nil, errors.New("hidraw-report match entries are [offset, value]")
			}
			match = append(match, hidrawbtn.ByteMatch{Offset: m[0], Value: byte(m[1])})
		}
		return hidrawbtn.New(hidrawbtn.Spec{ID: c.ID, Vendor: v, Product: p, Interface: *c.Interface,
			ReportID: c.ReportID, Match: match}), nil
	})
}

// withUSBNode narrows an evdev button to the USB interface its device data
// names, when it names one.
func withUSBNode(b *evdevkey.Buttons, c device.ButtonConfig) (driver.Buttons, error) {
	if c.USB == "" {
		return b, nil
	}
	v, p, err := c.USBIDs()
	if err != nil || c.Interface == nil {
		return nil, errors.New("an evdev button's usb needs interface too")
	}
	return b.WithUSB(evdevkey.USBNode{Vendor: v, Product: p, Interface: *c.Interface}), nil
}
