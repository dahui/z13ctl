# The dashboard offers five history windows, and the refresh cadence scales with the one on screen

**The dashboard offers five history windows, and the refresh cadence scales
with the one on screen** (`internal/telemetryplot/span.go`; Jeff, 2026-08-14:
"I imagine people may want to see longer history"). 1m, 5m, 15m, 30m, 1h,
filtered by `Offered` against what the device actually retains — a span the
ring cannot fill would only ever draw a part-filled chart. The shortest is
always kept, so a device with a tiny ring gets one button rather than an empty
strip that reads as a broken control.
**The buttons already existed; what was missing was the history.** The device
declared 300 seconds, so 15m and 1h were filtered out at build time and had
never once appeared. The change is `history_seconds = 3600`, and the cost was
measured rather than assumed: `driver.Sample` is 288 bytes plus a two-int RPM
slice, so an hour is ~1 MiB of ring. Memory was never the constraint.
**The wire was.** `telemetry-history` is ~440 bytes per sample, so an hour is
~1.5 MiB *per poll*, and the dashboard polled every second — 1.5 MB/s of socket
traffic and JSON decoding, on a tool whose purpose is managing the machine's
power, to animate a chart that had not changed. `RefreshInterval` is
`span / 300` with a one-second floor (300 being about the horizontal pixels a
card resolves), so an hour refreshes every twelve seconds and the per-second
cost stops growing at 5m: 129 KiB/s at every span from there up, measured.
Only the *trace* slows — the card headers' live numbers come from the separate
1 Hz `get-state` poll every view runs, so the readouts stay current at any
span. `setSpan` restarts the loop rather than only refreshing, or a loop armed
for the old span would keep its cadence.
`TestRefreshCostDoesNotGrowWithTheSpan` is the guard, and **its first version
passed with the scaling deleted**: it derived its ceiling from
`RefreshInterval` itself, so the ceiling moved with the bug. It anchors to a
fixed reference now — five minutes of samples at 1 Hz, the most this feature
has ever cost. The negative control is the only thing that caught it, which is
the second time in two days a source-shaped or self-referential guard needed
one.
One consequence worth knowing rather than fixing: **right after a daemon
restart a long span looks nearly empty**, because the ring is in memory and
holds only what has been sampled since. That is the honest rendering — points
are placed by timestamp, so the trace occupies the right-hand sliver and the
rest is genuinely unmeasured — and it is why the list stops at an hour rather
than offering a day. Anything longer wants a store on disk, which is a
different feature.
