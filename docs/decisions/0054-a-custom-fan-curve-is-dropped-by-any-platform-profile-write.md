# A custom fan curve is dropped by any `platform_profile` write

**A custom fan curve is dropped by any `platform_profile` write** (issue #15).
`throttle_thermal_policy_write()` — which every profile write goes through —
ends by clearing `custom_fan_curves[*].enabled`, and `fan_curve_write()` then
returns early on `!enabled`. Nothing is reported to the process that set the
curve. On a GNOME desktop, power-profiles-daemon writes `platform_profile` on
every AC/battery transition and on any PPD hold, so a curve set by voltaire stops
working minutes later for no visible reason; Fn+F5, asusctl and tuned do the
same. Two halves fix it: `asus.setFanCurves` reads `pwm_enable` back
(`VerifyFanCurveActive`) so a dropped curve is an error rather than a false
success — which is also what makes `ApplyTDPSafely` genuinely fail closed — and
`internal/daemon/reconcile.go` polls the curve device's `pwm_enable` every 2s
and re-applies. It polls the enable flag rather than `platform_profile` because
`fan_curve_enable_show()` returns the driver's cached `enabled`, making it
ground truth and catching every cause rather than the one we predicted. It
never writes `platform_profile` (that would be a write-fight with PPD over
every AC transition) and acts only while the active profile is a custom one
(`obs.Custom`, from `state.ActiveCustomProfile()`), so a deliberate
firmware-profile switch is left alone by construction. Named profiles are
defended exactly as `custom` is; a reserved name never is.
