// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package gamepad reads Linux evdev gamepad events and dispatches normalized
// actions to the GUI. It scans /dev/input/event* for gamepad devices, reads
// events in background goroutines, and translates them into Action values.
// Only dispatches when the overlay is visible so games keep their input.
//
// Device classification:
//   - gamepad: full controllers (Xbox, PS, Switch, virtual Steam devices) →
//     read events + EVIOCGRAB to suppress background game input
//   - grab-only: related input devices (PS touchpad) → EVIOCGRAB only,
//     events discarded (prevents touchpad acting as mouse in background)
//   - ignored: accelerometers/gyro (INPUT_PROP_ACCELEROMETER), keyboards,
//     mice, and other non-gamepad devices
//
// Permissions: on modern systemd (Arch, Fedora, Ubuntu 22.04+), the uaccess
// udev tag in 70-uaccess.rules grants the active session user ACL access to
// joystick devices automatically — no input group membership needed. If device
// access fails, the device is silently skipped (graceful degradation).
package gamepad

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/dahui/z13gui/internal/keyrepeat"
	evdev "github.com/holoplot/go-evdev"
)

// Action represents a normalized gamepad input dispatched to the GUI.
type Action int

// Gamepad actions dispatched to the GUI handler.
const (
	ActionUp     Action = iota // D-pad up
	ActionDown                 // D-pad down
	ActionLeft                 // D-pad left
	ActionRight                // D-pad right
	ActionAccept               // A / BTN_SOUTH — activate focused widget
	ActionBack                 // B / BTN_EAST — dismiss / go back
	ActionBumpL                // Left shoulder (LB / BTN_TL)
	ActionBumpR                // Right shoulder (RB / BTN_TR)
)

// Handler is called on the GTK main thread for each gamepad action.
type Handler func(Action)

// deviceClass categorizes an evdev device for input handling.
type deviceClass int

const (
	deviceIgnore   deviceClass = iota // not gamepad-related; skip
	deviceGamepad                     // full gamepad: read events + EVIOCGRAB
	deviceGrabOnly                    // related device (e.g. PS touchpad): EVIOCGRAB only
)

// gamepadButtons are evdev button codes that identify a device as a gamepad.
// Covers Xbox, PlayStation, Nintendo Switch, and Steam virtual controllers.
var gamepadButtons = []evdev.EvCode{
	evdev.BTN_SOUTH,  // A / Cross
	evdev.BTN_EAST,   // B / Circle
	evdev.BTN_NORTH,  // Y / Triangle
	evdev.BTN_WEST,   // X / Square
	evdev.BTN_TL,     // Left bumper
	evdev.BTN_TR,     // Right bumper
	evdev.BTN_TL2,    // Left trigger (digital)
	evdev.BTN_TR2,    // Right trigger (digital)
	evdev.BTN_SELECT, // Select / Share
	evdev.BTN_START,  // Start / Options
	evdev.BTN_MODE,   // PS / Xbox / Home button
	evdev.BTN_THUMBL, // L3 (left stick click)
	evdev.BTN_THUMBR, // R3 (right stick click)
}

// Reader manages gamepad device discovery and event reading.
type Reader struct {
	handler   Handler
	isVisible func() bool
	dispatch  func(func()) // wraps glib.IdleAdd; injected to avoid glib import

	mu       sync.Mutex
	devices  map[string]*evdev.InputDevice // gamepad devices: read events + grab
	grabOnly map[string]*evdev.InputDevice // related devices: grab only (e.g. PS touchpad)
	grabbed  bool                          // true while overlay is visible (exclusive grab)
	grabSeq  uint64                        // highest SetGrabbed seq applied; rejects stale requests
	stop     chan struct{}
}

// New creates a Reader. handler is called (via dispatch) for each action.
// isVisible gates dispatch so events are ignored when the overlay is hidden.
// dispatch must schedule f on the GTK main thread (typically glib.IdleAdd wrapper).
func New(handler Handler, isVisible func() bool, dispatch func(func())) *Reader {
	return &Reader{
		handler:   handler,
		isVisible: isVisible,
		dispatch:  dispatch,
		devices:   make(map[string]*evdev.InputDevice),
		grabOnly:  make(map[string]*evdev.InputDevice),
		stop:      make(chan struct{}),
	}
}

// Run scans for gamepad devices and reads events. Blocks until Stop is called.
func (r *Reader) Run() {
	r.scan()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.scan()
		}
	}
}

// Stop terminates the reader and all device goroutines.
func (r *Reader) Stop() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}

// SetGrabbed acquires (grab=true) or releases (grab=false) exclusive access
// (EVIOCGRAB) on every tracked device, so events either reach only the drawer or
// go back to the desktop and any running game. New devices discovered while
// grabbed are auto-grabbed in tryOpen.
//
// seq orders requests that overlap. Both callers run on their own goroutine —
// the socket work must not block the GTK thread — and the gamescope hide path
// delays its release so the dismiss button's release event is consumed first, so
// they can and do arrive out of order. Applying a stale one is not cosmetic: an
// ungrab landing after a re-show hands the game the same D-pad presses being used
// to navigate the drawer, and a grab landing after a hide leaves every controller
// exclusively grabbed with nothing on screen — no input reaches the game at all
// until the next full open/close cycle.
//
// The caller supplies a seq that increases with each show/hide from the GTK
// thread, which is the only place that knows the intended order.
func (r *Reader) SetGrabbed(seq uint64, grab bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if seq <= r.grabSeq {
		slog.Debug("gamepad: ignoring superseded grab request",
			"seq", seq, "current", r.grabSeq, "grab", grab)
		return
	}
	r.grabSeq = seq
	r.grabbed = grab

	apply := func(path string, dev *evdev.InputDevice) {
		var err error
		failed, done := "ungrab failed", "ungrabbed"
		if grab {
			err = dev.Grab()
			failed, done = "grab failed", "grabbed"
		} else {
			err = dev.Ungrab()
		}
		if err != nil {
			slog.Warn("gamepad: "+failed, "path", path, "err", err)
			return
		}
		slog.Info("gamepad: "+done, "path", path)
	}
	for path, dev := range r.devices {
		apply(path, dev)
	}
	for path, dev := range r.grabOnly {
		apply(path, dev)
	}
}

// scan enumerates /dev/input/event* and starts readers for new devices.
func (r *Reader) scan() {
	paths, err := evdev.ListDevicePaths()
	if err != nil {
		slog.Debug("gamepad: scan failed", "err", err)
		return
	}
	for _, p := range paths {
		r.mu.Lock()
		_, inDevices := r.devices[p.Path]
		_, inGrabOnly := r.grabOnly[p.Path]
		r.mu.Unlock()
		if inDevices || inGrabOnly {
			continue
		}
		r.tryOpen(p.Path)
	}
}

// tryOpen opens a device, classifies it, and starts the appropriate handler.
func (r *Reader) tryOpen(path string) {
	dev, err := evdev.OpenWithFlags(path, os.O_RDONLY)
	if err != nil {
		return
	}

	class := classifyDevice(dev)
	if class == deviceIgnore {
		_ = dev.Close()
		return
	}

	name, _ := dev.Name()
	id, _ := dev.InputID()
	attrs := []any{
		"path", path,
		"name", name,
		"id", fmt.Sprintf("%04x:%04x", id.Vendor, id.Product),
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	switch class {
	case deviceGamepad:
		r.devices[path] = dev
		if r.grabbed {
			if err := dev.Grab(); err != nil {
				slog.Warn("gamepad: grab failed", append(attrs, "err", err)...)
			}
		}
		go r.readLoop(path, dev)
		slog.Info("gamepad: found", append(attrs, "class", "gamepad")...)

	case deviceGrabOnly:
		r.grabOnly[path] = dev
		if r.grabbed {
			if err := dev.Grab(); err != nil {
				slog.Warn("gamepad: grab failed", append(attrs, "err", err)...)
			}
		}
		go r.holdLoop(path, dev)
		slog.Info("gamepad: found", append(attrs, "class", "grab-only")...)
	}
}

// classifyDevice determines how to handle an evdev device.
func classifyDevice(dev *evdev.InputDevice) deviceClass {
	// Skip accelerometers/gyro (PS motion sensors). High-frequency events,
	// not routable to game input — grabbing is wasteful.
	for _, p := range dev.Properties() {
		if p == evdev.INPUT_PROP_ACCELEROMETER {
			return deviceIgnore
		}
	}

	// Check for gamepad button capabilities (Xbox, PS, Switch, virtual).
	keys := dev.CapableEvents(evdev.EV_KEY)
	hasGamepadBtn := false
	for _, k := range keys {
		for _, gb := range gamepadButtons {
			if k == gb {
				hasGamepadBtn = true
				break
			}
		}
		if hasGamepadBtn {
			break
		}
	}
	if hasGamepadBtn {
		// Steam virtual gamepad (VID 28de, PID 11ff) — grab to block game's
		// evdev reader, but don't read events (we read the physical device).
		id, err := dev.InputID()
		if err == nil && id.Vendor == 0x28DE && id.Product == 0x11FF {
			return deviceGrabOnly
		}
		return deviceGamepad
	}

	// Check for touchpad (PS controller touchpad): has multitouch but no
	// gamepad buttons. Must be grabbed to prevent it acting as a mouse.
	abs := dev.CapableEvents(evdev.EV_ABS)
	for _, a := range abs {
		if a == evdev.ABS_MT_POSITION_X {
			return deviceGrabOnly
		}
	}

	return deviceIgnore
}

// repeat timing constants.
const (
	repeatInitial  = 400 * time.Millisecond
	repeatInterval = 120 * time.Millisecond
)

// readLoop reads events from a gamepad device. Runs until the device
// disconnects or the reader is stopped.
func (r *Reader) readLoop(path string, dev *evdev.InputDevice) {
	defer func() {
		r.mu.Lock()
		if r.grabbed {
			_ = dev.Ungrab()
		}
		delete(r.devices, path)
		r.mu.Unlock()
		_ = dev.Close()
		slog.Info("gamepad: disconnected", "path", path)
	}()

	// Auto-repeat for held directions. keyrepeat owns the "who holds the repeat"
	// bookkeeping (tested there); this keeps only the timer.
	var repeatMu sync.Mutex
	var repeatTimer *time.Timer
	var repeat keyrepeat.Tracker[Action]

	// stopRepeat cancels the repeat. With no arguments it stops whatever is
	// active; given actions it stops only if the repeat belongs to one of them,
	// so releasing one held direction leaves another still held repeating.
	stopRepeat := func(only ...Action) {
		repeatMu.Lock()
		defer repeatMu.Unlock()
		if repeat.Stop(only...) && repeatTimer != nil {
			repeatTimer.Stop()
			repeatTimer = nil
		}
	}
	defer stopRepeat()

	startRepeat := func(a Action) {
		repeatMu.Lock()
		defer repeatMu.Unlock()
		if repeatTimer != nil {
			repeatTimer.Stop()
		}
		// gen retires any callback already in flight. Without it the previous
		// direction's timer — which has fired and is waiting on this lock — re-armed
		// itself and overwrote this timer, so the old direction repeated forever
		// while the new one never started.
		gen := repeat.Start(a)
		var tick func()
		tick = func() {
			r.emit(a)
			repeatMu.Lock()
			if repeat.ReArm(gen) {
				repeatTimer = time.AfterFunc(repeatInterval, tick)
			}
			repeatMu.Unlock()
		}
		repeatTimer = time.AfterFunc(repeatInitial, tick)
	}

	for {
		select {
		case <-r.stop:
			return
		default:
		}

		ev, err := dev.ReadOne()
		if err != nil {
			return // device disconnected
		}

		switch ev.Type {
		case evdev.EV_KEY:
			switch ev.Value {
			case 1: // key down
				if a, ok := buttonToAction(ev.Code); ok {
					r.emit(a)
					if isDirectional(a) {
						startRepeat(a)
					}
				}
			case 0: // key up
				if a, ok := buttonToAction(ev.Code); ok {
					if isDirectional(a) {
						// Only this direction: a button-style D-pad reports each
						// direction separately, so an unqualified stop here cancelled
						// a different direction the user was still holding.
						stopRepeat(a)
					}
				}
			}

		case evdev.EV_ABS:
			// A hat axis returning to centre says nothing about the other axis, so
			// each centre event stops only the two directions on its own axis.
			switch ev.Code {
			case evdev.ABS_HAT0Y:
				switch {
				case ev.Value < 0:
					r.emit(ActionUp)
					startRepeat(ActionUp)
				case ev.Value > 0:
					r.emit(ActionDown)
					startRepeat(ActionDown)
				default:
					stopRepeat(ActionUp, ActionDown)
				}
			case evdev.ABS_HAT0X:
				switch {
				case ev.Value < 0:
					r.emit(ActionLeft)
					startRepeat(ActionLeft)
				case ev.Value > 0:
					r.emit(ActionRight)
					startRepeat(ActionRight)
				default:
					stopRepeat(ActionLeft, ActionRight)
				}
			}
		}
	}
}

// holdLoop holds a grab-only device open (e.g. PS touchpad). Reads and
// discards events to detect disconnect for cleanup.
func (r *Reader) holdLoop(path string, dev *evdev.InputDevice) {
	defer func() {
		r.mu.Lock()
		if r.grabbed {
			_ = dev.Ungrab()
		}
		delete(r.grabOnly, path)
		r.mu.Unlock()
		_ = dev.Close()
		slog.Info("gamepad: grab-only disconnected", "path", path)
	}()

	for {
		select {
		case <-r.stop:
			return
		default:
		}
		_, err := dev.ReadOne()
		if err != nil {
			return // device disconnected
		}
	}
}

// emit dispatches an action to the GUI thread if the overlay is visible.
func (r *Reader) emit(a Action) {
	if !r.isVisible() {
		return
	}
	r.dispatch(func() { r.handler(a) })
}

// buttonToAction maps evdev button codes to actions.
func buttonToAction(code evdev.EvCode) (Action, bool) {
	switch code {
	case evdev.BTN_SOUTH:
		return ActionAccept, true
	case evdev.BTN_EAST:
		return ActionBack, true
	case evdev.BTN_TL:
		return ActionBumpL, true
	case evdev.BTN_TR:
		return ActionBumpR, true
	case evdev.BTN_DPAD_UP:
		return ActionUp, true
	case evdev.BTN_DPAD_DOWN:
		return ActionDown, true
	case evdev.BTN_DPAD_LEFT:
		return ActionLeft, true
	case evdev.BTN_DPAD_RIGHT:
		return ActionRight, true
	default:
		return 0, false
	}
}

// isDirectional returns true for actions that should auto-repeat when held.
func isDirectional(a Action) bool {
	return a == ActionUp || a == ActionDown || a == ActionLeft || a == ActionRight
}
