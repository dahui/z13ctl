# Fan-floor checks read hardware for the *live* profile, and the profile's own TDP for any other

**Fan-floor checks read hardware for the *live* profile, and the profile's own
TDP for any other.** `safety.CheckCurveAgainstTDP`/`CheckFanFloorRelease` go through
`safety.Engine.ReadEffective`; their pure siblings `CheckCurveAgainstTDP` and
`CheckFanFloorReleaseAt` take a limit directly, which is what an inactive
profile needs — hardware says nothing about a profile that is not running. The
effect is a *stronger* invariant than before: a profile can never be stored in
a state that would be unsafe the moment it is activated, and `ApplyTDPSafely`
still fails closed at activation.
