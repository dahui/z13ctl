package daemon

// deviceinfo.go — the device-get capability document, and the generic
// feature/feature-get commands over the device's firmware toggles.
//
// deviceInfoFor is a pure projection of the assembled device's constructor
// data — shapes, envelopes, zone/toggle/name lists — so it touches no hardware
// and its answer is static for the daemon's lifetime. That is also what makes
// it fully testable against the real Z13 assembly: nothing here can write the
// developer's machine.

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
)

func (d *Daemon) handleDeviceGet() response {
	return response{OK: true, Device: deviceInfoFor(d.hw)}
}

// deviceInfoFor builds the capability/limits document clients render their
// controls from. Capability discovery is by absence: a nil driver produces an
// omitted section, never an error. The floor curve is copied so the wire
// document cannot alias the envelope's own slice.
func deviceInfoFor(hw *device.Device) *api.DeviceInfo {
	if hw == nil {
		return &api.DeviceInfo{}
	}
	info := &api.DeviceInfo{ID: hw.ID, Model: hw.Model}
	if hw.Fans != nil {
		s := hw.Fans.Shape()
		info.Fans = &api.FanInfo{Points: s.Points, TempMin: s.TempMin, TempMax: s.TempMax, PWMMax: s.PWMMax}
	}
	if hw.Power != nil {
		env := hw.Power.Envelope()
		info.Power = &api.PowerInfo{
			TDPMin:       env.TDPMin,
			TDPMaxSafe:   env.TDPMaxSafe,
			TDPMaxForced: env.TDPMaxForced,
			FloorCurve:   append([]api.FanCurvePoint(nil), env.FloorCurve...),
		}
	}
	if hw.Profiles != nil {
		info.Profiles = &api.ProfileInfo{Names: hw.Profiles.Names()}
	}
	if hw.Lighting != nil {
		info.Lighting = &api.LightingInfo{Zones: hw.Lighting.Zones()}
	}
	if hw.Toggles != nil {
		for _, t := range hw.Toggles.List() {
			info.Toggles = append(info.Toggles, api.ToggleInfo{ID: t.ID, Label: t.Label, Kind: string(t.Kind)})
		}
	}
	if hw.Undervolt != nil {
		lo, hi := hw.Undervolt.Range()
		info.Undervolt = &api.UndervoltInfo{Min: lo, Max: hi}
	}
	info.Battery = hw.Battery != nil
	info.Telemetry = hw.Telemetry != nil
	info.Buttons = hw.Buttons != nil
	return info
}

// featureSpec resolves a request's toggle ID against the device's declared
// set, returning the spec or the rejection response. Resolving against List
// rather than passing the ID straight to Get/Set is what turns the driver's
// generic ErrUnsupported into a message that names the unknown feature.
func (d *Daemon) featureSpec(id string) (driver.ToggleSpec, response) {
	if d.hw == nil || d.hw.Toggles == nil {
		return driver.ToggleSpec{}, response{OK: false, Error: "feature: no firmware toggles on this device"}
	}
	if id == "" {
		return driver.ToggleSpec{}, response{OK: false, Error: "feature: missing id"}
	}
	for _, t := range d.hw.Toggles.List() {
		if t.ID == id {
			return t, response{}
		}
	}
	return driver.ToggleSpec{}, response{OK: false, Error: fmt.Sprintf("feature: unknown feature %q on this device", id)}
}

// handleFeatureGet reads a firmware toggle by wire ID. Like every *-get it
// reads hardware directly — ground truth over cache, since another process may
// have changed the setting.
func (d *Daemon) handleFeatureGet(req request) response {
	spec, rej := d.featureSpec(req.ID)
	if spec.ID == "" {
		return rej
	}
	v, err := d.hw.Toggles.Get(spec.ID)
	if err != nil {
		return response{OK: false, Error: "feature " + spec.ID + ": " + err.Error()}
	}
	return response{OK: true, Value: strconv.Itoa(v)}
}

// handleFeature sets a firmware toggle by wire ID, validating the value
// against the toggle's declared kind. Firmware toggles live in the BIOS, not
// daemon state, so like the bootsound/paneloverdrive commands this neither
// persists nor broadcasts anything.
func (d *Daemon) handleFeature(req request) response {
	spec, rej := d.featureSpec(req.ID)
	if spec.ID == "" {
		return rej
	}
	value, err := strconv.Atoi(req.Set)
	if err != nil {
		return response{OK: false, Error: fmt.Sprintf("feature %s: value %q must be an integer", spec.ID, req.Set)}
	}
	if spec.Kind == driver.ToggleBool && value != 0 && value != 1 {
		return response{OK: false, Error: fmt.Sprintf("feature %s: value must be 0 or 1", spec.ID)}
	}
	if err := d.hw.Toggles.Set(spec.ID, value); err != nil {
		return response{OK: false, Error: "feature " + spec.ID + ": " + err.Error()}
	}
	slog.Info("feature", "id", spec.ID, "set", value)
	return response{OK: true}
}
