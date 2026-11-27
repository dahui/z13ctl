# The fan-curve editor draws the operating point, and it marks what the curve *commands* rather than what the fans are doing

**The fan-curve editor draws the operating point, and it marks what the curve
*commands* rather than what the fans are doing.** The dashed vertical at the
live APU temperature says *where you are*; the dot where it meets the curve
says what that means, which is the half a curve editor exists to show —
reading a duty cycle off a line by eye is the work the chart should be doing.
It is deliberately not the effective value: the curve under edit may not be
the one in force, and the high-TDP floor (drawn separately) can raise it, so
claiming the applied duty cycle would need state this widget does not have.
`limits.PWMAt` is `FloorPWMAt` under the name the arithmetic always deserved —
piecewise-linear interpolation over any non-decreasing point list, never
specific to the floor. `FloorPWMAt` now delegates, and
`TestPWMAtAndFloorPWMAtAgree` keeps them one function, because if they diverge
the editor's dot and the daemon's floor stop agreeing about what a curve says
at a temperature.
