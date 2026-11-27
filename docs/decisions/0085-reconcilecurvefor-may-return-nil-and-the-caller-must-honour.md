# `reconcileCurveFor` may return nil, and the caller must honour that rather than substituting the tick's own `act.Curve`

**`reconcileCurveFor` may return nil, and the caller must honour that rather
than substituting the tick's own `act.Curve`.** `reconcileOnce` writes the TDP
first and then recomputes the curve against the limit that write established;
`act.Curve` was computed against the *drifted* limit, so falling back to it
reintroduced the stale answer. A curveless custom profile whose PPT had wandered
above `TDPMaxSafe` fired both arms, the TDP arm restored the profile's own 52 W,
and the fallback then wrote `FloorCurve` anyway — pinning both fans to a
50% minimum on a profile that controls no fan curve at all. It stuck, because
the next tick read `pwm_enable=1` as "the curve is live" and the TDP now
matched. nil means "the limit imposes nothing and the profile has no curve;
leave the fans where they are". The helper is pure so the table can cover it
without writing the developer's fan controller.
