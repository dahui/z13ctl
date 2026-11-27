# A fan-curve preset is sugar for a curve, and the single most important thing about it is that nothing applies one but the user

**A fan-curve preset is sugar for a curve, and the single most important
thing about it is that nothing applies one but the user.** `z13ctl-plus` made
its Quiet *profile* auto-apply a `QuietFanCurve()`, and that curve is what
defeated the firmware's zero-RPM idle — the fork fixed it by deleting the
feature (commit `293fdff`). So presets exist here with the coupling removed:
`applyStockHW` still releases the fans to firmware auto on every stock-profile
switch, and no code path anywhere selects a preset. "Quiet profile should have
a quiet curve" is an obvious-sounding idea that is wrong for a measured
reason, which is why it is written down rather than left to be re-derived.
**There is no preset command and no protocol addition beyond the document.**
`--preset <name>` resolves to the identical curve string `--set` takes and
then runs the existing path — so `safety.CheckCurveAgainstTDP`, `VerifyFanCurveActive`,
the reconcile watcher and the profile edit-target rules all apply unchanged,
and there is no second way to write a curve that a future safety rule could
miss. The GUI is the same shape: choosing a preset only loads the editor, and
the commit bar writes it like any drag.
**They ride on `driver.FanShape`, which is also what they must satisfy.** A
preset holds exactly `Points` points inside `PWMMax`, so the curves and the
shape they are checked against are one value rather than two that can drift —
the same reasoning that puts `FloorCurve` and `StockProfilePPT` on
`PowerEnvelope`. `TestZ13PresetsParseAsUserCurves` runs each one through
`safety.ParseFanCurve` rather than re-checking the rules `Validate` checked,
because two copies of "what a legal curve is" is exactly how the preset path
could come to offer something the write path refuses.
**`limits.Sanitized` drops a malformed preset where it repairs a malformed
floor**, and the asymmetry is deliberate: a floor is a safety rule, so a
suspect one is worth keeping in degraded form, while a preset is a
convenience — offering the user a silently *repaired* curve under a name the
device chose would put our arithmetic behind the device's label. It is also
the one field `Sanitized` does **not** fill in from `DefaultLimits` when
empty, because absence is a real answer here: a device that declares no
presets has none, and substituting another machine's curves would offer a fan
profile designed for hardware the user is not running.
**The Z13's three are starting points, not measurements** — the first block in
that device file that is not — and the TOML says so. Two facts do constrain
them, both load-bearing and both tested: Quiet and Balanced hold **0** across
the idle range, since zero-RPM survives only while the curve commands 0 there
(a nominally gentle curve starting at 15% is *louder* at idle than firmware
auto); and Turbo is the only one that already clears `floor_curve` at every
point, so raising the sustained limit past `tdp_max_safe` leaves it exactly as
drawn. `TestZ13TurboIsTheHighTDPReadyPreset` asserts *both* halves — turbo
accepted and unaltered, the other two refused — so a future edit that made
every preset clear the floor would fail rather than quietly making the
device file's prose wrong. Verified on hardware at 80 W sustained: quiet came
back raised to the floor with the notice printed, turbo byte-for-byte as
declared.
`full:custom` re-baselined 20 → 23 (the row is `11:0/1/2:fan`, above the
chart because it is above it on screen); the other five focus lines were
byte-identical.
