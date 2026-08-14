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
// ("APU", "Fan", "Package", "Battery").
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
		return FormatValue(s.Kind, s.Latest) + s.Unit
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

// FormatValue renders a reading. Temperature, RPM and percentages are whole
// numbers on this hardware; power, the GHz-denominated clocks and the GB
// memory gauges are not — truncating 27.4 W to 27, or 3.2 GHz to 3, loses the
// only digit that moves.
func FormatValue(kind Kind, v float64) string {
	switch kind {
	case KindPower, KindClock, KindMemory:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
