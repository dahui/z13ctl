// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// deviceundervolt.go — the one-slider device's remembered Curve Optimizer
// offset (State.DeviceUndervolt), the undervolt counterpart of the remembered
// TDP (Jeff, 2026-11-17).
//
// On a slider device an undervolt kept only in custom profiles would undo
// itself: moving the TDP slider leaves the running profile for the pinned
// firmware profile, and every route to a firmware profile clears the offset.
// So, as with the TDP, an undervolt command naming no profile is a device
// setting: applied, remembered, and in force whenever no running custom
// profile carries an offset of its own. A profile that does carry one wins
// while it runs, and leaving it puts the device setting back.
//
// Every SMU rule of the profile path holds here. The probe runs only where an
// offset is about to be written; a reset still goes through uvApplied, whose
// Active walk covers this offset too (setUndervoltActive/undervoltActive); and
// Active is set only after a successful write. The offset does not survive a
// reboot or a suspend, so both mark it lost first (deviceUndervoltLost) and
// the restore writes it again.

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/dahui/voltaire/api/v2"
)

// deviceUndervoltModel reports whether this device keeps its undervolt as a
// device setting: the one-slider power model, with a Curve Optimizer.
func (d *Daemon) deviceUndervoltModel() bool {
	return d.hw.PowerSlider && d.hw.Undervolt != nil
}

// wantDeviceUndervolt returns the remembered offset and whether it is in
// hardware now; ok is false when there is none (or this is not a slider
// device). An offset of 0 is no offset: it is what a reset leaves while the
// hardware half is still pending. Takes d.mu.
func (d *Daemon) wantDeviceUndervolt() (co int, inForce, ok bool) {
	if !d.deviceUndervoltModel() {
		return 0, false, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	uv := d.state.DeviceUndervolt
	if uv == nil || uv.CPUCO == 0 {
		return 0, false, false
	}
	return uv.CPUCO, uv.Active, true
}

// applyDeviceUndervolt puts the remembered offset in hardware, for a caller
// that has established it applies (no running custom profile with an offset
// of its own). It writes nothing when state says the offset is already in
// force, and reports whether it is in force afterwards. Caller holds hwMu and
// not d.mu.
func (d *Daemon) applyDeviceUndervolt(why string) (bool, error) {
	co, inForce, ok := d.wantDeviceUndervolt()
	if !ok {
		return false, nil
	}
	if inForce {
		return true, nil
	}
	// The probe only here, where an offset is about to be written: its first
	// run is a CO reset.
	if !d.uvProbe() {
		return false, errors.New("the Curve Optimizer is not available (ryzen_smu module missing, or it does not support this platform)")
	}
	if err := d.hw.Undervolt.Apply(co); err != nil {
		// What is in hardware is unknown after a failed write; claim nothing,
		// as applyCustomHW does.
		d.mu.Lock()
		setUndervoltActive(d.state, false)
		d.mu.Unlock()
		return false, err
	}
	slog.Info("undervolt (device setting)", "cpu", co, "why", why)
	d.mu.Lock()
	setUndervoltActive(d.state, false)
	if d.state.DeviceUndervolt != nil {
		d.state.DeviceUndervolt.Active = true
	}
	d.mu.Unlock()
	return true, nil
}

// deviceUndervoltLost marks the remembered offset as no longer in hardware,
// which is true after a boot or a suspend, so the restore that follows writes
// it again instead of trusting a flag the state file carried over. Takes d.mu.
func (d *Daemon) deviceUndervoltLost() {
	d.mu.Lock()
	if d.state.DeviceUndervolt != nil {
		d.state.DeviceUndervolt.Active = false
	}
	d.mu.Unlock()
}

// sliderLanding is the firmware profile a slider command leaves a custom
// profile for: the pin, else the device default ("" when there is neither).
func (d *Daemon) sliderLanding() string {
	if d.hw.ProfilePin != "" {
		return d.hw.ProfilePin
	}
	if d.hw.Profiles != nil {
		return d.hw.Profiles.Default()
	}
	return ""
}

// setDeviceUndervolt is a bare undervolt set or reset on a slider device: co
// becomes the remembered offset (0 clears it), and the hardware follows. Made
// while a custom profile runs, it leaves that profile for the firmware profile,
// as the TDP slider does, and applyStockHW then applies the new setting (or
// clears the profile's offset) as its one SMU step. The caller holds hwMu.
func (d *Daemon) setDeviceUndervolt(cmd string, co int) response {
	d.mu.Lock()
	leaving := ""
	if d.state.InCustomProfile() {
		leaving = d.state.Profile
	}
	prev := cloneUndervolt(d.state.DeviceUndervolt)
	// Active is carried over when only the hardware half is still to come
	// (co 0): it is what tells uvApplied there is an offset to clear.
	wasActive := prev != nil && prev.Active
	d.state.DeviceUndervolt = &api.UndervoltState{CPUCO: co, Active: wasActive && (co == 0 || co == prev.CPUCO)}
	d.mu.Unlock()

	restore := func() {
		d.mu.Lock()
		d.state.DeviceUndervolt = prev
		d.mu.Unlock()
	}

	switch {
	case leaving != "":
		landing := d.sliderLanding()
		if landing == "" {
			restore()
			return response{OK: false, Error: cmd + ": no firmware profile to leave " + leaving + " for"}
		}
		if err := d.applyStockHW(landing); err != nil {
			restore()
			return response{OK: false, Error: cmd + ": leaving profile " + leaving + ": " + err.Error()}
		}
		slog.Info(cmd+": left the custom profile for the undervolt setting", "profile", leaving, "landing", landing, "cpu", co)
		if _, inForce, ok := d.wantDeviceUndervolt(); ok && !inForce {
			d.finishDeviceUndervolt()
			return response{OK: false, Error: cmd + ": left " + leaving + ", but the offset could not be applied (see the daemon log)"}
		}
	case co == 0:
		if d.uvApplied() {
			slog.Info(cmd + ": clearing the Curve Optimizer offset (device setting)")
			if err := d.hw.Undervolt.Reset(); err != nil {
				restore()
				return response{OK: false, Error: cmd + ": " + err.Error()}
			}
		} else {
			slog.Info(cmd + ": no Curve Optimizer offset applied; skipping the SMU write")
		}
		d.mu.Lock()
		setUndervoltActive(d.state, false)
		d.mu.Unlock()
	default:
		if _, err := d.applyDeviceUndervolt(cmd); err != nil {
			restore()
			return response{OK: false, Error: fmt.Sprintf("%s: %v", cmd, err)}
		}
	}
	d.finishDeviceUndervolt()
	return response{OK: true}
}

// finishDeviceUndervolt drops a cleared setting from state and saves. Takes
// d.mu.
func (d *Daemon) finishDeviceUndervolt() {
	d.mu.Lock()
	if uv := d.state.DeviceUndervolt; uv != nil && uv.CPUCO == 0 && !uv.Active {
		d.state.DeviceUndervolt = nil
	}
	s := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(s)
}
