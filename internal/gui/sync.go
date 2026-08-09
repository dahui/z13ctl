// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// sync.go — daemon state synchronization and API communication.

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13gui/internal/colorconv"
	"github.com/dahui/z13gui/internal/daemon"
	"github.com/dahui/z13gui/internal/lighting"
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

// syncModeVis shows/hides color and speed sections based on the active mode.
// Safe to call at any time (including during sync).
func (w *Window) syncModeVis() {
	c := lighting.ControlsFor(activeButton(w.modeButtons, lighting.DefaultMode))
	if w.color1Box != nil {
		w.color1Box.SetVisible(c.Color1)
	}
	if w.color2Box != nil {
		w.color2Box.SetVisible(c.Color2)
	}
	if w.speedBox != nil {
		w.speedBox.SetVisible(c.Speed)
	}
	if w.brightBox != nil {
		w.brightBox.SetVisible(c.Brightness)
	}
}

// syncState updates all widgets from the current daemon state.
// Sets syncing=true to suppress signal handlers from firing sendApply.
func (w *Window) syncState() {
	if w.state == nil {
		return
	}
	w.syncing = true
	defer func() { w.syncing = false }()
	w.syncLightingSection()
	w.syncProfile()
	w.syncBattery()
	w.syncOverdrive()
	w.syncBootSound()
	w.syncCustomView()
	if w.headerTelemetry != nil {
		w.headerTelemetry.SetLabel(fmt.Sprintf("%d°C · %d RPM", w.state.Temperature, w.state.FanRPM))
	}
}

// syncLightingSection updates mode, colors, speed, and brightness from the
// daemon state for the active device tab.
func (w *Window) syncLightingSection() {
	prev := w.syncing
	w.syncing = true
	defer func() { w.syncing = prev }()

	ls := lighting.StateForZone(w.state, w.tab)
	setActiveButton(w.modeButtons, lighting.ResolveMode(ls))
	// Normalize on ingest. Daemon state is not guaranteed well-formed — z13ctl has
	// had corrupt-state-file bugs — and an unparseable colour used to silently
	// become black in the picker and then be written back to the hardware on the
	// next apply. Keep the previous value instead.
	if w.color1 != nil {
		if hex, ok := colorconv.Normalize(ls.Color); ok {
			w.color1.hex = hex
		} else if ls.Color != "" {
			slog.Warn("daemon sent an unparseable color, keeping previous", "zone", w.tab, "field", "color", "value", ls.Color)
		}
	}
	if w.color2 != nil {
		if hex, ok := colorconv.Normalize(ls.Color2); ok {
			w.color2.hex = hex
		} else if ls.Color2 != "" {
			slog.Warn("daemon sent an unparseable color, keeping previous", "zone", w.tab, "field", "color2", "value", ls.Color2)
		}
	}
	w.updateSwatches()
	setActiveButton(w.speedBtns, lighting.ResolveSpeed(ls))
	if w.brightScale != nil {
		w.brightScale.SetValue(float64(lighting.ResolveBrightness(ls)))
	}
	w.syncModeVis()
}

// syncProfile highlights the profile button matching the daemon state.
func (w *Window) syncProfile() {
	if w.state == nil || w.state.Profile == "" {
		return
	}
	setActiveButton(w.profileBtns, w.state.Profile)
}

// syncBattery sets the battery limit scale to match the daemon state.
func (w *Window) syncBattery() {
	if w.state == nil || w.state.Battery == 0 || w.battScale == nil {
		return
	}
	w.battScale.SetValue(float64(w.state.Battery))
}

// queueApply debounces rapid API calls from continuous inputs (color wheel,
// sliders). Discrete inputs (mode buttons, speed buttons, preset clicks)
// call sendApply directly.
func (w *Window) queueApply() {
	if w.syncing {
		return
	}
	if w.applyTimer != nil {
		w.applyTimer.Stop()
	}
	w.applyTimer = time.AfterFunc(150*time.Millisecond, func() {
		glib.IdleAdd(func() bool {
			w.sendApply()
			return false
		})
	})
}

// sendApply sends the current lighting state to the daemon. Guarded by
// w.syncing to prevent sending defaults during widget initialization.
func (w *Window) sendApply() {
	if w.syncing {
		return
	}
	color1 := lighting.DefaultColor1
	if w.color1 != nil {
		color1 = w.color1.hex
	}
	color2 := lighting.DefaultColor2
	if w.color2 != nil {
		color2 = w.color2.hex
	}

	mode := activeButton(w.modeButtons, lighting.DefaultMode)
	speed := activeButton(w.speedBtns, lighting.DefaultSpeed)

	brightness := lighting.DefaultBrightness
	if w.brightScale != nil {
		brightness = int(w.brightScale.Value())
	}

	// Widget reads happen above, on the GTK thread; only the socket round-trip
	// runs in the goroutine. api commands carry a 10s deadline, so calling them
	// inline would freeze the drawer for that long against a wedged daemon.
	device := w.tab

	// "off" uses the daemon's dedicated off command so that Enabled=false
	// is persisted and survives a reboot.
	if mode == "off" {
		go func() {
			slog.Debug("sendApply: calling daemon off", "device", device)
			start := time.Now()
			if err := daemon.Err(api.SendOff(device)); err != nil {
				w.reportError("Turn off "+device+" lighting", err)
				return
			}
			w.clearErrorAsync()
			slog.Debug("sendApply: off done", "elapsed", time.Since(start))
		}()
		return
	}

	go func() {
		slog.Debug("sendApply: calling daemon", "device", device, "mode", mode, "brightness", brightness)
		start := time.Now()
		if err := daemon.Err(api.SendApply(device, color1, color2, mode, speed, brightness)); err != nil {
			w.reportError("Apply "+device+" lighting", err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendApply: done", "elapsed", time.Since(start))
	}()
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
		if err := daemon.Err(api.SendProfileSet(prof)); err != nil {
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
					if err := daemon.Err(api.SendBatteryLimitSet(val)); err != nil {
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
		if err := daemon.Err(api.SendPanelOverdriveSet(value)); err != nil {
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
		if err := daemon.Err(api.SendBootSoundSet(value)); err != nil {
			w.reportError("Set boot sound", err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendBootSoundSet: done", "elapsed", time.Since(start))
	}()
}
