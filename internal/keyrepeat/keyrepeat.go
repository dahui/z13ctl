// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package keyrepeat decides which held direction owns a gamepad's auto-repeat.
//
// Holding a D-pad direction repeats it: one press, then a burst until release.
// The bookkeeping looks trivial and is not, because the events driving it are
// independent. Two directions can be held at once, a button-style D-pad reports
// each direction separately while a hat-axis one reports a shared axis returning
// to centre, and a timer that has already fired can still be running when the
// next press arrives. Getting it wrong produces a direction that repeats forever
// with nothing held, which is indistinguishable from a stuck controller.
//
// Two defects this pins down, both live in z13gui before it existed:
//
//   - Switching direction mid-repeat could leave the *old* direction repeating.
//     The in-flight timer callback re-armed itself after checking only that some
//     timer existed, so it overwrote the new direction's timer with its own.
//   - Releasing one held direction cancelled another still being held, because
//     the stop was unconditional and did not ask who owned the repeat.
//
// It is a separate package because internal/gui/gamepad is excluded from
// `make test` by path, so anything left in there cannot be verified — and this
// is exactly the sort of index-and-ownership bookkeeping that needs to be.
//
// Timers live in the caller: this type only answers "who owns the repeat now"
// and "is this callback still current". A Tracker is not internally
// synchronized; the caller holds its own lock across these calls, since the
// answers are only meaningful together with the timer they describe.
package keyrepeat

// Tracker records which action currently owns the auto-repeat, and hands out a
// generation number so a timer callback can tell whether it is still the one in
// charge.
//
// The zero value is a valid Tracker with no repeat active.
type Tracker[A comparable] struct {
	active bool
	action A
	gen    uint64
}

// Start takes ownership of the repeat for action and returns the generation the
// caller's timer callback must quote to ReArm.
//
// Every Start invalidates the previous generation, so a timer that fired just
// before this call cannot re-arm itself afterwards. That is the whole point: the
// old callback is still going to run, and it has to become a no-op rather than
// resurrect the direction the user has already let go of.
func (t *Tracker[A]) Start(action A) uint64 {
	t.gen++
	t.active = true
	t.action = action
	return t.gen
}

// ReArm reports whether a timer callback holding gen is still the current
// repeat, and therefore whether it should schedule its next tick.
func (t *Tracker[A]) ReArm(gen uint64) bool {
	return t.active && gen == t.gen
}

// Stop ends the repeat and reports whether there was one to end.
//
// With no arguments it stops whatever is active — the right behaviour when the
// reader is shutting down or focus is going away. Given one or more actions it
// stops only if the repeat belongs to one of them, so releasing Left leaves a
// still-held Up repeating. Pass the actions that share the physical control: for
// a hat axis returning to centre that is the two directions on that axis, since
// the event says "this axis is centred" and nothing about the other one.
func (t *Tracker[A]) Stop(only ...A) bool {
	if !t.active {
		return false
	}
	if len(only) > 0 {
		var owned bool
		for _, a := range only {
			if a == t.action {
				owned = true
				break
			}
		}
		if !owned {
			return false
		}
	}
	t.gen++
	t.active = false
	return true
}

// Active reports whether a repeat is currently running.
func (t *Tracker[A]) Active() bool { return t.active }

// Action returns the action that owns the repeat. Only meaningful while Active.
func (t *Tracker[A]) Action() A { return t.action }
