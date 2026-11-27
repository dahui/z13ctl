# The telemetry sampler stands down while suspending for a *different reason* than the other watchers, and the difference is load-bearing

**The telemetry sampler stands down while suspending for a *different reason*
than the other watchers, and the difference is load-bearing.** `reconcileTick`
and `powerTick` stand down because they **write hardware**, and a write landing
between `PrepareForSleep(true)` and the freeze undoes the fan release that lets
the EC stop the fans overnight — the `powerSourceOnce` bug. The sampler writes
nothing, so it cannot do that harm; it stands down because the samples would be
*misleading*. The pre-sleep release has already lowered the PPT and handed the
fans to firmware auto, so a sample taken there records the released machine and
the graph shows a thermal cliff seconds before a suspend that explains nothing.
Cosmetic, not a safety property — but the graph is the whole feature. Stating
it as "same lesson as powersource" would be wrong in a way that matters if
anyone relaxes it.
It needs **no staleness ceiling** for the same reason: `reconcileTick` counts
awake ticks against `reconcileSuspendMaxTicks` because a lost
`PrepareForSleep(false)` would leave the fans undefended forever, whereas here
a lost resume costs a gap in a graph until the next suspend clears the flag,
and re-arming on a guess would put samples back exactly where they are least
trustworthy. `sampleOnce` also takes neither `hwMu` nor `d.mu` for the read
itself, on the same grounds `*-get` handlers do not: blocking the dashboard's
data behind a fan write sequence is the regression, not the protection.
