// Package coalesce runs a job at most once at a time, folding every request
// that arrives while it runs into one more run afterwards.
//
// The GUI's state refresh is the user: a write ends in a refresh, and the
// daemon's state-changed broadcast triggers another, so requests overlap.
// Run in parallel, two get-state replies can land out of order and walk the
// widgets back to an older state; run once per request, a burst repeats work
// nobody will see. A request is never lost, though: one arriving mid-run gets
// a run that starts after it, so the last caller always sees state at least as
// new as its own write.
package coalesce

import "sync"

// Runner serialises and coalesces calls to Do. The zero value is ready.
type Runner struct {
	mu    sync.Mutex
	busy  bool
	again bool
}

// Do runs job unless a run is in progress, in which case it records that
// another run is wanted and returns at once; the running caller then runs job
// again before it returns. job runs on the goroutine of whichever caller
// started the run.
func (r *Runner) Do(job func()) {
	r.mu.Lock()
	if r.busy {
		r.again = true
		r.mu.Unlock()
		return
	}
	r.busy = true
	r.mu.Unlock()
	for {
		job()
		r.mu.Lock()
		if !r.again {
			r.busy = false
			r.mu.Unlock()
			return
		}
		r.again = false
		r.mu.Unlock()
	}
}
