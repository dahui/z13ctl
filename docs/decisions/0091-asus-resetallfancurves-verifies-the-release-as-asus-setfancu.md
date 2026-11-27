# `asus.resetAllFanCurves` verifies the release, as `asus.setFanCurves` verifies the curve

**`asus.resetAllFanCurves` verifies the release, as `asus.setFanCurves` verifies the
curve.** It was a bare `setAllFanModes(2)`, so a release the driver silently
ignored was indistinguishable from success. That became load-bearing with the
pre-sleep release: firmware auto is what lets the EC stop the fans through
s2idle, so an unnoticed failure is the difference between a quiet suspend and a
machine that runs its fans all night. `verifyFanModeReleased` accepts mode 0
(forced full speed is not a curve, so there is nothing left to release) and an
unreadable channel, exactly as `VerifyFanCurveActive` does.
