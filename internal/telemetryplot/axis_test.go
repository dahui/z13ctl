// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot

// axis_test.go — the axis table's own invariants. In-package because the table
// is not exported: it is presentation data, and the thing worth pinning is that
// a Kind added later cannot reach the plot without an entry.

import (
	"math"
	"testing"
)

// TestEveryKindHasAWellFormedAxis is what the defensive guards in bounds() are
// really for. A new Kind with no table entry gets the zero axis, whose zero step
// divides to infinity — so this test is the one that has to fail first.
func TestEveryKindHasAWellFormedAxis(t *testing.T) {
	for kind := KindTemp; kind <= KindMemory; kind++ {
		a, ok := axes[kind]
		if !ok {
			t.Errorf("Kind %d has no axis entry", kind)
			continue
		}
		if a.label == "" || a.unit == "" {
			t.Errorf("Kind %d: label %q unit %q, both must be set", kind, a.label, a.unit)
		}
		if a.step <= 0 {
			t.Errorf("Kind %d: step %v, want positive", kind, a.step)
		}
		if a.nomMax-a.nomMin < a.step {
			t.Errorf("Kind %d: nominal frame %v..%v is narrower than one step (%v)",
				kind, a.nomMin, a.nomMax, a.step)
		}
	}
	// The last kind in the enum, so a kind added without an axis is caught
	// rather than silently framed 0..0 the first time something reports it.
	if len(axes) != int(KindMemory)+1 {
		t.Errorf("axes has %d entries for %d kinds", len(axes), int(KindMemory)+1)
	}
}

// TestBoundsSurvivesADegenerateAxis exercises the guards directly, since no
// entry in the shipped table can reach them.
func TestBoundsSurvivesADegenerateAxis(t *testing.T) {
	cases := []struct {
		name   string
		a      axis
		lo, hi float64
	}{
		{"zero step", axis{nomMin: 0, nomMax: 10, step: 0}, 2, 8},
		{"negative step", axis{nomMin: 0, nomMax: 10, step: -5}, 2, 8},
		{"collapsed frame", axis{nomMin: 5, nomMax: 5, step: 10}, 5, 5},
		{"inverted frame", axis{nomMin: 10, nomMax: 0, step: 10}, 5, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.a.bounds(tc.lo, tc.hi)
			if b.Span() <= 0 {
				t.Fatalf("span %v, want positive so the caller can divide by it", b.Span())
			}
			if math.IsNaN(b.Min) || math.IsNaN(b.Max) || math.IsInf(b.Min, 0) || math.IsInf(b.Max, 0) {
				t.Fatalf("bounds %v..%v are not finite", b.Min, b.Max)
			}
			if tc.lo < b.Min || tc.hi > b.Max {
				t.Errorf("bounds %v..%v clip the data %v..%v", b.Min, b.Max, tc.lo, tc.hi)
			}
		})
	}
}

func TestClamp01(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{-1, 0}, {0, 0}, {0.5, 0.5}, {1, 1}, {2, 1},
		// NaN reaches this only through a degenerate span, but a NaN coordinate
		// handed to Cairo poisons the whole path rather than misplacing a point.
		{math.NaN(), 0},
		{math.Inf(1), 1}, {math.Inf(-1), 0},
	}
	for _, tc := range cases {
		if got := clamp01(tc.in); got != tc.want {
			t.Errorf("clamp01(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
