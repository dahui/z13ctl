# The dashboard polls `telemetry-history` only while it is the visible view

**The dashboard polls `telemetry-history` only while it is the visible view.**
It is a separate loop from `startTelemetryPolling`, which reads `get-state` for
the header's live numbers on every view: history is a different command and a
much larger reply, so running it drawer-wide would be a per-second
few-hundred-sample round trip nobody is looking at. Every path that leaves the
view calls `dashboardView.stopPolling`, including `hide()`, which has no view switch
to be caught by the tick's own visible-child guard.
Failures are silent for the same reason the telemetry poll's are — a background
refresh the user did not ask for must not repaint the error bar every second —
but the *empty* state names which kind of nothing it is: daemon not running,
daemon too old, or no readings yet. Any error reads as "too old", on the same
grounds as `probeStoredTarget`; over-claiming is cheap here only because the
refresh runs every second, so a transient failure shows it for one tick.
