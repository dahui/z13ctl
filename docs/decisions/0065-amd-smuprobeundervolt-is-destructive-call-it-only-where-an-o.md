# `amd.smuProbeUndervolt()` is destructive — call it only where an offset is about to be written

**`amd.smuProbeUndervolt()` is destructive — call it only where an offset is
about to be written.** The "safe no-op probe" sends CO offset 0, which is
byte-for-byte what `ResetCurveOptimizer` sends, so probing *clears any active
undervolt* — and its first run in a process is an MP1 mailbox write, the step
with a known hard-hang mode (see the applied-offset gate below). In the daemon
it is reached only through `d.uvProbe()` (`Undervolter.ProbeAvailable`), from
`handleUndervolt`, `applyCustomHW` and `reconcileOnce`, each about to write an
offset, and from `uvApplied` once state says one is applied;
`undervolt_gate_test.go:TestOnlyWritersProbeTheSMU` pins the list. Anything
that only *asks* — `get-state`'s `undervolt_available`, `undervolt-get`, an
`undervolt-reset` with nothing applied — uses `d.uvAvailable()`
(`Undervolter.Available`, `SMUUndervoltAvailable` on the Z13): the probe's
cached answer once something has probed, module presence until then. Both
`uvAvailable` and `get-state` used to probe, so the GUI's first poll after every
daemon start sent a CO reset on machines that had never undervolted (ported to
main alongside). The trade: a wrong-fork machine reports "available" until the
first real write probes and fails. In a short-lived CLI process the
`sync.Once` is fresh every invocation, so a `status` that probed would wipe the
user's undervolt every run; `status` asks the daemon and falls back to
`SMUAvailable()` with wording that claims less.
`internal/drivers/amd/smu_test.go:TestSMUProbeIsDestructive` pins the payload equality;
if the probe ever becomes genuinely read-only, that test is the signal to relax
these warnings.
