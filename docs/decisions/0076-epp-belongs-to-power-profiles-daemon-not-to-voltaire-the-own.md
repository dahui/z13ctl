# EPP belongs to power-profiles-daemon, not to voltaire — the ownership test

**EPP belongs to power-profiles-daemon, not to voltaire — the ownership
test.** Power control is layered: `platform_profile` (EC thermal policy) →
the PPT limits (a package power *ceiling*) → EPP (a per-core *bias* under that
ceiling, applied sub-millisecond by CPPC) → boost and `scaling_{min,max}_freq`
(clamps). voltaire owns a knob only when **nothing else replays it**. Boost
passes — cpufreq comes up boosting on every boot and nothing restores a
user's "off", hence `driver.CPUBoost` and its persisted state. EPP fails:
power-profiles-daemon rewrites it on every profile change, which is the same
event our `platform_profile` write causes, so a voltaire EPP setting would be
overwritten by the write that accompanied it, or would fight PPD. voltaire
already moves EPP — through PPD, by selecting the profile (`linux.setProfileFor`
syncs `powerprofilesctl`). The z13ctl-plus fork's EPP control is therefore
redundant where PPD runs and a write-fight where it does; do not add one
without a machine on which nothing else owns it.
