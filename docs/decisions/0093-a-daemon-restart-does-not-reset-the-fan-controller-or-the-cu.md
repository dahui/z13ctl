# A daemon restart does not reset the fan controller or the Curve Optimizer

**A daemon restart does not reset the fan controller or the Curve Optimizer.**
Both are hardware state that outlives the process, so a custom profile that was
in force still is. If the startup autoswitch resolution moves off it onto a
firmware profile, `Run()` must release them exactly as `applyStockHW` would —
otherwise the machine keeps running the old curve and offset while reporting a
firmware profile, and the reconcile watcher stays inert because the profile is
no longer custom.
