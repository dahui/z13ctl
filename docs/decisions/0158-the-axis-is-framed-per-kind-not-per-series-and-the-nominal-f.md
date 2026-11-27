# The axis is framed per *kind*, not per series, and the nominal frame is a starting point that expands rather than a clamp

**The axis is framed per *kind*, not per series, and the nominal frame is a
starting point that expands rather than a clamp.** Two fans are drawn on one
chart, so separate axes would make their line heights incomparable — which is
the one thing a viewer will use them for. The nominal ranges keep the axis
steady while values wander, because a chart
that rescales every second at a 1 Hz refresh is unreadable; anything outside
expands the frame to a step boundary, so no reading is ever cut off. That
invariant is what makes it safe to carry one laptop's numbers in a package
meant to serve every device, and it is a test
(`TestTheAxisNeverClipsAReading`) rather than a comment.
**The nominal top is the device's where it can say** (`telemetryplot.Hints`,
2026-10-09): clocks from `telemetry.clock_max_mhz` (the CPU's
`cpuinfo_max_freq` and the GPU's top `pp_dpm_sclk`; the built-in 0–4 GHz cut
through this CPU's 5.19 GHz boost), temperature from `telemetry.temp_limit_c`
(the ACPI passive trip, 100 °C here), power from the live `tdp_max_forced`,
fans from `fans.rpm_max` (8900 on the Z13, measured; 9000 axis), memory from the
samples' own capacity. The placeholders take the same hints so the first data
does not jump the axis, and a device with no fans gets no Fan placeholder.
`Plot.Shape()` is the rebuild key: chart widgets are torn down only when the
*layout* changes (a fan stops being reported, a power source appears), never
when the values do — the same reasoning as the profile selector's signature.
