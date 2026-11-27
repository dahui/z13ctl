# Every route to a stock profile must clear the undervolt

**Every route to a stock profile must clear the undervolt.** `handleProfile`
did; `handleTDPReset` and `cmd/tdp.go`'s `runTdpReset` did not, even though
both land on "balanced". That left CO applied in hardware with
`Undervolt.Active` still true while the daemon reported a stock profile — the
same "custom setting leaks into a stock profile" defect as #12. Saved values
are still preserved for recall; only `Active` and the hardware are reset.
`setUndervoltActive(state, false)` stamps every saved profile, since CO is
global hardware and at most one profile's offset can be applied. `Active` is
set true in exactly one place — `applyCustomHW`, and only when the SMU write
succeeded — so a profile copied while CO was live never claims to be applied
before anything was written.
