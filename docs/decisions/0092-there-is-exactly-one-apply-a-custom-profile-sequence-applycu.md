# There is exactly one apply-a-custom-profile sequence: `applyCustomHW`

**There is exactly one apply-a-custom-profile sequence: `applyCustomHW`.** The
socket command, the autoswitch watcher, `Run()`'s startup restore and
`resume.go` all call it. Four hand-rolled copies is precisely how the ordering
and clearing rules drifted apart; `handleTDP`'s post-lower fan step is the last
place that repeats any of it, and it mirrors the same rule deliberately
(restore the profile's curve, else release the fans, so lowering a limit and
selecting the profile converge on the same hardware).
