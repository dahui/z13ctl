// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot

// span.go — the history windows a dashboard offers, and how often each is
// worth redrawing.
//
// Both were GTK-side literals: a hardcoded list of spans and a hardcoded 1 Hz
// refresh. Neither survives a longer window (Jeff, 2026-08-14: "I imagine
// people may want to see longer history"), because a chart's cost and its
// usefulness both scale with how much it is showing, and a per-second refetch
// of an hour of samples is a laptop tool spending real power to redraw a
// picture that has not changed.

import (
	"fmt"
	"time"
)

// Span is one history window a dashboard can show.
type Span struct {
	Label string
	D     time.Duration
}

// Spans are the windows to offer, shortest first.
//
// The list stops at an hour, and the reason is not that longer is unhelpful —
// it is that the ring is in memory. A window voltaire cannot fill after a
// daemon restart is a promise it breaks every upgrade, and a chart that
// silently covers "since the daemon started" rather than the span on its button
// would be the flat-at-zero dishonesty one layer up. Anything genuinely
// long-term wants a store on disk, which is a different feature.
//
// Callers filter this against what the device retains — see Offered.
func Spans() []Span {
	return []Span{
		{"1m", time.Minute},
		{"5m", 5 * time.Minute},
		{"15m", 15 * time.Minute},
		{"30m", 30 * time.Minute},
		{"1h", time.Hour},
	}
}

// Offered returns the spans worth showing for a device that retains the given
// amount of history.
//
// A span longer than the ring could only ever draw a part-filled chart, so it
// is dropped — but the shortest is always kept, because a device with a very
// short ring would otherwise present an empty button strip, which reads as a
// broken control rather than as a limit. Retention of zero or less means
// nothing is known (no daemon, no document), and the caller gets the shortest
// span alone rather than a list it cannot honour.
func Offered(retained time.Duration) []Span {
	all := Spans()
	out := make([]Span, 0, len(all))
	for _, s := range all {
		if s.D > retained && len(out) > 0 {
			break
		}
		out = append(out, s)
	}
	return out
}

// refreshPixels is the number of distinct horizontal positions a chart is
// assumed to resolve — near enough one card's width in the window's default
// layout, where eight tiles sit four to a row.
//
// It is what turns "how long is the window" into "how often is a redraw worth
// anything": a new sample that cannot move the trace by a pixel is a redraw
// that draws the same picture.
const refreshPixels = 300

// minRefresh is the floor. The sampler runs at 1 Hz, so nothing is gained by
// asking more often, and the short spans people watch live want every sample.
const minRefresh = time.Second

// RefreshInterval is how often a chart of this span is worth refetching and
// redrawing.
//
// At a minute the answer is every second — each sample is several pixels of
// trace. At an hour it is every twelve, because 3600 samples across a card is a
// dozen readings per pixel and eleven of every twelve refreshes would redraw an
// identical chart. That is not a cosmetic saving: telemetry-history is ~440
// bytes per sample on the wire, so an hour is about 1.5 MiB *per poll*, and a
// power-management tool spending 1.5 MB/s of socket traffic and JSON decoding
// to animate a still image would be defeating its own purpose.
//
// Only the trace slows down. The card headers' live numbers come from the
// separate 1 Hz get-state poll, which every view runs and this does not touch,
// so the readouts stay current at any span.
func RefreshInterval(span time.Duration) time.Duration {
	if span <= 0 {
		return minRefresh
	}
	every := span / refreshPixels
	if every < minRefresh {
		return minRefresh
	}
	return every.Round(time.Second)
}

// SpanHint is the help text for one span's button.
func SpanHint(s Span) string {
	return fmt.Sprintf("Show the last %s of telemetry", s.Label)
}
