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
	w.syncSettings()
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
		// The settings page's switches, and it must be inside the syncing
		// guard: gtk.Switch fires state-set on a *programmatic* SetActive too,
		// so an unguarded sync writes every row straight back to the daemon —
		// which calls refreshState again.
		//
		// Here rather than only in syncState because this is the funnel: every
		// write path ends in refreshState, the daemon's state-changed broadcast
		// calls it, and the full window's show() calls it, whereas syncState
		// runs on the drawer's own fetch — a surface the settings page does not
		// live on. Without this the page is correct only at the moment its tab
		// is first opened.
		w.syncSettings()
		// The charge limit and the RGB block, for the same reason and since the
		// same date: both are on the dashboard rail now, and both were synced
		// only by syncState — the drawer's own fetch. Left out, the window's
		// copies would show whatever the daemon happened to be reporting when
		// the page was built and never move again.
		w.syncBattery()
		w.syncLightingSection()
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
		if gen != w.telemetryGen || !w.anyVisible() {
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

				// Custom view telemetry, on whichever surface is showing
				// one. Asked through the view's own host rather than the
				// drawer's stack, so the full window's instance is served by
				// the same poll instead of needing a second.
				for _, c := range w.customViews() {
					if c.host.current() {
						c.pollTick(state)
					}
				}

				// The dashboard's battery card header. Fed from this poll
				// rather than the history reply so the state word and the
				// wattage come from one source — see dashboardView.pollTick.
				if m := w.mainWin; m != nil && m.dashboard != nil && m.dashboard.host.current() {
					m.dashboard.pollTick(state)
				}
			})
		}()
		return true
	})
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

// sendFeatureSet writes one firmware toggle by its wire id — the only toggle
// write path the GUI has, and what the settings page uses for every row.
//
// It replaced a named send per toggle when the drawer's two bespoke switches
// moved to that page. The api keeps SendBootSoundSet and SendPanelOverdriveSet
// for the CLI and for clients written against them; nothing here needs a
// function per toggle, which is the point of rendering rows from the document.
//
// The error names the toggle's id rather than a prose label because this
// function does not have one: the label is device data the page renders, and
// an id is what the user would grep the daemon's log for.
func (w *Window) sendFeatureSet(id string, value int) {
	go func() {
		slog.Debug("sendFeatureSet: calling daemon", "id", id, "value", value)
		start := time.Now()
		if err := apiresult.Err(api.SendFeatureSet(id, value)); err != nil {
			w.reportError("Set "+id, err)
			return
		}
		w.clearErrorAsync()
		slog.Debug("sendFeatureSet: done", "id", id, "elapsed", time.Since(start))
	}()
}
