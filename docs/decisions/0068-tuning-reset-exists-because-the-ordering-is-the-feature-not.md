# `tuning-reset` exists because the *ordering* is the feature, not the convenience

**`tuning-reset` exists because the *ordering* is the feature, not the
convenience.** Clearing the fan curve, power limits and Curve Optimizer offset
by hand means three commands, and each of them has to lower power before
releasing the fans — so issuing them in the wrong order leaves the machine at a
high sustained limit with no floor, and a failure partway leaves it there. One
daemon operation under one `hwMu` hold takes that out of the caller's hands,
which is why the GUI's Reset All button sends one request rather than three.
It shares its whole body with `tdp-reset` (`resetToStock`), because they *are*
the same sequence: `handleTDPReset` already cleared the curve, the limits and
CO in hardware. They differ in one thing — whether the profile's **saved** fan
curve and offset go too. `tdp-reset` keeps the offset so `profile --set custom`
can recall it; `tuning-reset` is the command that says to discard all of it.
Two copies of that sequence is exactly how the fan-floor rules drifted apart.
The `implicit()` guard applies unchanged: a reset resolved from a *firmware*
profile must not commit a cleared `custom`, which is the silent-data-loss rule.
The non-live path needs no fan-floor check even though it can drop a stored
curve, because it drops that profile's TDP in the same edit — unlike
`handleFanCurveReset`, which clears the curve and leaves the limit.
In the GUI it is a **bar** action beside Commit, not a card action: the
per-card resets each remove one subsystem, this removes all three and lands on
balanced. `full:custom` re-baselined 19 → 20 (`13:0:commit 13:1:commit`), every
other focus line byte-identical.
