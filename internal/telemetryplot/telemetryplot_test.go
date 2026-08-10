// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot_test

// telemetryplot_test.go — the three rules the package exists to enforce
// (time-placed points, gaps that break, quantities that are absent rather than
// zero), plus the degenerate inputs a live daemon can actually produce.

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/telemetryplot"
)

// now is a fixed reference so nothing here reads a clock.
var now = time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC)

// sampleAt builds a reading offsetSec seconds before now.
func sampleAt(offsetSec, temp int, rpm []int) api.TelemetrySample {
	return api.TelemetrySample{
		At:    now.Add(-time.Duration(offsetSec) * time.Second).Unix(),
		TempC: temp,
		RPM:   rpm,
	}
}

// find returns the series of a kind with the given label, or fails.
func find(t *testing.T, p telemetryplot.Plot, kind telemetryplot.Kind, label string) telemetryplot.Series {
	t.Helper()
	for _, s := range p.Series {
		if s.Kind == kind && (label == "" || s.Label == label) {
			return s
		}
	}
	t.Fatalf("no series of kind %v labelled %q; got %d series", kind, label, len(p.Series))
	return telemetryplot.Series{}
}

func has(p telemetryplot.Plot, kind telemetryplot.Kind) bool {
	for _, s := range p.Series {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestNothingToDraw(t *testing.T) {
	cases := []struct {
		name    string
		samples []api.TelemetrySample
		window  time.Duration
	}{
		{"no samples", nil, time.Minute},
		{"empty slice", []api.TelemetrySample{}, time.Minute},
		{"zero window", []api.TelemetrySample{sampleAt(1, 50, nil)}, 0},
		{"negative window", []api.TelemetrySample{sampleAt(1, 50, nil)}, -time.Minute},
		{"every sample older than the window", []api.TelemetrySample{sampleAt(9999, 50, nil)}, time.Minute},
		// A sample that carries nothing readable is not a data point. Without
		// this the chart draws a series for a machine that measured nothing.
		{"samples carry no readings", []api.TelemetrySample{{At: now.Unix()}}, time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := telemetryplot.Build(tc.samples, now, tc.window, 0)
			if !p.Empty() {
				t.Errorf("Empty() = false with %d series, want an empty plot", len(p.Series))
			}
		})
	}
}

// TestPointsArePlacedByTimestamp is the rule the package exists for. The daemon
// skips samples across a suspend and after a failed read, so the slice index
// says nothing about when a reading was taken.
func TestPointsArePlacedByTimestamp(t *testing.T) {
	// Three readings in a 100s window: 100s ago, 50s ago, and now. Placed by
	// index they would sit at 0, 0.5 and 1.0 — which happens to agree here.
	// Placed by index in the *gapped* case below they would not.
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(100, 40, nil),
		sampleAt(50, 60, nil),
		sampleAt(0, 80, nil),
	}, now, 100*time.Second, 0)

	s := find(t, p, telemetryplot.KindTemp, "")
	var xs []float64
	for _, seg := range s.Segments {
		for _, pt := range seg {
			xs = append(xs, pt.X)
		}
	}
	want := []float64{0, 0.5, 1}
	if len(xs) != len(want) {
		t.Fatalf("got %d points, want %d", len(xs), len(want))
	}
	for i := range want {
		if math.Abs(xs[i]-want[i]) > 1e-9 {
			t.Errorf("point %d at X=%v, want %v", i, xs[i], want[i])
		}
	}
}

// TestSuspendIsDrawnAsAGap covers the case an index-placed plot gets wrong: a
// ten-hour suspend between two readings that are adjacent in the slice.
func TestSuspendIsDrawnAsAGap(t *testing.T) {
	window := 12 * time.Hour
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(int((11 * time.Hour).Seconds()), 45, nil),
		sampleAt(int((11*time.Hour - time.Second).Seconds()), 46, nil),
		// ...machine suspended for ten hours...
		sampleAt(1, 50, nil),
		sampleAt(0, 51, nil),
	}, now, window, 0)

	s := find(t, p, telemetryplot.KindTemp, "")
	if len(s.Segments) != 2 {
		t.Fatalf("got %d segments, want 2 — the suspend must break the line", len(s.Segments))
	}
	if len(s.Segments[0]) != 2 || len(s.Segments[1]) != 2 {
		t.Fatalf("segment sizes %d and %d, want 2 and 2", len(s.Segments[0]), len(s.Segments[1]))
	}

	// The two halves must sit at opposite ends of the window. Placed by index
	// the four points would be evenly spread at 0, 1/3, 2/3, 1 — drawing the
	// suspend as though the machine had been running through it.
	pre := s.Segments[0][0].X
	post := s.Segments[1][0].X
	if pre > 0.1 {
		t.Errorf("pre-suspend point at X=%v, want near the left edge", pre)
	}
	if post < 0.9 {
		t.Errorf("post-resume point at X=%v, want near the right edge", post)
	}
}

// TestOneMissedReadingDoesNotFragmentTheLine is the other half of the gap rule:
// the threshold is generous on purpose, so a flaky sensor does not shatter the
// chart into single points.
func TestOneMissedReadingDoesNotFragmentTheLine(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(4, 50, nil),
		sampleAt(3, 51, nil),
		// 2s ago missing — one failed read
		sampleAt(1, 52, nil),
		sampleAt(0, 53, nil),
	}, now, time.Minute, 0)

	s := find(t, p, telemetryplot.KindTemp, "")
	if len(s.Segments) != 1 {
		t.Errorf("got %d segments, want 1 — a single dropped read is noise, not a gap", len(s.Segments))
	}
}

func TestGapThresholdIsHonoured(t *testing.T) {
	samples := []api.TelemetrySample{sampleAt(10, 50, nil), sampleAt(0, 60, nil)}

	// Exactly at the threshold is not a gap; strictly beyond it is.
	if got := len(telemetryplot.Build(samples, now, time.Minute, 10*time.Second).Series[0].Segments); got != 1 {
		t.Errorf("a gap exactly at maxGap produced %d segments, want 1", got)
	}
	if got := len(telemetryplot.Build(samples, now, time.Minute, 9*time.Second).Series[0].Segments); got != 2 {
		t.Errorf("a gap beyond maxGap produced %d segments, want 2", got)
	}
	// Zero means the default, which is well above a 10s gap... and below it.
	if telemetryplot.DefaultMaxGap >= 10*time.Second {
		t.Fatalf("test assumes DefaultMaxGap < 10s, got %v", telemetryplot.DefaultMaxGap)
	}
	if got := len(telemetryplot.Build(samples, now, time.Minute, 0).Series[0].Segments); got != 2 {
		t.Errorf("maxGap=0 produced %d segments, want the default's 2", got)
	}
}

// TestAQuantityNobodyMeasuredHasNoSeries is the capability-honesty rule at the
// plot level, and it is the live case on the Z13: the daemon reports zero
// package power because energy_uj is unreadable under the Platypus mitigation,
// and a flat line at 0 W looks like a measurement.
func TestAQuantityNobodyMeasuredHasNoSeries(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(2, 50, []int{2000}),
		sampleAt(1, 51, []int{2100}),
		sampleAt(0, 52, []int{2200}),
	}, now, time.Minute, 0)

	if has(p, telemetryplot.KindPower) {
		t.Error("a power series was built from samples that carry no power reading")
	}
	if !has(p, telemetryplot.KindTemp) || !has(p, telemetryplot.KindFan) {
		t.Error("temperature and fan series must still be built")
	}
}

// TestZeroMeansAbsentForTemperatureButNotForFans pins an asymmetry that looks
// like an inconsistency until you know why. Both fields are omitempty, so on
// the wire an absent reading and a zero one are identical — but a stopped fan
// genuinely reads 0 RPM, whereas a 0°C APU does not exist.
func TestZeroMeansAbsentForTemperatureButNotForFans(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		{At: now.Add(-2 * time.Second).Unix(), TempC: 0, RPM: []int{0}},
		{At: now.Add(-time.Second).Unix(), TempC: 0, RPM: []int{0}},
	}, now, time.Minute, 0)

	if has(p, telemetryplot.KindTemp) {
		t.Error("0°C was treated as a reading; it is how the wire spells 'absent'")
	}
	fan := find(t, p, telemetryplot.KindFan, "")
	if n := len(fan.Segments[0]); n != 2 {
		t.Errorf("stopped fan produced %d points, want 2 — 0 RPM is a measurement", n)
	}
}

func TestFanLabels(t *testing.T) {
	one := telemetryplot.Build([]api.TelemetrySample{sampleAt(0, 50, []int{2000})}, now, time.Minute, 0)
	if got := find(t, one, telemetryplot.KindFan, "").Label; got != "Fan" {
		t.Errorf("single fan labelled %q, want %q — a number implies a second one", got, "Fan")
	}

	two := telemetryplot.Build([]api.TelemetrySample{sampleAt(0, 50, []int{2000, 2100})}, now, time.Minute, 0)
	find(t, two, telemetryplot.KindFan, "Fan 1")
	find(t, two, telemetryplot.KindFan, "Fan 2")
}

// TestRaggedFanSlices covers a driver returning a shorter slice on a failed
// read: the second fan's series exists but skips that sample.
func TestRaggedFanSlices(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(3, 50, []int{2000, 2100}),
		sampleAt(2, 50, []int{2000}), // fan 2 unreadable this tick
		sampleAt(1, 50, []int{2000, 2200}),
	}, now, time.Minute, 0)

	f1 := find(t, p, telemetryplot.KindFan, "Fan 1")
	f2 := find(t, p, telemetryplot.KindFan, "Fan 2")
	if n := countPoints(f1); n != 3 {
		t.Errorf("Fan 1 has %d points, want 3", n)
	}
	if n := countPoints(f2); n != 2 {
		t.Errorf("Fan 2 has %d points, want 2 — the short sample carries no reading for it", n)
	}
}

func countPoints(s telemetryplot.Series) int {
	n := 0
	for _, seg := range s.Segments {
		n += len(seg)
	}
	return n
}

// TestFansShareOneAxis is why the frame is computed per kind rather than per
// series. Two fans are drawn on one chart, so separate axes would make their
// line heights incomparable — which is the one thing a viewer will use them for.
func TestFansShareOneAxis(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		// Fan 2 runs far faster, and past the nominal frame, so an axis fitted
		// per series would differ between them.
		sampleAt(2, 50, []int{1200, 8800}),
		sampleAt(1, 51, []int{1300, 8900}),
	}, now, time.Minute, 0)

	f1 := find(t, p, telemetryplot.KindFan, "Fan 1")
	f2 := find(t, p, telemetryplot.KindFan, "Fan 2")
	if f1.Bounds != f2.Bounds {
		t.Errorf("fan axes differ: %v vs %v; series drawn on one chart must share a frame",
			f1.Bounds, f2.Bounds)
	}
	// The shared frame must still contain the faster fan.
	if f1.Bounds.Max < 8900 {
		t.Errorf("shared axis tops out at %v, below the fastest reading", f1.Bounds.Max)
	}
	// ...and the slower fan must then sit low on it, which is the whole point.
	if y := f1.Segments[0][0].Y; y > 0.3 {
		t.Errorf("slow fan plotted at Y=%v on the shared axis, expected near the bottom", y)
	}
}

func TestGroupsAreCharts(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(2, 50, []int{1200, 1300}),
		sampleAt(1, 51, []int{1250, 1350}),
	}, now, time.Minute, 0)

	groups := p.Groups()
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2 (temperature and fans; no power on this device)", len(groups))
	}
	if groups[0].Kind != telemetryplot.KindTemp || len(groups[0].Series) != 1 {
		t.Errorf("group 0 = kind %v with %d series, want one temperature series",
			groups[0].Kind, len(groups[0].Series))
	}
	if groups[1].Kind != telemetryplot.KindFan || len(groups[1].Series) != 2 {
		t.Errorf("group 1 = kind %v with %d series, want both fans on one chart",
			groups[1].Kind, len(groups[1].Series))
	}
	for _, g := range groups {
		for _, s := range g.Series {
			if s.Bounds != g.Bounds {
				t.Errorf("%s: series bounds %v differ from its group's %v", s.Label, s.Bounds, g.Bounds)
			}
			if s.Unit != g.Unit {
				t.Errorf("%s: unit %q differs from its group's %q", s.Label, s.Unit, g.Unit)
			}
		}
	}
}

// TestShapeTracksTheChartsAndNotTheValues is the contract the drawer rebuilds
// on: values move every second, the chart layout almost never does.
func TestShapeTracksTheChartsAndNotTheValues(t *testing.T) {
	base := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(2, 50, []int{1200, 1300}),
	}, now, time.Minute, 0).Shape()

	same := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(2, 91, []int{4400, 4900}), // hotter, faster, same charts
	}, now, time.Minute, 0).Shape()
	if same != base {
		t.Errorf("shape changed with the values: %q then %q", base, same)
	}

	for _, tc := range []struct {
		name    string
		samples []api.TelemetrySample
	}{
		{"a fan stops being reported", []api.TelemetrySample{sampleAt(2, 50, []int{1200})}},
		{"temperature goes unreadable", []api.TelemetrySample{{At: now.Add(-2 * time.Second).Unix(), RPM: []int{1200, 1300}}}},
		{"a power source appears", []api.TelemetrySample{{
			At: now.Add(-2 * time.Second).Unix(), TempC: 50, RPM: []int{1200, 1300}, PackagePowerW: 28,
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := telemetryplot.Build(tc.samples, now, time.Minute, 0).Shape(); got == base {
				t.Errorf("shape %q unchanged; the chart layout differs and must be rebuilt", got)
			}
		})
	}

	if got := (telemetryplot.Plot{}).Shape(); got != "" {
		t.Errorf("empty plot shape = %q, want the empty string", got)
	}
}

func TestGroupsOfAnEmptyPlot(t *testing.T) {
	if g := (telemetryplot.Plot{}).Groups(); len(g) != 0 {
		t.Errorf("empty plot produced %d groups, want none", len(g))
	}
}

// TestTheAxisNeverClipsAReading is the invariant that makes it safe for the
// nominal frames to carry one laptop's numbers: they are a starting frame, and
// the data always wins.
func TestTheAxisNeverClipsAReading(t *testing.T) {
	cases := []struct {
		name    string
		samples []api.TelemetrySample
	}{
		{"inside the nominal frame", []api.TelemetrySample{sampleAt(1, 55, []int{3000})}},
		{"hotter than the frame", []api.TelemetrySample{sampleAt(1, 118, []int{3000})}},
		{"colder than the frame", []api.TelemetrySample{sampleAt(1, 12, []int{3000})}},
		{"faster than the frame", []api.TelemetrySample{sampleAt(1, 50, []int{9500})}},
		{"a wide sweep", []api.TelemetrySample{
			sampleAt(3, 12, []int{0}),
			sampleAt(2, 60, []int{4000}),
			sampleAt(1, 118, []int{9500}),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := telemetryplot.Build(tc.samples, now, time.Minute, 0)
			for _, s := range p.Series {
				if s.Bounds.Span() <= 0 {
					t.Fatalf("%s: span %v, want positive", s.Label, s.Bounds.Span())
				}
				for _, seg := range s.Segments {
					for _, pt := range seg {
						if pt.V < s.Bounds.Min || pt.V > s.Bounds.Max {
							t.Errorf("%s: value %v outside bounds %v..%v",
								s.Label, pt.V, s.Bounds.Min, s.Bounds.Max)
						}
						if pt.Y < 0 || pt.Y > 1 {
							t.Errorf("%s: Y=%v outside 0..1", s.Label, pt.Y)
						}
						if pt.X < 0 || pt.X > 1 {
							t.Errorf("%s: X=%v outside 0..1", s.Label, pt.X)
						}
					}
				}
			}
		})
	}
}

// TestTheAxisIsSteadyWhileValuesWander is why the frame exists at all: an axis
// that rescales to the data every second is unreadable, and the drawer refreshes
// this chart at 1 Hz.
func TestTheAxisIsSteadyWhileValuesWander(t *testing.T) {
	var prev telemetryplot.Bounds
	for i, temp := range []int{42, 61, 55, 78, 49, 88} {
		p := telemetryplot.Build([]api.TelemetrySample{sampleAt(1, temp, nil)}, now, time.Minute, 0)
		b := find(t, p, telemetryplot.KindTemp, "").Bounds
		if i > 0 && b != prev {
			t.Errorf("axis moved from %v to %v at %d°C; values inside the frame must not rescale it",
				prev, b, temp)
		}
		prev = b
	}
}

// TestFlatSeriesHasADivisibleSpan — the caller divides by Span without checking.
func TestFlatSeriesHasADivisibleSpan(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(2, 50, []int{2000}),
		sampleAt(1, 50, []int{2000}),
	}, now, time.Minute, 0)
	for _, s := range p.Series {
		if s.Bounds.Span() <= 0 {
			t.Errorf("%s: flat series has span %v", s.Label, s.Bounds.Span())
		}
	}
}

// TestOutOfOrderSamplesAreSorted — the daemon's ring stamps with wall clock,
// which a clock step can move backwards, so arrival order is not time order.
func TestOutOfOrderSamplesAreSorted(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(1, 53, nil),
		sampleAt(3, 51, nil),
		sampleAt(2, 52, nil),
	}, now, time.Minute, 0)

	s := find(t, p, telemetryplot.KindTemp, "")
	var last float64 = -1
	for _, seg := range s.Segments {
		for _, pt := range seg {
			if pt.X < last {
				t.Fatalf("points are not in time order: %v after %v", pt.X, last)
			}
			last = pt.X
		}
	}
	if s.Latest != 53 {
		t.Errorf("Latest = %v, want the most recent reading (53)", s.Latest)
	}
}

func TestWindowEdges(t *testing.T) {
	// Exactly at the window's start is inside it; a second older is not.
	in := telemetryplot.Build([]api.TelemetrySample{sampleAt(60, 50, nil)}, now, time.Minute, 0)
	if in.Empty() {
		t.Error("a sample exactly at the window's start was dropped")
	}
	out := telemetryplot.Build([]api.TelemetrySample{sampleAt(61, 50, nil)}, now, time.Minute, 0)
	if !out.Empty() {
		t.Error("a sample older than the window was kept")
	}

	// A reading newer than now — sub-second clock skew between the daemon's
	// stamp and the client's read — is clamped to the edge, not discarded.
	// Dropping it makes a live chart look stalled.
	ahead := telemetryplot.Build([]api.TelemetrySample{
		sampleAt(5, 49, nil),
		sampleAt(-2, 50, nil),
	}, now, time.Minute, 0)
	s := find(t, ahead, telemetryplot.KindTemp, "")
	if n := countPoints(s); n != 2 {
		t.Fatalf("got %d points, want 2 — a slightly-future reading must be kept", n)
	}
	last := s.Segments[len(s.Segments)-1]
	if x := last[len(last)-1].X; x != 1 {
		t.Errorf("future reading placed at X=%v, want it clamped to 1", x)
	}
}

func TestPlotCarriesItsDomain(t *testing.T) {
	p := telemetryplot.Build([]api.TelemetrySample{sampleAt(1, 50, nil)}, now, 5*time.Minute, 0)
	if !p.End.Equal(now) {
		t.Errorf("End = %v, want %v", p.End, now)
	}
	if want := now.Add(-5 * time.Minute); !p.Start.Equal(want) {
		t.Errorf("Start = %v, want %v", p.Start, want)
	}
}

// TestBuildDoesNotMutateItsInput — Build sorts, and the caller's slice is the
// api response it may still be reading.
func TestBuildDoesNotMutateItsInput(t *testing.T) {
	in := []api.TelemetrySample{sampleAt(1, 53, nil), sampleAt(3, 51, nil), sampleAt(2, 52, nil)}
	before := append([]api.TelemetrySample(nil), in...)
	telemetryplot.Build(in, now, time.Minute, 0)
	if !reflect.DeepEqual(in, before) {
		t.Fatalf("Build reordered the caller's slice:\n got %+v\nwant %+v", in, before)
	}
}
