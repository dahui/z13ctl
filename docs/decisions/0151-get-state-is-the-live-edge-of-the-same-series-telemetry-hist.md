# `get-state` is the *live* edge of the same series `telemetry-history` plots, and it must carry every quantity the device measures

**`get-state` is the *live* edge of the same series `telemetry-history`
plots, and it must carry every quantity the device measures.** The roadmap
puts live updates on the existing 1 Hz `get-state` poll and history on the
ring, so the two are one feature seen at two timescales. That was half-built
for a release: `Sample()` reported temperature, both fans, the energy counter
and battery flow, and the get-state handler published `Temperature` and `RPM[0]` —
a dashboard charting four series beside readouts showing two. `RPM []int`,
`PackagePowerW` and `BatteryPowerW` close it; `FanRPM` stays forever as
`RPM[0]` because every pre-2.0 client reads it. Quoting one of two fans that
cool the same die describes neither — the Z13's differ by hundreds of RPM.
**Package power is the one quantity a handler may not derive.** It is a rate,
so on counter hardware it needs the previous reading and the interval since;
`d.prevEnergy` belongs to the sampler's goroutine and is unguarded *because*
that goroutine is its only writer, so re-baselining it from a socket handler
would be both a data race and a corruption of the series being drawn. The
handler serves the sampler's most recent figure instead, which is also the
only way the live readout and the right-hand edge of the chart can agree —
two numbers for one quantity that disagree is indistinguishable from a bug.
It is served **absent rather than stale**, bounded by `liveTelemetryMaxAge`.
