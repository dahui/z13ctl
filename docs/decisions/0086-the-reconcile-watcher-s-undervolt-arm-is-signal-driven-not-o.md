# The reconcile watcher's undervolt arm is signal-driven, not observation-driven, because CO has no readback

**The reconcile watcher's undervolt arm is signal-driven, not
observation-driven, because CO has no readback.** `SetCurveOptimizer` and
`ResetCurveOptimizer` are write-only — the ryzen_smu interface offers nothing to
read back — so the watcher cannot tell that hardware lost the offset. CO is
volatile across suspend and `restoreVolatileState` normally replaces it; when
that signal never arrives the offset sits at stock while state reports it
applied, and no observation can notice. `reconcileTick` therefore sets
`act.Undervolt` on exactly one path: the stand-down budget expiring, which is
the only evidence available that a `PrepareForSleep(false)` went missing. It is
deliberately **not** a periodic re-apply — that would write the CPU voltage
curve every 2 s on no evidence — and it is idempotent in the case the signal
cannot distinguish (an abandoned suspend never lost the offset).
`amd.smuProbeUndervolt()` is called in the *apply* step rather than the observe
step so the common path never touches it; the daemon's startup probe means the
`sync.Once` makes it a cached bool read rather than the destructive write it
would otherwise be.
