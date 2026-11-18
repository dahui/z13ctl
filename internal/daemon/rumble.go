// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// rumble.go — the game controller's vibration strength (driver.Rumble; the
// OneXPlayer X2's hid-oxp rumble_intensity).
//
// The same contract as CPU boost: get-state and rumble-get read the hardware,
// and the value in state is the user's instruction, replayed at startup and
// after a resume because the controller is not known to keep it. Nothing is
// written until the user first sets one. It reaches no EC and no SMU (one HID
// report to the controller), so none of the wedged-EC gates apply.

import (
	"errors"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
)

// readRumble reports the live strength, or nil with no control or a failed
// read (a client renders that as unknown, never as off). Not under hwMu, as
// every read here avoids it.
func (d *Daemon) readRumble() *int {
	if d.hw == nil || d.hw.Rumble == nil {
		return nil
	}
	v, err := d.hw.Rumble.Get()
	if err != nil {
		slog.Debug("rumble read failed", "err", err)
		return nil
	}
	return &v
}

func (d *Daemon) handleRumbleGet() response {
	if d.hw == nil || d.hw.Rumble == nil {
		return response{OK: false, Error: "reading rumble: not supported on this device"}
	}
	v, err := d.hw.Rumble.Get()
	if err != nil {
		return response{OK: false, Error: "reading rumble: " + err.Error()}
	}
	return response{OK: true, Value: strconv.Itoa(v)}
}

// handleRumble sets the strength and records it. The driver refuses a value
// outside the kernel's range before writing, and that refusal is the answer.
func (d *Daemon) handleRumble(req request) response {
	v, err := strconv.Atoi(strings.TrimSpace(req.Set))
	if err != nil {
		return response{OK: false, Error: "rumble strength must be a whole number"}
	}
	if d.hw == nil || d.hw.Rumble == nil {
		return response{OK: false, Error: "rumble: not supported on this device"}
	}
	d.hwMu.Lock()
	defer d.hwMu.Unlock()
	if err := d.hw.Rumble.Set(v); err != nil {
		msg := "rumble: " + err.Error()
		if errors.Is(err, fs.ErrPermission) {
			msg += " (run 'sudo voltaire setup' to grant access)"
		}
		return response{OK: false, Error: msg}
	}
	slog.Info("rumble", "set", v)

	d.mu.Lock()
	d.state.Rumble = &v
	snapshot := cloneState(d.state)
	d.mu.Unlock()
	d.saveAndNotify(snapshot)
	return response{OK: true}
}

// restoreRumble puts the user's strength back, if they ever set one. Called at
// startup and after a resume; a failure is logged and left, since the
// controller may still be enumerating and nothing else depends on it.
func (d *Daemon) restoreRumble(when string) {
	if d.hw == nil || d.hw.Rumble == nil {
		return
	}
	d.mu.Lock()
	want := d.state.Rumble
	d.mu.Unlock()
	if want == nil {
		return
	}
	if cur, err := d.hw.Rumble.Get(); err == nil && cur == *want {
		return
	}
	if err := d.hw.Rumble.Set(*want); err != nil {
		slog.Warn(when+": failed to restore the rumble strength", "want", *want, "err", err)
		return
	}
	slog.Info(when+": rumble strength restored", "value", *want)
}
