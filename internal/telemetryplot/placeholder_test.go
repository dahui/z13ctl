// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot

import (
	"testing"
	"time"

	"github.com/dahui/voltaire/api/v2"
)

func TestPlaceholderKinds(t *testing.T) {
	all := PlaceholderCaps{Power: true, Battery: true, GPU: true, CPUStats: true, NPU: true, Net: true}
	cases := []struct {
		name string
		caps PlaceholderCaps
		want []Kind
	}{
		{"everything declared", all,
			[]Kind{KindTemp, KindFan, KindPower, KindBattery, KindLoad, KindClock, KindMemory, KindNet}},
		{"nothing declared", PlaceholderCaps{},
			[]Kind{KindTemp, KindFan}},
		{"battery only", PlaceholderCaps{Battery: true},
			[]Kind{KindTemp, KindFan, KindBattery}},
		// A net source alone brings exactly its own frame.
		{"net only", PlaceholderCaps{Net: true},
			[]Kind{KindTemp, KindFan, KindNet}},
		// An NPU alone brings a power and a load frame — it has a series on
		// each — but no clock or memory card, whose series it never fills.
		{"npu only", PlaceholderCaps{NPU: true},
			[]Kind{KindTemp, KindFan, KindPower, KindLoad}},
		// cpu_stats alone: load, clocks and memory but no power domain.
		{"cpu stats only", PlaceholderCaps{CPUStats: true},
			[]Kind{KindTemp, KindFan, KindLoad, KindClock, KindMemory}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlaceholderKinds(tc.caps)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestPlaceholderFramesAreDrawable pins the properties the dashboard's draw
// path relies on: every group carries its kind's nominal axis (a non-zero span
// to divide by, labels to print) and no series (nothing traced, so the frame
// cannot claim a measurement).
func TestPlaceholderFramesAreDrawable(t *testing.T) {
	groups := Placeholder(PlaceholderKinds(PlaceholderCaps{
		Power: true, Battery: true, GPU: true, CPUStats: true, NPU: true, Net: true,
	}))
	if len(groups) != 8 {
		t.Fatalf("got %d groups, want 8", len(groups))
	}
	for _, g := range groups {
		if len(g.Series) != 0 {
			t.Errorf("kind %d: placeholder carries %d series; a trace here is a fabricated reading", g.Kind, len(g.Series))
		}
		if g.Bounds.Span() <= 0 {
			t.Errorf("kind %d: degenerate bounds %+v", g.Kind, g.Bounds)
		}
		if g.HeaderTitle() == "" {
			t.Errorf("kind %d: no header title", g.Kind)
		}
	}
	// The battery frame is the same hard 0–100 the real chart uses — state of
	// charge, not the signed flow this frame straddled zero for before
	// 2026-08-14 — so the first real data lands in an identical frame with no
	// visible jump.
	for _, g := range groups {
		if g.Kind == KindBattery && (g.Bounds.Min != 0 || g.Bounds.Max != 100) {
			t.Errorf("battery frame %+v, want the 0..100 charge frame", g.Bounds)
		}
	}
}

// TestPlaceholderIsNotAPlotShape: the dashboard keys chart rebuilds on shape
// strings; a real plot's shape must never collide with the placeholder's
// sentinel, or the first data would fail to replace the frames.
func TestPlaceholderIsNotAPlotShape(t *testing.T) {
	now := time.Now()
	p := Build([]api.TelemetrySample{{At: now.Unix(), TempC: 52}}, now, time.Minute, 0)
	if p.Shape() == "" {
		t.Fatal("a plot with a reading has an empty shape; the sentinel comparison below proves nothing")
	}
	if p.Shape() == "placeholder" {
		t.Fatal("a real plot's shape collides with the placeholder sentinel")
	}
}
