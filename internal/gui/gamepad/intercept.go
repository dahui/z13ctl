// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gamepad

// intercept.go — the second input source: the controller taken through the
// daemon's gamepad-intercept (InputPlumber's intercept mode), for machines
// where InputPlumber hides the controller and presents games a virtual one
// this reader cannot open or grab (the Ally's emulated Steam Deck controller
// is hidraw only). Intercepting also stops InputPlumber feeding that virtual
// controller, so nothing reaches a game while the overlay is up and the hidraw
// blocker has nothing to do there. Everywhere else the request is refused and
// the evdev reader carries on alone.

import (
	"log/slog"

	"github.com/dahui/voltaire/api/v2"
)

// Press is one press or release from an intercepted controller.
type Press struct {
	Action  Action
	Pressed bool
}

// InterceptFunc takes the controller and returns its presses until release is
// called; the channel closes then, or when the source goes away. It returns a
// nil channel when there is no source (the daemon is not running), and an
// error when the source refused.
type InterceptFunc func() (presses <-chan Press, release func(), err error)

// SetIntercept installs the second source. Each grab opens it and each
// release gives it back, under the same ordering as the evdev grab.
func (r *Reader) SetIntercept(f InterceptFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intercept = f
}

// setInterceptLocked follows a grab request: open on grab (off the lock, since
// it is a socket round trip), give back on release. Callers hold r.mu.
func (r *Reader) setInterceptLocked(seq uint64, grab bool) {
	if r.intercept == nil {
		return
	}
	if grab {
		go r.runIntercept(r.intercept, seq)
		return
	}
	if rel := r.icptRelease; rel != nil {
		r.icptRelease = nil
		go rel()
	}
}

// runIntercept holds one intercept for the grab numbered seq, feeding its
// presses to the handler until it is given back. A grab superseded while the
// request was in flight gives it straight back: holding the controller with
// the overlay gone is the one outcome that must not happen.
func (r *Reader) runIntercept(open InterceptFunc, seq uint64) {
	presses, release, err := open()
	if presses == nil {
		r.mu.Lock()
		first := !r.icptLogged
		r.icptLogged = true
		r.mu.Unlock()
		if err != nil && first {
			slog.Info("gamepad: controller intercept unavailable; reading evdev only", "reason", err)
		}
		return
	}
	r.mu.Lock()
	if r.grabSeq != seq || !r.grabbed || r.icptRelease != nil {
		r.mu.Unlock()
		release()
		return
	}
	r.icptGen++
	gen := r.icptGen
	r.icptRelease = release
	r.mu.Unlock()
	slog.Info("gamepad: reading the controller through InputPlumber")

	rep := &repeater{emit: r.emit}
	defer rep.stop()
	for p := range presses {
		rep.press(p.Action, p.Pressed)
	}

	r.mu.Lock()
	if r.icptGen == gen && r.icptRelease != nil {
		r.icptRelease = nil // the source went away on its own (daemon restart)
		slog.Info("gamepad: controller intercept ended")
	}
	r.mu.Unlock()
}

// actionNames maps the daemon's gamepad actions to the drawer's.
var actionNames = map[string]Action{
	"up": ActionUp, "down": ActionDown, "left": ActionLeft, "right": ActionRight,
	"accept": ActionAccept, "back": ActionBack, "l1": ActionBumpL, "r1": ActionBumpR,
}

// DaemonIntercept is the InterceptFunc for voltaire's daemon (api.InterceptGamepad).
func DaemonIntercept() (presses <-chan Press, release func(), err error) {
	events, release, err := api.InterceptGamepad()
	if events == nil {
		return nil, nil, err
	}
	out := make(chan Press, 16)
	go func() {
		defer close(out)
		for ev := range events {
			if a, ok := actionNames[ev.Action]; ok {
				out <- Press{Action: a, Pressed: ev.Pressed}
			}
		}
	}()
	return out, release, nil
}
