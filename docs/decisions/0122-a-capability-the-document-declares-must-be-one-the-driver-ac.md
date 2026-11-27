# A capability the document declares must be one the driver actually reads

**A capability the document declares must be one the driver actually reads.**
Capability discovery is by absence, so a declared-but-unread source does not
degrade to "nothing shown" — it degrades to a dashboard drawing a graph that
is flat at zero, which is worse than no graph because it looks like a
measurement. This bit at `telemetry.power_draw`, which was declared-and-unread
for a release: the Z13 exposes powercap RAPL (`intel-rapl:0`, name
`package-0`), but `energy_uj` is `0400 root:root` under the Platypus
mitigation, so it needs a udev grant — and rather than do the grant, the
device TOML simply named no source. **Both halves now exist**: `voltaire
setup` grants group *read* on `energy_uj`, `Sample()` reads the counter, and
the TOML declares `power_draw = "rapl"`.
`TestTelemetryDeclarationMatchesWhatIsRead` (`internal/daemon`) replaced the
test that pinned the absence, and still fails in both directions — a source
declared with no reader, or a readable counter with no declaration. It
*skips* when the counter exists but is unreadable, because that is a machine
that has not run setup rather than a defect, and the skip message says which.
Any new capability field with a reading behind it wants the same guard.
