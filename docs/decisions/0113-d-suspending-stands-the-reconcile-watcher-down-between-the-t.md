# `d.suspending` stands the reconcile watcher down between the two signals, with a staleness ceiling

**`d.suspending` stands the reconcile watcher down between the two signals,
with a staleness ceiling.** The watcher polls every 2 s and would otherwise
re-enable the curve in the window before userspace freezes. `reconcileObs`
carries the flag so `reconcileTick` stays pure. The ceiling
(`reconcileSuspendMaxTicks`) is counted in *ticks*, not elapsed time, and that
is load-bearing: Go timers use `CLOCK_MONOTONIC`, which does not advance across
suspend, so the count is of *awake* ticks, reachable only by a
`PrepareForSleep(false)` that never arrived. It must also **exceed logind's
`InhibitDelayMaxSec`**, since the delay lock is the only awake window inside a
suspend: a shorter ceiling fires inside a real pre-freeze window and re-enables
the curve, the very thing it guards against. 60 ticks is 120 s against a default
of 5 s, with room for the 30 s and 60 s values people configure.
`internal/daemon/resume_test.go:TestSuspendCeilingExceedsInhibitDelay` pins it.
`restoreVolatileState` clears the flag via `defer` placed *before* `hwMu` is
taken, so the early return for a firmware profile reaches it too.
**Every watcher that writes fan hardware must consult it, not just
`reconcileOnce`.** `powerSourceOnce` was missed: unplug the charger and suspend
immediately, and it confirmed the edge, took `hwMu` after `releaseVolatileState`
dropped it, and `applyCustomHW` put `pwm_enable` back to 1 before the freeze — the
fans then ran for the whole s2idle period. A third watcher added later needs the
same treatment.
**It must stand down *before* `powerTick`, returning `prev` untouched.** The first
attempt ran the tick and dropped only the apply, which discarded the transition
permanently: this watcher is edge-triggered, `powerTick` latches `st.onAC` on the
confirming tick, and every later tick then computed `sourceChanged == false`. The
machine ran the AC profile on battery until the charger was physically cycled.
Leaving the edge unobserved is what lets it be re-detected after the resume.
`TestAutoswitchDeferralSurvivesTheSuspend` asserts the re-application, not just
that an action was once produced — the test that missed this only checked the
latter. The same reasoning applies to `d.suspending` being armed only once
`sleepTick` has decided to write something: arming it unconditionally stood both
watchers down for suspends that released nothing.
**The budget is per-suspend, and `d.suspendGen` is what makes it so.** Resetting
the counter on the first non-suspending tick looks sufficient and is not: a
machine flapping sleep→wake→sleep faster than the 2 s poll never presents an idle
tick, so the count accumulated across suspends and eventually expired *inside* a
real pre-freeze window — the watcher then re-enabled the curve and undid the
release. `setSuspending` bumps the generation on entry only (not on exit, and not
on a repeated entry), and `reconcileTick` zeroes its counter whenever the
generation it is counting for changes.
