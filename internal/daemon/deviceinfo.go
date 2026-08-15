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
		// Copied for the same reason as the floor curve: the wire document must
		// not alias the envelope's own map, or a client that mutated what it was
		// given would be editing the device definition every later caller reads.
		if len(env.StockProfilePPT) > 0 {
			ppt := make(map[string]api.TDPState, len(env.StockProfilePPT))
			for name, t := range env.StockProfilePPT {
				ppt[name] = t
			}
			info.Power.StockProfilePPT = ppt
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
			// A driver written before plugins existed leaves Source empty;
			// substituting here rather than in every driver keeps the wire
			// contract "always populated" true no matter which driver answered.
			source := t.Source
			if source == "" {
				source = api.ToggleSourceCore
			}
			info.Toggles = append(info.Toggles, api.ToggleInfo{
				ID: t.ID, Label: t.Label, Description: t.Description,
				Kind: string(t.Kind), Source: source,
			})
		}
	}
	if hw.CPUBoost != nil {
		info.CPU = &api.CPUInfo{Boost: true}
	}
	if hw.Undervolt != nil {
		lo, hi := hw.Undervolt.Range()
		info.Undervolt = &api.UndervoltInfo{Min: lo, Max: hi}
	}
	if hw.Battery != nil {
		caps := hw.Battery.Caps()
		info.Battery = &api.BatteryInfo{ChargeLimit: caps.ChargeLimit, Health: caps.Health}
	}
	if hw.Telemetry != nil {
		t := hw.Telemetry.Info()
		info.Telemetry = &api.TelemetryInfo{
			PowerDraw:      t.PowerDraw,
			GPU:            t.GPU,
			CPUStats:       t.CPUStats,
			NPU:            t.NPU,
			Net:            t.Net,
			HistorySeconds: t.HistorySeconds,
		}
	}
	info.Buttons = hw.Buttons != nil
	return info
}

// readFeatures reads the current value of every firmware toggle the device
// declares, keyed by id. It is what lets a client render toggle rows from the
// device document without a socket round trip per toggle per refresh.
//
// A toggle whose value cannot be read is left out of the map rather than
// recorded as zero, because zero is "off" — a claim about the hardware, and the
// wrong one to make about an attribute that could not be read at all. Callers
// that want the old always-a-number behaviour index the map and take Go's zero
// value, which is exactly what the named BootSound/PanelOverdrive fields do.
//
// Returns nil for a device with no toggles, which omitempty drops from the wire.
func (d *Daemon) readFeatures() map[string]int {
	if d.hw == nil || d.hw.Toggles == nil {
		return nil
	}
	specs := d.hw.Toggles.List()
	if len(specs) == 0 {
		return nil
	}
	out := make(map[string]int, len(specs))
	for _, t := range specs {
		if v, err := d.hw.Toggles.Get(t.ID); err == nil {
			out[t.ID] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// readPendingReboot reports whether a firmware setting is waiting on a restart,
// or nil when this device cannot say.
//
// Nil covers three cases that a client must treat alike: no toggles at all, a
// Toggles driver that does not implement driver.RebootPending, and a read that
// failed. None of them is evidence that nothing is pending.
func (d *Daemon) readPendingReboot() *bool {
	if d.hw == nil || d.hw.Toggles == nil {
		return nil
	}
	rp, ok := d.hw.Toggles.(driver.RebootPending)
	if !ok {
		return nil
	}
	pending, err := rp.PendingReboot()
	if err != nil {
		return nil
	}
	return &pending
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
	d.notifyToggleChanged(spec.ID, value)
	return response{OK: true}
}

// notifyToggleChanged records a firmware toggle's new value and tells every
// subscriber the state moved.
//
// The notify is the load-bearing half. Toggle values reach clients through
// get-state — State.Features, plus the two named fields older clients read —
// so a client that re-reads sees truth; what it had no way to learn was that
// there was anything to re-read. Without this, a toggle changed by any other
// client leaves every open UI showing the old position until something else
// happens to refresh it, which is exactly the stale-value failure saveAndNotify
// exists to prevent. It became visible when the full window grew a settings
// page rendering these rows from State.Features: handlePanelOverdrive notified
// and its two siblings did not, so the same switch updated live or did not
// depending on which command wrote it.
//
// The state write is a projection, not persistence: these are BIOS settings the
// firmware owns, and readFeatures re-reads them from hardware on every
// get-state. It is kept because the two named fields are the vocabulary pre-2.0
// clients know, and leaving them behind after a write makes the saved state
// disagree with the machine for anyone reading the file directly.
func (d *Daemon) notifyToggleChanged(id string, value int) {
	d.mu.Lock()
	switch id {
	case "boot_sound":
		d.state.BootSound = value
	case "panel_overdrive":
		d.state.PanelOverdrive = value
	}
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
}
