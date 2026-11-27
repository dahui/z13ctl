# The dashboard's three rules live in `internal/telemetryplot`, and each is there because the obvious alternative draws something false

**The dashboard's three rules live in `internal/telemetryplot`, and each is
there because the obvious alternative draws something false.** (1) Points are
placed by **timestamp**, never by slice index — the samples are not evenly
spaced, so an index-placed plot renders a ten-hour suspend as though no time
passed. (2) A gap **breaks the line**; interpolating across one draws a smooth
ramp through hours nobody measured. `DefaultMaxGap` is 5 s — four consecutive
misses — and is deliberately generous, because the gap worth showing is a
suspend while a single failed sysfs read is noise, and a threshold tight enough
to catch one would fragment the chart on any machine with a flaky sensor.
(3) A quantity **no sample carries produces no series at all**: the same
honesty rule as `TestTelemetryDeclarationMatchesWhatIsRead`, one layer up.
For most fields the wire cannot distinguish absent from zero (they are
`omitempty`), so presence is decided per field on what a zero would *mean*:
0°C is not a plausible APU temperature and reads as absent, while 0 RPM is a
stopped fan and is a reading — hence the RPM **slice's** presence is the test
there, not its value. That asymmetry looks like an inconsistency until you
know why, so `TestZeroMeansAbsentForTemperatureButNotForFans` pins it.
Battery charge is the case where per-field cleverness ran out and the *wire*
had to change: its zero is plausible (a flat pack), so it is a pointer and
presence is the pointer — first established for the flow this chart plotted
before it became state of charge, and inherited by the level for the same
reason. Testing the value instead would drop a dying machine's chart at the
moment it matters, and testing nothing draws one flat at zero on a desktop
with no pack — the exact false measurement rule (3) exists to prevent
(`TestBatteryZeroIsAReadingButAbsenceIsNot`).
