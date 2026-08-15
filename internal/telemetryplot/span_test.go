// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot_test

import (
	"testing"
	"time"

	"github.com/dahui/voltaire/v2/internal/telemetryplot"
)

func TestSpansAreOrderedAndLabelled(t *testing.T) {
	t.Parallel()

	spans := telemetryplot.Spans()
	if len(spans) < 2 {
		t.Fatalf("spans = %d, want a choice", len(spans))
	}
	for i, s := range spans {
		if s.Label == "" {
			t.Errorf("span %d has no label", i)
		}
		if s.D <= 0 {
			t.Errorf("span %s has duration %v", s.Label, s.D)
		}
		if i > 0 && s.D <= spans[i-1].D {
			t.Errorf("span %s (%v) does not follow %s (%v); the list must be shortest first",
				s.Label, s.D, spans[i-1].Label, spans[i-1].D)
		}
	}
	// The ring is in memory, so a window voltaire cannot fill after a daemon
	// restart is a promise it breaks on every upgrade.
	if last := spans[len(spans)-1].D; last > time.Hour {
		t.Errorf("longest span is %v; anything past an hour wants a store on disk", last)
	}
}

func TestOffered(t *testing.T) {
	t.Parallel()

	labels := func(spans []telemetryplot.Span) []string {
		out := make([]string, 0, len(spans))
		for _, s := range spans {
			out = append(out, s.Label)
		}
		return out
	}

	cases := []struct {
		name     string
		retained time.Duration
		want     int // how many spans are offered
	}{
		{"an hour of history offers everything", time.Hour, len(telemetryplot.Spans())},
		{"five minutes offers the two that fit", 5 * time.Minute, 2},
		{"thirty minutes stops before the hour", 30 * time.Minute, 4},
		// Never an empty strip: a row of no buttons reads as a broken control
		// rather than as a device that keeps very little.
		{"ten seconds still offers the shortest", 10 * time.Second, 1},
		{"nothing known offers the shortest", 0, 1},
		{"a negative is not a trap", -time.Hour, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := telemetryplot.Offered(tc.retained)
			if len(got) != tc.want {
				t.Errorf("Offered(%v) = %v, want %d spans", tc.retained, labels(got), tc.want)
			}
			// Whatever is offered has to be a prefix of the full list, or the
			// selector would show a gap.
			all := telemetryplot.Spans()
			for i, s := range got {
				if s != all[i] {
					t.Errorf("offered[%d] = %s, want %s — the list must stay a prefix",
						i, s.Label, all[i].Label)
				}
			}
		})
	}
}

// A span is never offered that the device cannot fill, which is the property
// that keeps a part-filled chart off the screen.
func TestOfferedNeverExceedsRetention(t *testing.T) {
	t.Parallel()

	for _, retained := range []time.Duration{
		time.Minute, 5 * time.Minute, 17 * time.Minute, time.Hour, 6 * time.Hour,
	} {
		got := telemetryplot.Offered(retained)
		for i, s := range got {
			// The shortest is the deliberate exception — see Offered.
			if i > 0 && s.D > retained {
				t.Errorf("Offered(%v) includes %s, which the device cannot fill", retained, s.Label)
			}
		}
	}
}

func TestRefreshInterval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		span time.Duration
		want time.Duration
	}{
		// The sampler runs at 1 Hz, so nothing is gained by asking faster —
		// and the short spans people watch live want every sample.
		{time.Minute, time.Second},
		{5 * time.Minute, time.Second},
		{15 * time.Minute, 3 * time.Second},
		{30 * time.Minute, 6 * time.Second},
		{time.Hour, 12 * time.Second},
		// Never zero, never negative: this becomes a timer interval.
		{0, time.Second},
		{-time.Hour, time.Second},
	}
	for _, tc := range cases {
		if got := telemetryplot.RefreshInterval(tc.span); got != tc.want {
			t.Errorf("RefreshInterval(%v) = %v, want %v", tc.span, got, tc.want)
		}
	}
}

// The whole point of the scaling: the per-poll cost of a window must not grow
// with the window. telemetry-history runs about 440 bytes per sample, so an
// hour is ~1.5 MiB — refetched every second that would be 1.5 MB/s to animate
// a chart that changes by less than a pixel.
func TestRefreshCostDoesNotGrowWithTheSpan(t *testing.T) {
	t.Parallel()

	const bytesPerSample = 440
	perSecond := func(s telemetryplot.Span) float64 {
		samples := s.D.Seconds() // the sampler runs at 1 Hz
		return samples * bytesPerSample / telemetryplot.RefreshInterval(s.D).Seconds()
	}

	// The ceiling is five minutes of samples once a second — the window the
	// dashboard was originally specified against, and the most it has ever
	// cost. It is a *fixed* reference on purpose: deriving it from
	// RefreshInterval was the first attempt and passes vacuously with the
	// scaling deleted, because the ceiling then moves with the bug. The
	// negative control is the only thing that caught that.
	const ceiling = 300 * bytesPerSample

	for _, s := range telemetryplot.Spans() {
		cost := perSecond(s)
		t.Logf("%-4s %6.0f samples  %7.0f KiB/poll  %6.0f KiB/s",
			s.Label, s.D.Seconds(), s.D.Seconds()*bytesPerSample/1024, cost/1024)
		// A percent of slack for the interval's rounding to whole seconds.
		if cost > ceiling*1.01 {
			t.Errorf("%s costs %.0f KiB/s, more than the %.0f KiB/s a 5m window "+
				"costs at 1 Hz — a longer window must not cost more per second",
				s.Label, cost/1024, float64(ceiling)/1024)
		}
	}
}
