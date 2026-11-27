package coalesce

import (
	"sync"
	"sync/atomic"
	"testing"
)

// A request during a run gets exactly one more run, after it.
func TestRequestDuringARunRunsOnceMore(t *testing.T) {
	var r Runner
	var runs atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		r.Do(func() {
			if runs.Add(1) == 1 {
				close(started)
				<-release
			}
		})
		close(done)
	}()
	<-started
	for range 5 { // a burst while the first run is in flight
		r.Do(func() { runs.Add(1) })
	}
	close(release)
	<-done
	if got := runs.Load(); got != 2 {
		t.Errorf("runs = %d, want 2 (the first, and one for the whole burst)", got)
	}
}

// Runs never overlap.
func TestRunsNeverOverlap(t *testing.T) {
	var r Runner
	var inFlight, maxInFlight atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Do(func() {
				n := inFlight.Add(1)
				for {
					m := maxInFlight.Load()
					if n <= m || maxInFlight.CompareAndSwap(m, n) {
						break
					}
				}
				inFlight.Add(-1)
			})
		}()
	}
	wg.Wait()
	if maxInFlight.Load() != 1 {
		t.Errorf("max runs in flight = %d, want 1", maxInFlight.Load())
	}
}

// An idle runner runs immediately, on the caller.
func TestIdleRunsAtOnce(t *testing.T) {
	var r Runner
	ran := false
	r.Do(func() { ran = true })
	if !ran {
		t.Error("Do on an idle runner did not run the job")
	}
}
