// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryring_test

import (
	"sync"
	"testing"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/telemetryring"
)

// base is a fixed instant so nothing here reads a clock.
var base = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }

func sample(temp int) driver.Sample {
	return driver.Sample{TempC: temp, RPM: []int{temp * 10, temp * 20}}
}

// temps flattens entries to their temperatures, which is enough to identify
// which samples came back and in what order.
func temps(entries []telemetryring.Entry) []int {
	out := make([]int, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Sample.TempC)
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestZeroRingRetainsNothing(t *testing.T) {
	t.Parallel()
	// The zero value and New(<=0) are the same thing: a device that declares no
	// telemetry history. Every method must answer rather than panic, so no
	// caller needs a nil check around a ring it was handed.
	for name, r := range map[string]*telemetryring.Ring{
		"zero value":       {},
		"New(0)":           telemetryring.New(0),
		"New(-1)":          telemetryring.New(-1),
		"New(math.MinInt)": telemetryring.New(-1 << 62),
	} {
		r.Add(at(0), sample(50))
		r.Add(at(1), sample(51))
		switch {
		case r.Len() != 0:
			t.Errorf("%s: Len = %d, want 0", name, r.Len())
		case r.Cap() != 0:
			t.Errorf("%s: Cap = %d, want 0", name, r.Cap())
		case r.All() != nil:
			t.Errorf("%s: All = %v, want nil", name, r.All())
		case r.Since(at(1), time.Minute) != nil:
			t.Errorf("%s: Since = %v, want nil", name, r.Since(at(1), time.Minute))
		}
		if _, ok := r.Latest(); ok {
			t.Errorf("%s: Latest reported a sample", name)
		}
	}
}

func TestWrapsAndReportsOldestFirst(t *testing.T) {
	t.Parallel()
	r := telemetryring.New(3)

	// Filling exactly to capacity, then past it.
	for i, temp := range []int{40, 41, 42, 43, 44} {
		r.Add(at(i), sample(temp))
	}
	if got, want := temps(r.All()), []int{42, 43, 44}; !equal(got, want) {
		t.Errorf("All after wrap = %v, want %v", got, want)
	}
	if r.Len() != 3 || r.Cap() != 3 {
		t.Errorf("Len/Cap = %d/%d, want 3/3", r.Len(), r.Cap())
	}
	// Latest must follow the write cursor across the wrap, not the slice end.
	last, ok := r.Latest()
	if !ok || last.Sample.TempC != 44 || !last.At.Equal(at(4)) {
		t.Errorf("Latest = %+v (ok=%v), want the 44 sample at t+4", last, ok)
	}
}

func TestPartiallyFilledRingReportsOnlyWhatItHolds(t *testing.T) {
	t.Parallel()
	r := telemetryring.New(300)
	r.Add(at(0), sample(40))
	r.Add(at(1), sample(41))

	if got, want := temps(r.All()), []int{40, 41}; !equal(got, want) {
		t.Errorf("All = %v, want %v", got, want)
	}
	if r.Len() != 2 || r.Cap() != 300 {
		t.Errorf("Len/Cap = %d/%d, want 2/300", r.Len(), r.Cap())
	}
}

func TestAddCopiesTheSample(t *testing.T) {
	t.Parallel()
	// driver.Sample carries an RPM slice and a telemetry driver is free to
	// reuse its buffer between reads. A plain struct copy would alias it and
	// the whole history would show the newest fan speeds.
	rpm := []int{1000, 2000}
	r := telemetryring.New(2)
	r.Add(at(0), driver.Sample{TempC: 50, RPM: rpm})

	rpm[0] = 9999

	got := r.All()
	if len(got) != 1 || got[0].Sample.RPM[0] != 1000 {
		t.Fatalf("ring saw the caller's later write: %v", got)
	}
}

func TestReadsReturnCopies(t *testing.T) {
	t.Parallel()
	// The mirror of the above: a client mutating what it was handed must not be
	// editing the daemon's history.
	r := telemetryring.New(2)
	r.Add(at(0), sample(50))

	all := r.All()
	all[0].Sample.RPM[0] = 9999
	all[0].Sample.TempC = 9999

	latest, _ := r.Latest()
	if latest.Sample.RPM[0] == 9999 || latest.Sample.TempC == 9999 {
		t.Errorf("mutating All's result reached the ring: %+v", latest)
	}

	since := r.Since(at(0), time.Minute)
	since[0].Sample.RPM[0] = 8888
	if again := r.Since(at(0), time.Minute); again[0].Sample.RPM[0] == 8888 {
		t.Error("mutating Since's result reached the ring")
	}
}

func TestSinceWindow(t *testing.T) {
	t.Parallel()
	r := telemetryring.New(10)
	for i := range 6 {
		r.Add(at(i), sample(40+i)) // t+0 … t+5
	}
	now := at(5)

	cases := []struct {
		name string
		d    time.Duration
		want []int
	}{
		// The boundary is inclusive, so a 5s window over second-spaced samples
		// holds six of them, not five: that is what makes a 300-slot 1 Hz ring
		// answer "the last 300 seconds" with all 300 samples.
		{"exactly spans the ring", 5 * time.Second, []int{40, 41, 42, 43, 44, 45}},
		{"partial window", 2 * time.Second, []int{43, 44, 45}},
		{"newest only", time.Nanosecond, []int{45}},
		{"wider than the history", time.Hour, []int{40, 41, 42, 43, 44, 45}},
		// A caller that failed to parse its "seconds" field asks for zero and
		// must not be handed the whole ring by accident.
		{"zero", 0, nil},
		{"negative", -time.Second, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := temps(r.Since(now, tc.d)); !equal(got, tc.want) {
				t.Errorf("Since(%v) = %v, want %v", tc.d, got, tc.want)
			}
		})
	}
}

func TestSuspendGapFallsOutOfTheWindow(t *testing.T) {
	t.Parallel()
	// The sampler stands down while d.suspending is set, so the ring's contents
	// straddle the suspend with no samples in between. Filtering on wall-clock
	// time is what makes the pre-suspend half age out normally; a monotonic
	// clock would not have advanced and they would sit in the window forever.
	r := telemetryring.New(10)
	r.Add(at(0), sample(40))
	r.Add(at(1), sample(41))
	r.Add(at(36000), sample(42)) // ten hours asleep
	r.Add(at(36001), sample(43))

	if got, want := temps(r.Since(at(36001), 5*time.Minute)), []int{42, 43}; !equal(got, want) {
		t.Errorf("after resume Since = %v, want %v", got, want)
	}
	if r.Len() != 4 {
		t.Errorf("Len = %d, want 4 — the window filters, it does not evict", r.Len())
	}
}

func TestBackwardsClockKeepsInsertionOrder(t *testing.T) {
	t.Parallel()
	// An NTP step backwards mid-history. The ring must not panic, must not
	// re-sort, and must not drop what it was not asked to drop — but a sample
	// stamped before the cutoff is outside the window even in the middle of the
	// slice, and that is the honest answer rather than a repaired one.
	r := telemetryring.New(10)
	r.Add(at(100), sample(40))
	r.Add(at(30), sample(41)) // clock stepped back
	r.Add(at(101), sample(42))

	if got, want := temps(r.All()), []int{40, 41, 42}; !equal(got, want) {
		t.Errorf("All = %v, want insertion order %v", got, want)
	}
	if got, want := temps(r.Since(at(101), 10*time.Second)), []int{40, 42}; !equal(got, want) {
		t.Errorf("Since = %v, want %v (the stepped sample is outside the window)", got, want)
	}
	if last, ok := r.Latest(); !ok || last.Sample.TempC != 42 {
		t.Errorf("Latest = %+v, want the last-added sample regardless of its stamp", last)
	}
}

func TestConcurrentAddAndRead(t *testing.T) {
	// Under -race this is the whole point of the internal lock: the 1 Hz
	// sampler goroutine writes while socket handlers read.
	r := telemetryring.New(64)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 500 {
			r.Add(at(i), sample(40+i%10))
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 500 {
			_ = r.All()
			_ = r.Since(at(i), time.Minute)
			_, _ = r.Latest()
			_ = r.Len()
		}
	}()
	wg.Wait()

	if r.Len() != 64 {
		t.Errorf("Len = %d, want 64", r.Len())
	}
}
