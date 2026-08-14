// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot_test

import (
	"testing"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/telemetryplot"
)

func TestFormatValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kind telemetryplot.Kind
		v    float64
		want string
	}{
		// Power keeps the one digit that moves while a load ramps.
		{"power keeps a decimal", telemetryplot.KindPower, 27.44, "27.4"},
		{"temperature is whole", telemetryplot.KindTemp, 52.4, "52"},
		{"rpm is whole", telemetryplot.KindFan, 2600, "2600"},
		{"battery charge is whole", telemetryplot.KindBattery, 81.4, "81"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := telemetryplot.FormatValue(tc.kind, tc.v); got != tc.want {
				t.Fatalf("FormatValue(%v, %v) = %q, want %q", tc.kind, tc.v, got, tc.want)
			}
		})
	}
}

func TestHeaderTitleAndValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		group     telemetryplot.Group
		wantTitle string
		wantValue string
	}{
		{
			name: "single-series temperature",
			group: telemetryplot.Group{Kind: telemetryplot.KindTemp, Unit: "°C", Series: []telemetryplot.Series{
				{Kind: telemetryplot.KindTemp, Label: "CPU", Unit: "°C", Latest: 52},
			}},
			wantTitle: "Temp",
			wantValue: "52°C",
		},
		{
			name: "single-series power keeps its decimal",
			group: telemetryplot.Group{Kind: telemetryplot.KindPower, Unit: "W", Series: []telemetryplot.Series{
				{Kind: telemetryplot.KindPower, Label: "Pkg", Unit: "W", Latest: 27.4},
			}},
			wantTitle: "Power",
			wantValue: "27.4W",
		},
		{
			// Two fans on one chart: the series labels repeat the heading, so
			// the header trims the shared prefix rather than printing "Fan"
			// three times on one line.
			name: "two fans trim the shared prefix",
			group: telemetryplot.Group{Kind: telemetryplot.KindFan, Unit: "RPM", Series: []telemetryplot.Series{
				{Kind: telemetryplot.KindFan, Label: "Fan 1", Unit: "RPM", Latest: 2400},
				{Kind: telemetryplot.KindFan, Label: "Fan 2", Unit: "RPM", Latest: 2600},
			}},
			wantTitle: "Fan",
			wantValue: "1: 2400 · 2: 2600 RPM",
		},
		{
			name: "single fan",
			group: telemetryplot.Group{Kind: telemetryplot.KindFan, Unit: "RPM", Series: []telemetryplot.Series{
				{Kind: telemetryplot.KindFan, Label: "Fan", Unit: "RPM", Latest: 2400},
			}},
			wantTitle: "Fan",
			wantValue: "2400RPM",
		},
		{
			name:      "an empty group claims nothing",
			group:     telemetryplot.Group{Kind: telemetryplot.KindTemp},
			wantTitle: "Temp",
			wantValue: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.group.HeaderTitle(); got != tc.wantTitle {
				t.Fatalf("HeaderTitle() = %q, want %q", got, tc.wantTitle)
			}
			if got := tc.group.HeaderValue(); got != tc.wantValue {
				t.Fatalf("HeaderValue() = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

// TestHeaderMatchesABuiltPlot drives the same strings through Build, so the
// literal groups above cannot drift from what the plot actually produces.
func TestHeaderMatchesABuiltPlot(t *testing.T) {
	t.Parallel()
	samples := []api.TelemetrySample{
		sampleAt(2, 51, []int{2300, 2500}),
		sampleAt(1, 52, []int{2400, 2600}),
	}
	p := telemetryplot.Build(samples, now, time.Minute, 0)

	for _, g := range p.Groups() {
		if g.Kind != telemetryplot.KindFan {
			continue
		}
		if got := g.HeaderTitle(); got != "Fan" {
			t.Fatalf("HeaderTitle() over a built plot = %q, want %q", got, "Fan")
		}
		if got := g.HeaderValue(); got != "1: 2400 · 2: 2600 RPM" {
			t.Fatalf("HeaderValue() over a built plot = %q, want %q", got, "1: 2400 · 2: 2600 RPM")
		}
		return
	}
	t.Fatal("no fan group in the built plot")
}
