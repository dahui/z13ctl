// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gui

// limitsync.go — keeping the widgets' bounds on what the device reports now,
// not on what it reported when the GUI started.
//
// The device document used to be fetched once, before any widget existed,
// and never again. That was wrong in two ways, both seen on the development
// machine on 2026-10-09: a GUI started against a daemon on asus-nb-wmi kept
// bounding PL1 at the device file's 5–93 W after the armoury grant landed and
// the daemon restarted onto the kernel's 28–80 W; and a GUI started while the
// daemon was down kept the built-in defaults for its whole life. The limits
// can also move inside one daemon's lifetime — armoury keeps separate AC and
// battery tables — and get-state carries them on every reply.
//
// So the bounds now arrive two ways. Every fetched state's tdp_limits is laid
// over the current limits (adoptStateLimits), and every connection to the
// daemon refetches the whole document (adoptDevice). Both end in applyLimits,
// which moves each slider's range in place: nothing is rebuilt, because
// nothing about a slider's *shape* depends on its bounds.
//
// What this cannot follow is a change in *which* controls exist — a daemon
// that comes back reporting a capability it did not have. The drawer's
// sections are chosen once from the document; rebuilding them under a
// running GUI is a different feature. adoptDevice says so in the log instead
// of silently showing a stale set.

import (
	"log/slog"
	"slices"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/controls"
	"github.com/dahui/voltaire/v2/internal/limits"
	"github.com/dahui/voltaire/v2/internal/profileui"
)

// firmware is the device's firmware profiles, from the current document (the
// built-in list when the daemon has not answered).
func (w *Window) firmware() profileui.Firmware {
	return profileui.FirmwareFrom(w.device)
}

// buttonLabel is the hardware button's name from the current document, "" when
// the device gives none (buttonpref then says "hardware button").
func (w *Window) buttonLabel() string {
	if w.device == nil {
		return ""
	}
	return w.device.ButtonLabel
}

// adoptStateLimits lays a fetched state's live power-limit ranges over the
// current limits. Main thread only; call it before syncing widgets from the
// same state, so values land inside the ranges they will be shown in.
func (w *Window) adoptStateLimits(state *api.State) {
	if state == nil {
		return
	}
	w.applyLimits(w.limits.WithTDPLimits(state.TDPLimits))
}

// adoptDevice takes a freshly fetched device document: its limits replace the
// current ones (with the last state's live ranges laid over them), and it
// becomes the document the rest of the GUI reads. A nil document — the daemon
// did not answer — keeps what the GUI already has: falling back to the
// built-in defaults would trade a known answer for a guess. Main thread only.
func (w *Window) adoptDevice(doc *api.DeviceInfo) {
	if doc == nil {
		return
	}
	next := limits.FromDevice(doc)
	if w.state != nil {
		next = next.WithTDPLimits(w.state.TDPLimits)
	}
	prev := w.device
	w.device = doc
	w.applyLimits(next)
	for _, s := range w.settingsViews() {
		s.syncPress() // the button's name is in the document
	}

	// The firmware profile buttons are built once from the document too.
	if have, now := profileui.FirmwareFrom(prev).Names(), profileui.FirmwareFrom(doc).Names(); !slices.Equal(have, now) {
		slog.Warn("the daemon now reports different firmware profiles; restart voltaire-gui to rebuild the profile buttons",
			"built", have, "reported", now)
	}

	// A different capability set changes which drawer sections exist, and the
	// drawer can rebuild them in place (quickbar.go).
	layout, _ := w.layoutConfig()
	resolved, _ := controls.Resolve(layout, doc, w.sessionCaps()...)
	if have, now := controls.IDsOf(w.controls), controls.IDsOf(resolved); !slices.Equal(have, now) {
		slog.Info("the daemon now reports different capabilities; rebuilding the drawer",
			"built", have, "reported", now)
		w.rebuildDrawer()
	}
}

// applyLimits makes next the GUI's limits and moves every built widget onto
// them. A no-op when nothing changed, which is the common case: it runs on
// every state refresh. Main thread only.
func (w *Window) applyLimits(next limits.Limits) {
	if w.limits.Equal(next) {
		return
	}
	slog.Info("device limits changed",
		"tdpMin", next.TDPMin, "tdpSafe", next.TDPMaxSafe, "tdpMax", next.TDPMaxForced,
		"uv", []int{next.UVMin, next.UVMax}, "battery", []int{next.BatteryMin, next.BatteryMax})
	w.limits = next

	// SetRange clamps a value that falls outside the new range, which fires
	// the value-changed handlers. Under w.syncing those are not edits: the
	// battery slider does not send, and the editor's dirty tracking does not
	// count them. The sync that follows sets the real values.
	prev := w.syncing
	w.syncing = true
	defer func() { w.syncing = prev }()
	for _, c := range w.customViews() {
		c.applyLimits()
	}
	for _, b := range w.batterySections() {
		b.applyLimits()
	}
}
