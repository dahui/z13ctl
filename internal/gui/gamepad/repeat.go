// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gamepad

// repeat.go — auto-repeat for a held direction, one per input source (each
// evdev device, the intercept stream), so a release on one source never stops
// a direction held on another.

import (
	"sync"
	"time"

	"github.com/dahui/voltaire/v2/internal/keyrepeat"
)

// repeater re-emits a held direction. keyrepeat owns the "who holds the
// repeat" bookkeeping (tested there); this keeps only the timer.
type repeater struct {
	emit  func(Action)
	mu    sync.Mutex
	timer *time.Timer
	track keyrepeat.Tracker[Action]
}

// stop cancels the repeat. With no arguments it stops whatever is active;
// given actions it stops only if the repeat belongs to one of them, so
// releasing one held direction leaves another still held repeating.
func (p *repeater) stop(only ...Action) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.track.Stop(only...) && p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

// start begins repeating a, replacing any other repeat.
func (p *repeater) start(a Action) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	// gen retires any callback already in flight. Without it the previous
	// direction's timer — which has fired and is waiting on this lock — re-armed
	// itself and overwrote this timer, so the old direction repeated forever
	// while the new one never started.
	gen := p.track.Start(a)
	var tick func()
	tick = func() {
		p.emit(a)
		p.mu.Lock()
		if p.track.ReArm(gen) {
			p.timer = time.AfterFunc(repeatInterval, tick)
		}
		p.mu.Unlock()
	}
	p.timer = time.AfterFunc(repeatInitial, tick)
}

// press handles one press or release from a source that reports both: emit on
// the press, and repeat a direction until its release.
func (p *repeater) press(a Action, pressed bool) {
	switch {
	case pressed:
		p.emit(a)
		if isDirectional(a) {
			p.start(a)
		}
	case isDirectional(a):
		p.stop(a)
	}
}
