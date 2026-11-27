# A declared telemetry source is offered only where its hardware is present

**A declared telemetry source is offered only where its hardware is present**
(2026-10-09). `telemetry.Info()` checks each declaration once, by stat or
static attribute (the NPU check never opens the accel node, which would wake
it), and drops the absent ones, so a client does not frame a chart that never
fills. The device data stays the opt-in: an undeclared source is never read.
Daemon tests pin the declaration (`deviceFileTelemetry`) while `Sample` reads
the real machine, which is what the declaration guard needs.
