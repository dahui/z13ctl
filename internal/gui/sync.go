// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// sync.go — daemon state synchronization and API communication.

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/apiresult"
	"github.com/dahui/voltaire/v2/internal/profileui"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// activeButton returns the key of the button with the .active CSS class,
// or the fallback value if none is found.
func activeButton(btns map[string]*gtk.Button, fallback string) string {
	for k, b := range btns {
		if b.HasCSSClass("active") {
			return k
		}
	}
	return fallback
}

// syncState updates all widgets from the current daemon state.
// Sets syncing=true to suppress signal handlers from firing sendApply.
func (w *Window) syncState() {
	if w.state == nil {
		return
	}
	if w.syncsSuppressed() {
		return
	}
	w.syncing = true
	defer func() { w.syncing = false }()
	w.syncLightingSection()
	w.syncProfiles()
	w.syncAutoswitch()
	w.syncBattery()
	w.syncOverdrive()
	w.syncBootSound()
	w.syncCustomView()
	w.updateHeader()
}

// updateHeader refreshes the header line: the power source when it is known,
// then live temperature and fan speed. The power label deliberately shows
// nothing when the source is unknown (a VM, a desktop, a pre-2.0 daemon) —
// see profileui.PowerLabel. Refreshed by syncState, the telemetry poll, and
// refreshState, which the power-source event triggers.
func (w *Window) updateHeader() {
	if w.headerTelemetry == nil || w.state == nil {
		return
	}
	text := fmt.Sprintf("%d°C · %d RPM", w.state.Temperature, w.state.FanRPM)
	if p := profileui.PowerLabel(w.state); p != "" {
		text = p + " · " + text
	}
	w.headerTelemetry.SetLabel(text)
}

// syncBattery sets the battery limit scale to match the daemon state.
func (w *Window) syncBattery() {
	if w.state == nil || w.state.Battery == 0 || w.battScale == nil {
		return
	}
	w.battScale.SetValue(float64(w.state.Battery))
}

// sendProfileSet sends a profile change to the daemon.
// The state refresh runs on this same goroutine, after the set returns. It must
// not be a separate goroutine: switching to a stock profile makes the daemon
// rewrite the PPT values and release the fans to firmware auto, and a concurrent
// get-state can read the old values and repaint the custom view with them.
func (w *Window) sendProfileSet(prof string) {
	go func() {
		slog.Debug("sendProfileSet: calling daemon", "profile", prof)
		start := time.Now()
		if err := apiresult.Err(api.SendProfileSet(prof)); err != nil {
			w.reportError("Set "+prof+" profile", err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendProfileSet: done", "elapsed", time.Since(start))
		w.refreshState()
	}()
}

// initBatteryDebounce sets up debounced battery limit changes on the given scale.
func (w *Window) initBatteryDebounce(sc *gtk.Scale) {
	var debounce *time.Timer
	sc.ConnectValueChanged(func() {
		// The syncing guard every other input has. syncBattery sets this scale from
		// daemon state, which fires this handler, so without it every drawer open
		// wrote the limit straight back to the hardware 200ms later. Harmless while
		// the write succeeds — it is the value the daemon just reported — but on a
		// device that rejects it that is now a visible error bar on every open,
		// since daemon failures are no longer swallowed.
		//
		// Checked here rather than in the timer: by the time it fires the sync has
		// long finished and the flag is false again.
		if w.syncing {
			return
		}
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(200*time.Millisecond, func() {
			glib.IdleAdd(func() bool {
				val := int(sc.Value()) // scale read must stay on the GTK thread
				go func() {
					slog.Debug("sendBatteryLimitSet: calling daemon", "limit", val)
					start := time.Now()
					if err := apiresult.Err(api.SendBatteryLimitSet(val)); err != nil {
						w.reportError("Set battery limit", err)
						return
					}
					w.clearErrorAsync()
					slog.Debug("sendBatteryLimitSet: done", "elapsed", time.Since(start))
				}()
				return false
			})
		})
	})
}

// syncOverdrive sets the overdrive switch to match the daemon state.
func (w *Window) syncOverdrive() {
	if w.state == nil || w.overdriveSwitch == nil {
		return
	}
	w.overdriveSwitch.SetActive(w.state.PanelOverdrive != 0)
}

// syncBootSound sets the boot sound switch to match the daemon state.
func (w *Window) syncBootSound() {
	if w.state == nil || w.bootSoundSwitch == nil {
		return
	}
	w.bootSoundSwitch.SetActive(w.state.BootSound != 0)
}

// sendOverdriveSet sends a panel overdrive change to the daemon.
func (w *Window) sendOverdriveSet(value int) {
	go func() {
		slog.Debug("sendOverdriveSet: calling daemon", "value", value)
		start := time.Now()
		if err := apiresult.Err(api.SendPanelOverdriveSet(value)); err != nil {
			w.reportError("Set panel overdrive", err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendOverdriveSet: done", "elapsed", time.Since(start))
	}()
}

// sendBootSoundSet sends a boot sound change to the daemon.
func (w *Window) sendBootSoundSet(value int) {
	go func() {
		slog.Debug("sendBootSoundSet: calling daemon", "value", value)
		start := time.Now()
		if err := apiresult.Err(api.SendBootSoundSet(value)); err != nil {
			w.reportError("Set boot sound", err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendBootSoundSet: done", "elapsed", time.Since(start))
	}()
}
