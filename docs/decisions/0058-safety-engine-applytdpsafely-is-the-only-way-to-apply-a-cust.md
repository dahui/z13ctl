# `safety.Engine.ApplyTDPSafely` is the only way to apply a custom TDP, and it takes the curve the caller intends to run

**`safety.Engine.ApplyTDPSafely` is the only way to apply a custom TDP, and it takes the
curve the caller intends to run.** Five paths apply a TDP — `handleTDP`, the
`handleProfile` "custom" branch, daemon startup, resume, and `cmd/tdp.go`'s
no-daemon path — and they previously enforced the floor four different ways:
two warned and applied anyway, one raised power *before* raising the fans and
discarded the fan error with `_ =`, and only one refused. They all now call
`ApplyTDPSafely`, which fails closed: if the fan write fails the TDP is not
written at all. Passing `nil` for the curve asks for `FloorCurve` wholesale
and is right only when there genuinely is no curve; `cmd/tdp.go` has no profile
state and so passes `liveFanCurve(hw)`, or a `tdp --set` would discard a
curve the user set moments earlier.
Release order is the mirror image — lower power *first*, then release the
fans (`handleTDPReset`, the stock-profile branch, `cmd/tdp.go` reset), so the
machine is never at a high limit with no floor. `internal/safety/engine_test.go`
(call-order fake) and `internal/drivers/asus/tdp_test.go` (fake sysfs) guard this; the refusal case and the
"keeps a curve that already meets the floor" case are the two that matter.
