# `handleTDPReset` writes `platform_profile` even when already on `balanced`, and that is deliberate

**`handleTDPReset` writes `platform_profile` even when already on `balanced`,
and that is deliberate.** The redundant write costs a WMI call whose
fan-controller reset is immediately superseded by the fan release that
follows, so it is invisible — whereas guarding it would also skip the PPD sync,
since `linux.setProfileFor` only syncs power-profiles-daemon after a successful
primary write. `Run()`'s same-value guard exists because nothing else there is
touching the fans; that reasoning does not transfer to a command whose whole
purpose is to land on `balanced`.
