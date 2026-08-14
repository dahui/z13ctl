// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package telemetryplot turns the daemon's sample history into plottable
// series: which points exist, where they sit in the window, where the line
// breaks, and what the y-axis spans.
//
// It is a separate package because internal/gui needs cgo and GTK4 headers, so
// `make test` cannot even compile it; anything left in there is permanently
// unverifiable. The GTK side walks the returned segments and multiplies by the
// chart's pixel size — every decision is made here. Mirrors the panelgeom and
// popupgeom splits.
//
// Three rules drive the whole package, and each exists because the obvious
// alternative draws something false:
//
//   - Points are placed by timestamp, never by slice index. Samples are not
//     evenly spaced — the daemon's sampler stands down across a suspend and
//     skips a failed read rather than recording a zero — so an index-placed
//     plot plots a ten-hour suspend as if no time passed.
//   - A gap breaks the line. Interpolating across one draws a smooth ramp
//     through hours nobody measured.
//   - A quantity no sample carries produces no series at all. Drawing it flat
//     at zero looks like a measurement, which is worse than showing nothing —
//     the same reason the device document declares no power source on a machine
//     whose Sample() cannot read one.
package telemetryplot

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dahui/voltaire/api/v2"
)

// DefaultMaxGap is how far apart two consecutive readings may be before the
// line between them is broken.
//
// The daemon samples at 1 Hz, so this is four consecutive misses. It is
// deliberately generous: the gap worth drawing is a suspend, which lasts
// minutes to hours, whereas a single failed sysfs read is noise. A threshold
// tight enough to catch one dropped reading would fragment the line on any
// machine with a flaky sensor and say nothing useful.
const DefaultMaxGap = 5 * time.Second

// Kind identifies what a series measures. The GTK side keys its colour and
// stacking order off this rather than off the label, which is prose.
type Kind int

// The quantities the daemon's telemetry sample can carry.
const (
	KindTemp Kind = iota
	KindFan
	KindPower
	KindBattery
)

// Point is one reading placed in the plot.
//
// X and Y are fractions rather than pixels so the caller can scale them to any
// chart size, including gamescope's, without this package knowing anything
// about the widget. V is kept alongside them because a readout label needs the
// number, and recovering it from Y would mean the caller re-deriving the axis.
type Point struct {
	X  float64   // 0 at the window's start, 1 at its end
	Y  float64   // 0 at Bounds.Min, 1 at Bounds.Max
	V  float64   // the raw value, in the series' unit
	At time.Time // the reading's own timestamp
}

// Bounds is a series' y-axis span. Min is always below Max by at least one
// step, so a caller may divide by the span without checking.
type Bounds struct {
	Min, Max float64
}

// Span is the axis height. It is never zero.
func (b Bounds) Span() float64 { return b.Max - b.Min }

// Series is one plotted quantity. Segments holds contiguous runs of readings;
// a gap longer than maxGap starts a new one, and the caller draws each segment
// as its own polyline rather than joining them.
//
// A series is only ever built when at least one sample carries the quantity, so
// an empty Segments slice cannot occur — see the package doc.
type Series struct {
	Kind     Kind
	Label    string
	Unit     string
	Bounds   Bounds
	Segments [][]Point

	// Latest is the most recent reading in the window, for a header readout.
	Latest float64
}

// Plot is the whole chart: a time domain and the series inside it.
type Plot struct {
	Start, End time.Time
	Series     []Series
}

// Empty reports whether there is nothing to draw. A caller should hide the
// chart entirely rather than render empty axes, which read as "the machine
// measured nothing" instead of "no history yet".
func (p Plot) Empty() bool { return len(p.Series) == 0 }

// axis describes one quantity's presentation: its label, its unit, and the
// frame its y-axis starts from.
//
// nomMin/nomMax are a *starting frame*, never a clamp. They keep the axis
// steady while values wander inside the range a machine normally occupies —
// an axis rescaling every second is unreadable — and they expand to fit
// anything outside it, so no reading is ever cut off. That invariant is what
// makes it safe to carry numbers shaped like one laptop's in a package meant to
// serve every device: the frame is a starting guess, and the data always wins.
type axis struct {
	label          string
	unit           string
	nomMin, nomMax float64
	step           float64
}

var axes = map[Kind]axis{
	KindTemp:  {label: "APU", unit: "°C", nomMin: 30, nomMax: 100, step: 10},
	KindFan:   {label: "Fan", unit: "RPM", nomMin: 0, nomMax: 6000, step: 1000},
	KindPower: {label: "Package", unit: "W", nomMin: 0, nomMax: 60, step: 10},
	// Battery flow is signed: discharging is positive, charging negative, so
	// the nominal frame straddles zero. It is a chart of its own rather than a
	// second series on the package chart, because the two answer different
	// questions — how hard the APU is working, and which way the pack is
	// moving — and sharing an axis would squash both.
	KindBattery: {label: "Battery", unit: "W", nomMin: -30, nomMax: 30, step: 10},
}

// bounds frames v's observed range: the nominal window, expanded outward to a
// step boundary wherever the data falls outside it.
func (a axis) bounds(lo, hi float64) Bounds {
	step := a.step
	if step <= 0 {
		step = 1
	}
	minV, maxV := a.nomMin, a.nomMax
	if lo < minV {
		minV = math.Floor(lo/step) * step
	}
	if hi > maxV {
		maxV = math.Ceil(hi/step) * step
	}
	if maxV-minV < step {
		// A degenerate nominal frame (or one a caller narrowed) must still
		// leave a divisible span; without this a flat series divides by zero.
		maxV = minV + step
	}
	return Bounds{Min: minV, Max: maxV}
}

// reading is one sample's contribution to one series, before placement.
type reading struct {
	at time.Time
	v  float64
}

// Build turns a history window into a plot.
//
// now is the right-hand edge of the domain — wall clock, matching the
// timestamps the daemon records. Readings older than the window are dropped.
// One newer than now is kept and clamped to the edge rather than discarded: the
// skew between the daemon's clock read and the client's is sub-second, and
// dropping the newest reading over it would make a live chart look stalled.
//
// maxGap of zero or less means DefaultMaxGap; a caller that genuinely wants
// every reading joined can pass a very large one, but nothing does.
func Build(samples []api.TelemetrySample, now time.Time, window, maxGap time.Duration) Plot {
	if window <= 0 || len(samples) == 0 {
		return Plot{}
	}
	if maxGap <= 0 {
		maxGap = DefaultMaxGap
	}
	start := now.Add(-window)
	p := Plot{Start: start, End: now}

	// Sort defensively. The daemon's ring keeps insertion order and stamps with
	// wall clock, which a clock step can move backwards, so arrival order is not
	// guaranteed to be time order. A line that zigzags backwards through the
	// window is worse than one drawn in the order the readings claim.
	sorted := append([]api.TelemetrySample(nil), samples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At < sorted[j].At })

	// How many fans any sample reports. Samples may disagree — a driver is free
	// to return a shorter slice on a failed read — so the widest wins and the
	// per-sample bounds check below covers the rest.
	fans := 0
	for _, s := range sorted {
		if len(s.RPM) > fans {
			fans = len(s.RPM)
		}
	}

	// Specs in drawing order. Every series of a kind is emitted consecutively,
	// which is what lets Groups() find a chart's members by walking runs.
	specs := []spec{{kind: KindTemp, value: func(s api.TelemetrySample) (float64, bool) {
		// TempC is omitempty, so an absent reading and a zero one are the same
		// on the wire. 0°C is not a plausible APU temperature, so it reads as
		// absent — which is the honest way round: a missing point leaves a gap,
		// a zeroed one draws a cliff to the floor.
		return float64(s.TempC), s.TempC != 0
	}}}

	for i := range fans {
		label := ""
		if fans > 1 {
			label = fanLabel(i)
		}
		specs = append(specs, spec{kind: KindFan, label: label,
			value: func(s api.TelemetrySample) (float64, bool) {
				// A stopped fan genuinely reads zero, so unlike temperature the
				// value cannot be the presence test. The slice is omitempty, so
				// presence is whether this sample carried an entry at all.
				if i >= len(s.RPM) {
					return 0, false
				}
				return float64(s.RPM[i]), true
			}})
	}

	specs = append(specs, spec{kind: KindPower, value: func(s api.TelemetrySample) (float64, bool) {
		// As with temperature: a device that cannot read package power omits the
		// field, and a machine drawing exactly 0 W is not a real state.
		return s.PackagePowerW, s.PackagePowerW != 0
	}})

	specs = append(specs, spec{kind: KindBattery, value: func(s api.TelemetrySample) (float64, bool) {
		// The one quantity whose zero is a reading — a full pack on mains moves
		// no energy — which is why the wire field is a pointer and presence is
		// the pointer, not the value. Testing the value instead would drop the
		// chart on every laptop sitting at 100%, and testing nothing would draw
		// one flat at zero on a desktop with no pack.
		if s.BatteryPowerW == nil {
			return 0, false
		}
		return *s.BatteryPowerW, true
	}})

	// Gather first, frame second. The axis is computed per *kind*, over every
	// series of that kind at once, because a chart draws them together: two fans
	// on separate axes are two lines whose heights cannot be compared, which is
	// the one thing a viewer will try to do with them.
	type gathered struct {
		spec     spec
		readings []reading
	}
	var live []gathered
	extent := map[Kind][2]float64{}
	for _, sp := range specs {
		rs := gather(sorted, start, sp.value)
		if len(rs) == 0 {
			continue
		}
		e, seen := extent[sp.kind]
		for i, r := range rs {
			if !seen && i == 0 {
				e = [2]float64{r.v, r.v}
				seen = true
				continue
			}
			e[0] = math.Min(e[0], r.v)
			e[1] = math.Max(e[1], r.v)
		}
		extent[sp.kind] = e
		live = append(live, gathered{spec: sp, readings: rs})
	}

	for _, g := range live {
		a := axes[g.spec.kind]
		label := g.spec.label
		if label == "" {
			label = a.label
		}
		e := extent[g.spec.kind]
		b := a.bounds(e[0], e[1])
		p.Series = append(p.Series, Series{
			Kind:     g.spec.kind,
			Label:    label,
			Unit:     a.unit,
			Bounds:   b,
			Latest:   g.readings[len(g.readings)-1].v,
			Segments: segment(g.readings, start, window, maxGap, b),
		})
	}

	return p
}

// Group is the set of series sharing one chart: same kind, so same unit and the
// same y-axis. The caller draws one chart per group.
type Group struct {
	Kind   Kind
	Unit   string
	Bounds Bounds
	Series []Series
}

// Groups partitions the plot into charts. Series of a kind are adjacent by
// construction, so this is a walk over runs rather than a re-sort — which also
// means the drawing order matches the order Build chose.
func (p Plot) Groups() []Group {
	var out []Group
	for _, s := range p.Series {
		if n := len(out); n > 0 && out[n-1].Kind == s.Kind {
			out[n-1].Series = append(out[n-1].Series, s)
			continue
		}
		out = append(out, Group{Kind: s.Kind, Unit: s.Unit, Bounds: s.Bounds, Series: []Series{s}})
	}
	return out
}

// Shape identifies the plot's chart layout — which kinds are present, how many
// series each has, and what they are called. It deliberately says nothing about
// the values.
//
// A caller rebuilds its chart widgets only when this changes and repaints
// otherwise. That matters because the shape genuinely can change under a live
// chart (a fan stops being reported, a device grows a power source) while the
// values change every second, and tearing down a DrawingArea per second to
// redraw the same three charts is the kind of churn that shows up as flicker.
// Same reasoning as the profile selector rebuilding on its signature rather
// than on every state refresh.
func (p Plot) Shape() string {
	var b strings.Builder
	for i, s := range p.Series {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(strconv.Itoa(int(s.Kind)))
		b.WriteByte(':')
		b.WriteString(s.Label)
	}
	return b.String()
}

// spec is one candidate series: what it is called and how to read it out of a
// sample. Specs exist before it is known whether any sample carries the value.
type spec struct {
	kind  Kind
	label string
	value func(api.TelemetrySample) (float64, bool)
}

// gather pulls one spec's readings out of the samples, dropping those the
// sample does not carry and those older than the window.
func gather(samples []api.TelemetrySample, start time.Time,
	value func(api.TelemetrySample) (float64, bool),
) []reading {
	var out []reading
	for _, s := range samples {
		v, ok := value(s)
		if !ok {
			continue
		}
		at := time.Unix(s.At, 0)
		if at.Before(start) {
			continue
		}
		out = append(out, reading{at: at, v: v})
	}
	return out
}

// fanLabel names one fan of several. Devices with a single fan get the bare
// "Fan" label instead, so the common case does not read as though a second one
// were missing.
func fanLabel(i int) string {
	return "Fan " + strconv.Itoa(i+1)
}

// segment places each reading in the window and splits the run wherever
// consecutive readings are more than maxGap apart.
func segment(readings []reading, start time.Time, window, maxGap time.Duration, b Bounds) [][]Point {
	var out [][]Point
	var cur []Point
	var prev time.Time

	for i, r := range readings {
		if i > 0 && r.at.Sub(prev) > maxGap {
			out = append(out, cur)
			cur = nil
		}
		prev = r.at

		x := float64(r.at.Sub(start)) / float64(window)
		cur = append(cur, Point{
			X:  clamp01(x),
			Y:  clamp01((r.v - b.Min) / b.Span()),
			V:  r.v,
			At: r.at,
		})
	}
	return append(out, cur)
}

func clamp01(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}
