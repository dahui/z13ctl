# `freshEnough` compares Unix seconds, and the reason is that the hazard it guards cannot be tested

**`freshEnough` compares Unix seconds, and the reason is that the hazard it
guards cannot be tested.** Ring timestamps and `time.Now()` both carry
monotonic readings, which `Sub`/`Since` prefer — and Go's monotonic clock is
`CLOCK_MONOTONIC`, which does not advance across a suspend (the same fact
`reconcileSuspendMaxTicks` is counted in ticks for). A monotonic comparison
serves a pre-suspend figure as the current draw for the second between a
resume and the sampler's next tick. The first fix was `at.Round(0)`, which is
correct and undetectably fragile: **the negative control was run, and a test
named for the wall clock passed with the safeguard deleted**, because
`time.Now().Add(-10*time.Hour)` moves both readings together and nothing
in-process separates them. `Unix()` has no monotonic path to remove by
accident. The cost is second granularity against a three-tick bound on a 1 Hz
sampler, which is nothing. `TestFreshEnoughBound` covers the window and says
in its own comment that it does *not* cover the hazard, so a green run is not
mistaken for evidence.
