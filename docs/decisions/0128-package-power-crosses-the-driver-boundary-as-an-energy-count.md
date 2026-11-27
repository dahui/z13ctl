# Package power crosses the driver boundary as an energy *counter*, not as watts

**Package power crosses the driver boundary as an energy *counter*, not as
watts.** RAPL publishes cumulative microjoules, so power is a difference over
an interval — arithmetic that needs the *previous* reading, which a driver
cannot hold: `Telemetry.Sample()` is called by the 1 Hz sampler *and* by every
`get-state` handler, and drivers are passive by design (no locking, no
goroutines). The counter therefore goes out as `Sample.PackageEnergyUJ` and
the daemon's sampler — the one sequential caller — converts it in
`packagePowerW`, a pure function with a table (`internal/daemon`). A device
whose hardware reports instantaneous power instead (OXP's pm-table) fills
`PackagePowerW` directly and the sampler leaves it alone.
Three of that table's cases are the ones worth knowing: a **wrap** is real
(262 kJ is ~73 minutes at 60 W) and must be added back rather than dropped; a
**reset** is indistinguishable from a wrap by the values alone and would
otherwise be reported as ~262 kW, which is what `implausiblePackageW` exists
to catch; and a gap longer than `maxEnergyGap` yields nothing, because the
average across a suspend describes no moment inside it. Every refusal records
**no** power rather than zero — 0 W is a claim the package drew nothing, and
a running machine never does.
