# `safety.Engine.ReadEffective` must be passed the *effective* profile

**`safety.Engine.ReadEffective` must be passed the *effective* profile**, not
`platform_profile`. `platform_profile` is never "custom", so passing it makes a
legitimate custom TDP equal to the boot cache (`safety.CacheStale`: 5 W on
asus-nb-wmi, every limit at its default on armoury) indistinguishable from it,
and the stock table gets reported instead of the real values. The daemon passes `d.effectiveProfile()`; `cmd/` uses
`effectiveProfileForTDP()`, which asks the daemon first and falls back to sysfs.
