// Package register wires the asusz13 driver into the device registry. It is
// imported for its side effect alone:
//
//	import _ "github.com/dahui/z13ctl/internal/drivers/asusz13/register"
//
// The wiring lives here rather than in the driver package so asusz13 does not
// import internal/device — internal/device's own tests import asusz13 for the
// data drift guard, and a direct registration import would be a cycle. It also
// keeps registration explicit: a binary that wants the driver says so.
package register

import (
	"github.com/dahui/z13ctl/internal/device"
	"github.com/dahui/z13ctl/internal/driver"
	"github.com/dahui/z13ctl/internal/drivers/asusz13"
	"github.com/dahui/z13ctl/internal/drivers/aurahid"
	"github.com/dahui/z13ctl/internal/drivers/evdevkey"
)

func init() {
	device.RegisterFans("asus-nb-wmi", func(c device.FansConfig) (driver.FanController, error) {
		return asusz13.NewFanController(c.Shape()), nil
	})
	device.RegisterPower("asus-nb-wmi-ppt", func(c device.PowerConfig) (driver.PowerLimiter, error) {
		return asusz13.NewPowerLimiter(c.Envelope()), nil
	})
	device.RegisterProfiles("platform-profile", func(c device.ProfilesConfig) (driver.ProfileController, error) {
		return asusz13.NewProfileController(c.Names), nil
	})
	device.RegisterToggles("asus-armoury", func(c device.TogglesConfig) (driver.Toggles, error) {
		specs := make([]driver.ToggleSpec, len(c.Entries))
		for i, e := range c.Entries {
			specs[i] = driver.ToggleSpec{ID: e.ID, Label: e.Label, Kind: driver.ToggleBool}
		}
		return asusz13.NewToggles(specs)
	})
	device.RegisterBattery("power-supply", func(device.BatteryConfig) (driver.Battery, error) {
		return asusz13.NewBattery(), nil
	})
	device.RegisterUndervolt("ryzen-smu-co", func(c device.UndervoltConfig) (driver.Undervolter, error) {
		return asusz13.NewUndervolter(c.Min, c.Max), nil
	})
	device.RegisterTelemetry("hwmon-rapl", func(device.TelemetryConfig) (driver.Telemetry, error) {
		return asusz13.NewTelemetry(), nil
	})
	// Lighting and buttons live in their own driver packages — the Aura HID
	// protocol and a watched evdev key are not Z13 sysfs concerns — but the Z13
	// is what registers them, since a registration is a per-binary statement of
	// which hardware this build drives.
	device.RegisterLighting("aura-hid", func(c device.LightingConfig) (driver.Lighting, error) {
		return aurahid.New(c.Zones), nil
	})
	device.RegisterButtons("evdev-key", func(c device.ButtonConfig) (driver.Buttons, error) {
		return evdevkey.New(c.Device, c.Keycode, c.Kind), nil
	})
}
