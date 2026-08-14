// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package telemetryplot

// placeholder.go — the dashboard's loading frames. The full card set appears
// the moment the page does, each chart framed at its kind's nominal axis with
// no trace in it, and real data replaces the frames as it arrives (Jeff,
// 2026-08-14: a bare page for the first second read as the app failing).
//
// This bends the package's honesty rule — "a chart frame with no line reads
// as 'the machine measured nothing'" — deliberately and only for the loading
// phase: a frame with no trace and a "—" readout beside it reads as *waiting*,
// which is the truth of that moment, while the two actionable kinds of nothing
// (daemon not running, daemon too old) still replace the frames with prose.

// PlaceholderCaps is what the device document declares, reduced to the
// decisions the placeholder set needs. Callers with no document set every
// field true — the keep-everything posture limits.FromDevice takes, for the
// same reason.
type PlaceholderCaps struct {
	Power    bool // telemetry.power_draw
	Battery  bool // a battery section
	GPU      bool // telemetry.gpu
	CPUStats bool // telemetry.cpu_stats
	NPU      bool // telemetry.npu
	Net      bool // telemetry.net
}

// PlaceholderKinds is the card set to frame before any history has arrived:
// temperature and fans always (every device shipping telemetry measures
// both), everything else only when a declared source would fill it — a
// placeholder for a source the device disclaims would sit empty forever,
// which is the false claim the honesty rule exists to prevent. Each kind
// appears when *any* of its series has a source behind it; the frames are a
// coarse guess the first real shape corrects.
func PlaceholderKinds(c PlaceholderCaps) []Kind {
	kinds := []Kind{KindTemp, KindFan}
	if c.Power || c.GPU || c.NPU {
		kinds = append(kinds, KindPower)
	}
	if c.Battery {
		kinds = append(kinds, KindBattery)
	}
	if c.CPUStats || c.GPU || c.NPU {
		kinds = append(kinds, KindLoad)
	}
	if c.CPUStats || c.GPU {
		kinds = append(kinds, KindClock, KindMemory)
	}
	if c.Net {
		kinds = append(kinds, KindNet)
	}
	return kinds
}

// Placeholder returns one framed, series-less group per kind, each spanning
// its kind's nominal axis. The caller draws them exactly as it draws real
// groups — grid rules and axis labels, no traces.
func Placeholder(kinds []Kind) []Group {
	out := make([]Group, 0, len(kinds))
	for _, k := range kinds {
		a, ok := axes[k]
		if !ok {
			continue
		}
		out = append(out, Group{
			Kind:   k,
			Unit:   a.unit,
			Bounds: a.bounds(a.nomMin, a.nomMax),
		})
	}
	return out
}
