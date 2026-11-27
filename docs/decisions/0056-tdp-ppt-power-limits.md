# TDP (PPT power limits)

**TDP (PPT power limits)** go through asus-armoury whenever it exposes them,
and through the deprecated `/sys/devices/platform/asus-nb-wmi/ppt_*` only as a
fallback (`internal/drivers/asus/tdp.go`; ported from z13ctl v1.4.0, issue
#22). Those exist only under `CONFIG_ASUS_WMI_DEPRECATED_ATTRS`, so a
distribution can build them out, and *reading* them logs the kernel's
once-per-boot deprecation notice. The two write the same WMI device IDs and
each keeps its own cache, so a read or write never mixes them. The interface is
chosen per call (`activePPT`), because armoury keeps separate AC and battery
tables (on the GZ302EA the min/max agree and only the defaults differ: 60/75/86
AC, 45/52/71 battery). Each armoury read is a `_PSR` evaluation, so reads are
kept down: selection reads only PL1's two bounds, the rest load on demand
(`withBounds`, for the envelope and clamping), `Engine.ReadEffective` builds the
envelope only when the profile has a stock row to substitute (custom profiles,
what the watcher reads, never do), and `d.env()` reuses a read for
`envCacheTTL` (30 s). Armoury is chosen only when PL1's `current_value` is
writable by this process: an install that has not re-run `voltaire setup`
since the grant was added keeps writing asus-nb-wmi's attributes rather than
failing every TDP write with EACCES. armoury's PL3 is `ppt_pl3_fppt`, not `ppt_fppt`, and it
exposes no APU/Platform sPPT, which then read as 0 and are never written.
**The kernel's ranges reach everything through the envelope**:
`powerLimiter.Envelope()` lays the active interface over the device file's —
on armoury `TDPMin`/`TDPMaxForced` become PL1's 28–80, and `PL2`/`PL3`,
`NoSPPTMirrors`, `Interface` and `Initial` (the defaults, the boot cache) are
set; on asus-nb-wmi the device file's 5–93 stand. `device-get` serves the
ranges, and the drawer bounds each slider by its own (`limits.PL2Range`/
`PL3Range`). `safety.ResolveTDP` is the one validation the CLI and daemon share:
PL1 outside its range is refused, PL1 above `TDPMaxSafe` needs force, and
PL2/PL3 below their minimum are *raised* with a note (`--set 30` stores
30/32/45) — and `limits.NeedsAdvanced` compares against `BasicTriple`, or every
low basic-view save would flip the drawer to advanced. The raise is not
cosmetic: 30/32/45 held 32 W under load for four minutes (z13ctl, measured
2026-10-08), so 32 W is armoury's practical floor. `SetTDPState` clamps into
armoury's range and logs, so a profile saved under the old 5 W floor still
applies and the stored value is untouched. **Anything comparing a wanted TDP
with a readback must compare `safety.EffectiveTDP(env, want)`** — the
reconcile tick does, or a 15 W profile reading back 28 W would be re-written
every two seconds forever; `customLimitInForce` likewise. When the **sustained** limit
(PL1) exceeds 75W, both fans must be on a curve that holds a 50% PWM floor,
written with `pwm_enable=1` before the PPT writes, and the TDP is not applied at
all if that fails (see `ApplyTDPSafely`). The floor is applied per point against
`FloorCurve` — the 50% bottom ramping to 100% at 80°C: a curve's points are
raised to that curve's value wherever they fall below it and left as drawn
wherever they do not, and it is written whole only when the profile has no curve
of its own (see `FanCurveForTDP`). The bottom of that curve was
80% (204) through v1.2.1 and users reported it as loud enough that they simply
stopped using high TDP, so it is now 127; the *ramp* is what protects the APU,
since a machine actually sustaining >75W is well past 60°C where the curve is
far above the floor anyway. Burst limits alone do not trigger it.
`SetAllFansFullSpeed` (`pwm_enable=0`) is an earlier strategy that nothing
calls any more; the docs, the dry-run output, and this file all described it
for far longer than the code did. It would not have worked as described
either: measured 2026-10-09, base-device `pwm1_enable=0` takes fan 1 to 8900
RPM and leaves fan 2 stopped. A 255-everywhere curve on the curve device
drives both (8600–8900 RPM, the Z13's `fans.rpm_max = 8900`, after ~40 s of EC
ramp at ~250 RPM/s). APU sPPT and Platform sPPT always follow PL2.
