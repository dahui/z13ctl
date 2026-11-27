# Activating a custom profile *clears* the subsystems it does not set

**Activating a custom profile *clears* the subsystems it does not set.**
`applyCustomHW` releases the fans when the profile has no curve, resets the
Curve Optimizer when it has no offset, and hands the PPT limits back to the
firmware profile underneath when it has no TDP. Without that, switching from a
profile with a 90W limit and a -25 offset to one that sets neither leaves both
in force while the daemon reports the second profile — and A→B→A does not give
the same machine as A. This only became reachable once custom→custom switching
existed. Ordering is the fail-closed part and is not free to rearrange: the
profile's own curve goes on *before* its TDP so `ApplyTDPSafely`'s floor is
written last and wins; clearing the TDP lowers power before the fans are
touched; and the fans are released only when no high sustained limit is in
force, so a high-TDP profile with no curve of its own keeps the floor
`ApplyTDPSafely` just wrote. `Run()` calls the same helper rather than
hand-rolling the restore, so startup cannot drift from it.
That last condition is checked against **hardware** — `safety.Engine.CheckFanFloorRelease`
— and not only against `p.TDP`. `highTDP` is false whenever `ApplyTDPSafely`
failed, *including* the case where it succeeded at writing `FloorCurve` and
then failed at `SetTDPState`, so trusting the flag alone released the floor it
had just written one line earlier. The same guard also covers a limit left high
out-of-band, which cached state says nothing about.
