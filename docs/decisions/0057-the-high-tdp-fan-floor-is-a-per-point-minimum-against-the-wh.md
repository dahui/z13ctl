# The high-TDP fan floor is a *per-point minimum against the whole `FloorCurve`*, not against its `HighTDPMinPWM` bottom, and not a replacement curve

**The high-TDP fan floor is a *per-point minimum against the whole
`FloorCurve`*, not against its `HighTDPMinPWM` bottom, and not a replacement
curve.** `safety.FanCurveForTDP` is the single place that rule lives: above
`TDPMaxSafe` it returns the caller's own curve with each point raised to
`FloorCurve`'s value at the matching index where it falls below it, and
**every point above it left exactly as drawn**. Nothing is ever lowered.
`FloorCurve` is returned whole only when there is no curve at all —
nothing to raise, so it is all that is left to write. `ApplyTDPSafely` and
`reconcileTick` both call it.
**The comparison is at each point's *temperature*, via `FloorPWMAt`, not at the
matching slice index.** Index matching was the first attempt: a curve of
`70:130,75:135,80:140,…` clears every index-matched comparison and still runs 55%
fans at 80°C, *weaker* than the wholesale replacement it replaced. Since the
justification is stated in temperature terms, and the commands reference
publishes the floor as a temperature table, the comparison has to be too.
**Clamping to the scalar minimum alone was wrong and shipped briefly during
development.** It honoured the first half of the rationale for lowering the
minimum from 204 to 127 and threw away the second: "the *ramp* is what protects
the APU, since a machine actually sustaining >75W is well past 60°C". A curve flat
at 127 satisfied a scalar 127 everywhere, so 93W sustained at 90°C ran the fans at
50% where every pre-1.3.1 path would have reached 100%. Temperatures are always
the user's — only PWM values move — and the floor is read at each point's
*temperature*, interpolated piecewise-linearly between floor points
(`safety.FloorPWMAt`), clamped to the first/last PWM outside the range. An
earlier index-matched reading is what the temperature rule replaced: a point
at 48°C is measured against ~137, the ramp's value there, not against
whichever floor point shares its slice position. The function stays pure and
total either way.
`FloorAdjustsCurve` is derived from `FanCurveForTDP` rather than reimplementing
the comparison, so the two cannot disagree about what counts as an adjustment;
the scalar version answered "no problem" for curves the ramp does raise.
**`CheckCurveAgainstTDP` must measure against the same floor.** It is the
*edit-time* refusal, and while it used the scalar minimum `fancurve --set` was an
open door around the whole rule: a curve flat at 127 cleared 127 everywhere, so
`handleFanCurve` accepted it and wrote it verbatim, and the reconcile watcher then
read `pwm_enable=1` as "the curve is live" and never corrected it — 93W at 90°C on
50% fans, through the one write path that did not consult `FanCurveForTDP`. Any
new path that writes a curve must go through one of the two.
They used to carry separate copies that disagreed — the watcher honoured a curve
above the floor, the apply path replaced *every* curve above `TDPMaxSafe` — and
the apply path was the one users saw. A saved curve of 204→255 came back as the
127→255 ramp, and a curve at 100% everywhere was downgraded to one that idles at
50%; the watcher could not correct it either, because the fans were left in mode
1, which reads as "the curve is live". It presented as "my fan curve resets to
stock after sleep" because `applyCustomHW` runs on every resume, but every
`tdp --set` above 75W had always done it.
Two properties of the clamp are load-bearing: it **copies** — `want` aliases
`p.FanCurve.Points` in daemon state, and clamping in place would rewrite the
user's saved curve so there was nothing to restore when the limit came down —
and it preserves the non-decreasing PWM order `ParseFanCurve` enforces, since
`max(x, floor)` over a non-decreasing `x` is still non-decreasing.
`safety.FloorAdjustsCurve` is the companion predicate every "the floor changed your
curve" message must gate on. A *nil* curve counts as adjusted, while
`CheckCurveAgainstTDP` alone answers "no problem" for it, since a curve with no
points has none below the floor.
**It must be evaluated against the curve as it was *before* the write.** On the
daemon path `cmd/tdp.go` asked it *after* `SendTdpSetFor`, by which point the
daemon had already written the clamped curve — so the live curve satisfied the
floor by construction, the notice never printed, and the one case that did print
it was a failed read returning nil. `preCurve` is now sampled before the send and
reused by both the daemon and no-daemon branches. Any new "we altered what you
asked for" message has the same ordering requirement.
`reconcileTick` has the mirror-image trap: its curve branch must set `act.Reason`
only when a curve action actually results, or a PPT-only reconcile on a curveless
profile logs a fan-floor reason for a machine at 52 W — the TDP arm below only
fills in a reason when one is not already set, so the wrong text wins.
The edit-time refusals (`safety.CheckCurveAgainstTDP` in `handleFanCurve`,
`CheckCurveAgainstTDP` for a non-live `--profile` target) deliberately stay
refusals rather than clamping: storing a curve silently different from the one
the user drew is worse than saying no up front. Clamping is for the curve already
in state when the *limit* rises afterwards, which no edit-time check can catch.
