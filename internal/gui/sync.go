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

// refreshState fetches daemon state and re-syncs the custom view, the profile
// section, the autoswitch section, and the header. Every profile operation
// and daemon event uses it rather than syncing one widget: the fan curve
// editor's PWM floor is derived from the target's limit, so a TDP change has
// to re-evaluate the whole view, not just a highlight — and a profile
// created or deleted by another client has to reshape the list.
// Safe to call from a background goroutine.
func (w *Window) refreshState() {
	ok, state, rawErr := api.SendGetState()
	// Report a failed read rather than returning quietly. This runs after a
	// successful write, so a failure here means the daemon went away in between
	// and everything on screen is now stale — worth saying, since the widgets
	// otherwise keep displaying values nothing is honouring.
	if err := apiresult.Err(ok, rawErr); err != nil {
		w.reportError("Read daemon state", err)
		return
	}
	if state == nil {
		return
	}
	glib.IdleAdd(func() {
		// While a popup is open the whole refresh is deferred, state
		// assignment included: syncAutoswitch would relabel the very trigger
		// the popup is anchored to, and syncAutoswitchVis could hide it.
		// closePopup re-runs refreshState, which fetches fresh state anyway.
		if w.syncsSuppressed() {
			return
		}
		w.state = state
		w.syncCustomView()
		w.syncing = true
		w.syncProfiles()
		w.syncAutoswitch()
		w.syncing = false
		w.updateHeader()
	})
}

// startTelemetryPolling begins polling the daemon for APU temp and fan RPM
// every second while the drawer is visible. It updates the header telemetry
// label on every view, and the custom view's own labels and fan curve
// indicator while that view is the visible one.
//
// It reads get-state, which every view wants. The dashboard's own loop is
// separate because it reads telemetry-history — a much larger reply that only
// one view has any use for.
func (w *Window) startTelemetryPolling() {
	w.telemetryGen++
	gen := w.telemetryGen
	glib.TimeoutAdd(1000, func() bool {
		if gen != w.telemetryGen || !w.visible.Load() {
			return false
		}
		// One request at a time. api commands carry a 10s deadline, so against a
		// slow daemon a goroutine per tick meant ten overlapping requests whose
		// replies could apply out of order and walk w.state backwards.
		if w.telemetryBusy {
			slog.Debug("telemetry: skipping tick, request still in flight")
			return true
		}
		w.telemetryBusy = true
		go func() {
			ok, state, err := api.SendGetState()
			glib.IdleAdd(func() {
				// Cleared unconditionally, including on the failure paths below:
				// leaving it set would stop the poll for good.
				w.telemetryBusy = false
				if gen != w.telemetryGen {
					return
				}
				// Deliberately silent, unlike every other daemon call: this is a
				// background poll the user did not ask for, and reporting it would
				// repaint the bar every second, overwriting whatever error they were
				// reading. Their next action reports it — see refreshState.
				if !ok || err != nil || state == nil {
					return
				}
				w.state = state

				// Header telemetry (visible on all views).
				w.updateHeader()

				// Custom view telemetry (only when active).
				if w.viewStack != nil && w.viewStack.VisibleChildName() == "custom" {
					w.custom.pollTick(state)
				}
			})
		}()
		return true
	})
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
