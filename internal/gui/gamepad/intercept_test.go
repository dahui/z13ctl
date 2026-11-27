// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package gamepad

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSource is an InterceptFunc whose presses the test sends.
type fakeSource struct {
	mu       sync.Mutex
	opened   int
	released int
	ch       chan Press
	refuse   error
}

func (f *fakeSource) open() (presses <-chan Press, release func(), err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return nil, nil, f.refuse
	}
	f.opened++
	ch := make(chan Press, 8)
	f.ch = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			f.mu.Lock()
			f.released++
			f.mu.Unlock()
			close(ch)
		})
	}, nil
}

func (f *fakeSource) counts() (opened, released int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opened, f.released
}

func (f *fakeSource) send(p Press) {
	f.mu.Lock()
	ch := f.ch
	f.mu.Unlock()
	ch <- p
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newTestReader(got *[]Action, mu *sync.Mutex) *Reader {
	return New(func(a Action) {
		mu.Lock()
		*got = append(*got, a)
		mu.Unlock()
	}, func() bool { return true }, func(f func()) { f() })
}

func TestInterceptFeedsTheHandlerWhileGrabbed(t *testing.T) {
	var mu sync.Mutex
	var got []Action
	r := newTestReader(&got, &mu)
	src := &fakeSource{}
	r.SetIntercept(src.open)

	r.SetGrabbed(1, true)
	waitFor(t, "the intercept to open", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.icptRelease != nil
	})
	src.send(Press{ActionAccept, true})
	src.send(Press{ActionAccept, false}) // a release emits nothing
	src.send(Press{ActionBumpR, true})
	waitFor(t, "two actions", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 })
	mu.Lock()
	if got[0] != ActionAccept || got[1] != ActionBumpR {
		t.Errorf("actions = %v", got)
	}
	mu.Unlock()

	r.SetGrabbed(2, false)
	waitFor(t, "the release", func() bool { _, rel := src.counts(); return rel == 1 })
}

func TestInterceptSupersededGrabGivesItBack(t *testing.T) {
	var mu sync.Mutex
	var got []Action
	r := newTestReader(&got, &mu)
	src := &fakeSource{}
	// The open is slow; a hide lands before it returns.
	gate := make(chan struct{})
	r.SetIntercept(func() (<-chan Press, func(), error) { <-gate; return src.open() })
	r.SetGrabbed(1, true)
	r.SetGrabbed(2, false)
	close(gate)
	waitFor(t, "the late intercept to be given back", func() bool {
		opened, released := src.counts()
		return opened == 1 && released == 1
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.icptRelease != nil {
		t.Error("a superseded intercept is still held with the overlay gone")
	}
}

func TestInterceptRefusedLeavesEvdevAlone(t *testing.T) {
	var mu sync.Mutex
	var got []Action
	r := newTestReader(&got, &mu)
	src := &fakeSource{refuse: errors.New("InputPlumber is not running")}
	r.SetIntercept(src.open)
	r.SetGrabbed(1, true)
	waitFor(t, "the refusal to be logged", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.icptLogged })
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.icptRelease != nil || !r.grabbed {
		t.Errorf("after a refusal: held %v, grabbed %v; the evdev grab must stand", r.icptRelease != nil, r.grabbed)
	}
}

func TestInterceptSourceEndingClearsTheHold(t *testing.T) {
	var mu sync.Mutex
	var got []Action
	r := newTestReader(&got, &mu)
	src := &fakeSource{}
	r.SetIntercept(src.open)
	r.SetGrabbed(1, true)
	waitFor(t, "open", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.icptRelease != nil })
	src.mu.Lock()
	close(src.ch) // the daemon restarted
	src.ch = nil
	src.mu.Unlock()
	waitFor(t, "the hold to clear", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.icptRelease == nil })
}

func TestRepeaterPress(t *testing.T) {
	var mu sync.Mutex
	var got []Action
	p := &repeater{emit: func(a Action) { mu.Lock(); got = append(got, a); mu.Unlock() }}
	p.press(ActionDown, true)
	p.press(ActionDown, false) // released before the first repeat
	p.press(ActionAccept, true)
	p.press(ActionAccept, false)
	time.Sleep(repeatInitial + repeatInterval)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != ActionDown || got[1] != ActionAccept {
		t.Errorf("emitted %v; a released direction must not repeat", got)
	}
}

func TestActionNamesCoverTheDaemonsSet(t *testing.T) {
	// The daemon's set (api.GamepadEvent's doc) and buttonToAction's targets.
	for name, want := range map[string]Action{
		"up": ActionUp, "down": ActionDown, "left": ActionLeft, "right": ActionRight,
		"accept": ActionAccept, "back": ActionBack, "l1": ActionBumpL, "r1": ActionBumpR,
	} {
		if got, ok := actionNames[name]; !ok || got != want {
			t.Errorf("%q → %v, %v; want %v", name, got, ok, want)
		}
	}
}
