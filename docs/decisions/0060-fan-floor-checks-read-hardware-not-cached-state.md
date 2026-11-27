# Fan-floor checks read hardware, not cached state

**Fan-floor checks read hardware, not cached state.** `safety.CheckCurveAgainstTDP`
and `safety.Engine.CheckFanFloorRelease` both take the *effective* profile and go through
`safety.Engine.ReadEffective`. `handleFanCurve` used to gate on `d.state.TDP`, which
silently skipped the guard whenever state and hardware disagreed (a TDP set
while the daemon was down, a reset state file). `fancurve --reset` is refused
above 75W in both the daemon and the CLI: firmware auto has no floor, so
dropping to it removes exactly the protection the limit requires. A PPT *read*
failure is deliberately not a refusal — it must not make fan control
unavailable.
