# `sleepTick` is gated on ownership, and that is what makes sleep and resume symmetric

**`sleepTick` is gated on ownership, and that is what makes sleep and resume
symmetric.** `sleepObs.Owned` is `ok && !active.Empty()` from
`ActiveCustomProfile()` — exactly the condition `restoreVolatileState` restores
under — so the invariant holds both ways: **the sleep hook releases only what
`applyCustomHW` will put back.** A hardware-only gate would be a one-way door:
a curve set by asusctl while voltaire sits on a firmware profile also reads
`pwm_enable=1`, so releasing it (let alone lowering its PPT) leaves nothing on
the resume side to restore either. `reconcileTick`'s `!obs.Custom` gate exists
for the same reason. Mode 0 and an unreadable mode are left alone, mirroring
`reconcileTick` branch for branch. `internal/daemon/resume_test.go` pins both
the table and the symmetry.
