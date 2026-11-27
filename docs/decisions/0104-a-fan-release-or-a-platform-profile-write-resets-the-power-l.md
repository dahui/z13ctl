# A fan release or a `platform_profile` write resets the power limits, and sysfs cannot see it

**A fan release or a `platform_profile` write resets the power limits, and
sysfs cannot see it** (issue #22; ported from main's v1.3.5). On the Z13 any
`pwm_enable=2` write to the curve device — even a redundant one with the fans
already on auto — and any `platform_profile` write make the firmware re-apply
that profile's own limits, while the `ppt_*` readback keeps the last written
value. Measured under load with RAPL: TDP 30 W on performance held 30 W, a
redundant release took it to 70 W, and `pwm_enable=1` left it alone. A
curveless profile at or below the safe maximum wrote its TDP and *then*
released the fans (`applyCustomHW`, `handleTDP`, and `fancurve --reset` on a
running custom profile) — hence "75 W does nothing, 76 W works". Two rules
follow. **Every release goes through `device.Device.ReleaseFans(keep)`**, which
routes to `safety.Engine.ReleaseFans`: release, then re-apply `keep`, refusing a
`keep` that needs the floor before the fans are touched. `nil` means "the
firmware profile's own limits", right for every stock-profile path. The raw
`Device.Fans` is a `driver.FanCurves`, which has no `Release`, so a direct
release does not compile (the raw `FanController` reaches only the engine and
the device's private fallback; a source scan enforced this until 2026-11-26). **The reconcile watcher re-writes the
TDP on any observed policy write** — the kernel's notification,
`platform_profile` changing between ticks, or a custom curve found dropped —
because the drift check compares against the readback and can never fire for
this. On a curveless profile two writes leave no trace in sysfs: a same-value
profile write (power-profiles-daemon on a charger edge) and another tool
writing `pwm_enable=2` to fans already on auto. `watchPolicyWrites`
(`policywatch.go`) closes both: the profile driver names the attributes the
kernel `sysfs_notify`s (`driver.PolicyWriteNotifier`; asus returns
`/sys/firmware/acpi/platform_profile`, notified on every profile write, and
asus-nb-wmi's `throttle_thermal_policy`, notified by
`throttle_thermal_policy_write()` — the call both a profile write and a
curve-device release make, which is *why* a release resets the limits). The
watcher polls them for `POLLPRI` and sets `d.policyWritten`, which the next
tick consumes as `obs.PolicyWritten`. `throttle_thermal_policy` exists only
under `CONFIG_ASUS_WMI_DEPRECATED_ATTRS` (without it a foreign redundant
release is invisible again); reading it logs no deprecation notice, and armoury
PPT writes do not notify it, so a re-apply cannot feed itself. Our own fan
releases notify too and already re-wrote the limit, so a re-apply whose only
evidence is the notification is `act.Routine` and logs at Info. Verified on a
GZ302EA (z13ctl A/B and this port): 30–40 W custom on performance held through
foreign releases and same-value writes, where one takes the machine to 70 W.
Never treat a `ppt_*` readback
as proof that a limit is in force; the safety rig's `held`/`firmware` fields
emulate the reset for exactly this reason.
**A charger edge moves the readback too, and that re-apply is routine**
(2026-11-15). asus-armoury reads back the new source's table, which holds
whatever was last written on that source, so a custom profile's PL1 "drifts"
on every plug and unplug and the watcher re-writes it — correctly, but it was
logged as a WARN. `reconcileOnce` now reads the power source (`d.acPower()`)
on the ticks that read the limits, *after* them, and `reconcileTick` puts a
drift within `reconcileSourceGraceTicks` (2) of a source change down to it:
two because a plug between the two reads shows as the change on one tick and
the drift on the next. A tick that does not read the source forgets it, or a
latch left over from a stretch on a firmware profile would excuse a later
drift. `TestReconcileSourceChangeDriftIsRoutine`; both negative controls
(branch disabled, grace of 1) were run.
