# `telemetryring` timestamps with wall-clock time and carries its own lock, and both are deliberate

**`telemetryring` timestamps with wall-clock time and carries its own lock,
and both are deliberate.** The ring stores the timestamp its caller supplies,
because it cannot know whether the caller means wall-clock or monotonic and
the two answer differently across a suspend: Go's monotonic clock does not
advance while the machine is asleep (the same fact `reconcileSuspendMaxTicks`
is counted in *ticks* for), so a ten-hour suspend would look like no elapsed
time and the pre-suspend samples would sit inside the window forever. The
daemon passes wall-clock, and the ring is written to survive what that costs —
a backwards clock step cannot make it panic, reorder its contents, or drop
anything it was not asked to. The lock is an exception to "serialization lives
in the daemon" that is safe because it is never held across a call out: it
guards a slice copy, so it cannot participate in the `hwMu` → `d.mu` order.
A fifth daemon lock taken by a watcher *and* by socket handlers is the shape
that produced the `saveState` race. Every value crossing the boundary is
deep-copied in both directions, because `driver.Sample` carries an RPM slice
a driver is free to reuse.
