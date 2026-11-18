// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot

// header.go — the card-header text for a chart group: the heading and the
// live readout the GTK side prints above each chart. Moved here from the cgo
// island so the strings are testable; the widget code only places labels.

import (
	"fmt"
	"strings"
)

// HeaderTitle is the card heading for the group: the kind's axis label
// ("Temp", "Fan", "Power", "Battery").
func (g Group) HeaderTitle() string {
	return axes[g.Kind].label
}

// HeaderValue is the live readout beside the heading: the latest reading with
// its unit ("52°C"), or a per-series list when the chart carries more than
// one ("1: 2400 · 2: 2600 RPM"). Multi-series labels repeat the heading
// ("Fan 1"), so the shared prefix is trimmed rather than printed twice in one
// header line.
func (g Group) HeaderValue() string {
	if len(g.Series) == 0 {
		return ""
	}
	if len(g.Series) == 1 {
		s := g.Series[0]
		return withUnit(FormatValue(s.Kind, s.Latest), s.Unit)
	}
	var b strings.Builder
	for i, s := range g.Series {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(strings.TrimPrefix(s.Label, g.HeaderTitle()+" "))
		b.WriteString(": ")
		b.WriteString(FormatValue(s.Kind, s.Latest))
	}
	b.WriteString(" ")
	b.WriteString(g.Unit)
	return b.String()
}

// withUnit joins a value and its unit as they are written: "52°C" and "40%"
// close up, a word-like unit takes a space ("2597 RPM", "7.6 W"). A single-fan
// device (the OXP) was the first to put RPM on the one-series path, and read
// "2597RPM".
func withUnit(v, unit string) string {
	if unit == "" || strings.HasPrefix(unit, "°") || unit == "%" {
		return v + unit
	}
	return v + " " + unit
}

// FormatValue renders a reading. Temperature, RPM and percentages are whole
// numbers on this hardware; power, the GHz-denominated clocks, the GB memory
// gauges and MB/s throughput are not — truncating 27.4 W to 27, or 3.2 GHz
// to 3, loses the only digit that moves (and an idle link's 0.3 MB/s would
// read as nothing at all).
func FormatValue(kind Kind, v float64) string {
	switch kind {
	case KindPower, KindClock, KindMemory, KindNet:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
