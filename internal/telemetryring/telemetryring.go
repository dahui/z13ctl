// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package telemetryring is the daemon's bounded history of telemetry samples:
// the 1 Hz sampler writes it, the dashboard's telemetry-history command reads
// it back over a time window.
//
// It is a separate package for the same reason panelgeom and focusgrid are —
// what it holds is a fixed-size circular buffer with copy-in/copy-out
// semantics and a time filter, none of which needs hardware, and all of which
// is worth pinning down. internal/gui cannot even be compiled by `make test`;
// this is the half of the dashboard that can be.
//
// # Timestamps
//
// The ring stores the timestamp the caller supplies rather than reading a
// clock itself. That keeps it testable, and it is also the honest split: the
// ring has no way to know whether the caller means wall-clock or monotonic
// time, and the two answer differently across a suspend. Go's monotonic clock
// does not advance while the machine is asleep (CLOCK_MONOTONIC on Linux — the
// same fact reconcileSuspendMaxTicks is counted in ticks for), so a ten-hour
// suspend would look like no elapsed time at all and stale samples would sit
// inside the window forever. The daemon therefore passes wall-clock time, and
// the ring is written to survive what that costs: a clock step backwards
// cannot make it panic, reorder its contents, or discard anything it was not
// asked to.
//
// # Concurrency
//
// A Ring is safe for concurrent use. That is a deliberate exception to the
// project's "serialization lives in the daemon" rule, and it is safe because
// the lock is never held across a call out: it guards a slice copy and
// nothing else, so it cannot participate in the daemon's hwMu → d.mu order or
// deadlock against it. The alternative — a fifth lock in the daemon, taken by
// a watcher and by socket handlers — is exactly the shape that produced the
// saveState race.
package telemetryring

import (
	"sync"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// Entry is one sample and the moment it was taken.
type Entry struct {
	At     time.Time
	Sample driver.Sample
}

// clone returns a copy that shares nothing with e. driver.Sample carries an
// RPM slice, so a plain struct copy still aliases the caller's backing array —
// the same trap cloneState exists for. Every value crossing the ring's
// boundary in either direction goes through here.
func (e Entry) clone() Entry {
	out := e
	out.Sample.RPM = append([]int(nil), e.Sample.RPM...)
	return out
}

// Ring is a fixed-capacity circular buffer of telemetry samples. Once full,
// each new sample overwrites the oldest.
//
// The zero Ring is valid and retains nothing, which is also what New(0) or a
// negative capacity gives you: a device that declares no telemetry history
// gets a ring that accepts every Add and answers every read with nothing,
// rather than a nil pointer every caller must check.
type Ring struct {
	mu    sync.Mutex
	buf   []Entry
	next  int // index the next Add writes
	count int // entries retained, saturating at len(buf)
}

// New returns a Ring holding at most capacity samples.
func New(capacity int) *Ring {
	if capacity <= 0 {
		return &Ring{}
	}
	return &Ring{buf: make([]Entry, capacity)}
}

// Add records s as taken at at, evicting the oldest sample if the ring is
// full. The sample is copied, so the caller may reuse its RPM slice.
func (r *Ring) Add(at time.Time, s driver.Sample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 {
		return
	}
	r.buf[r.next] = Entry{At: at, Sample: s}.clone()
	r.next = (r.next + 1) % len(r.buf)
	if r.count < len(r.buf) {
		r.count++
	}
}

// Len reports how many samples the ring currently holds.
func (r *Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// Cap reports how many samples the ring can hold.
func (r *Ring) Cap() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

// All returns every retained sample in insertion order, oldest first, as
// copies.
//
// Insertion order is not re-sorted by timestamp. Whenever the caller's clock
// runs forward the two are the same thing; when it does not, keeping what was
// recorded beats inventing an order for it, and a kink in a graph is a smaller
// lie than silently reordering history.
func (r *Ring) All() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot()
}

// snapshot copies the retained entries oldest-first. Caller holds r.mu.
func (r *Ring) snapshot() []Entry {
	if r.count == 0 {
		return nil
	}
	out := make([]Entry, 0, r.count)
	start := 0
	if r.count == len(r.buf) {
		start = r.next // full: the oldest sample is the one about to be overwritten
	}
	for i := range r.count {
		out = append(out, r.buf[(start+i)%len(r.buf)].clone())
	}
	return out
}

// Since returns the samples taken within d of now, oldest first, as copies.
// The boundary is inclusive: a sample taken exactly d ago is in the window,
// so a 300-sample second-spaced ring answers Since(now, 300*time.Second) with
// all 300.
//
// A non-positive d asks for an empty window and gets nil — never the whole
// ring, which is what a caller that failed to parse a "seconds" field would
// otherwise be handed.
func (r *Ring) Since(now time.Time, d time.Duration) []Entry {
	if d <= 0 {
		return nil
	}
	cutoff := now.Add(-d)

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Entry
	for _, e := range r.snapshot() {
		if !e.At.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

// Latest returns the most recently added sample, or ok=false when the ring is
// empty. It is the live reading, so it answers regardless of age — deciding
// that a sample is too old to show is the caller's judgement, not the ring's.
func (r *Ring) Latest() (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		return Entry{}, false
	}
	last := (r.next - 1 + len(r.buf)) % len(r.buf)
	return r.buf[last].clone(), true
}
