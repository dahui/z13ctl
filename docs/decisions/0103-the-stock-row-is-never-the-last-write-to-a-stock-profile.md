# The stock row is never the last write to a stock profile

**The stock row is never the last write to a stock profile** (issues #12,
#22; ported from main's v1.3.5). The `ppt_*` attributes have no "reset to
firmware default" operation — writing 5W (an earlier attempt) just crippled
the machine — so #12 made the envelope's `StockProfilePPT` authoritative on
write, on the belief that the firmware does *not* re-apply per-profile PPT on
a `platform_profile` write. Measured 2026-10-08 on BIOS WMI 9.4 it does (see
the next note); the `ppt_*` readback, which never moves, is the likely source
of the original conclusion. And the row is *looser* than the firmware: only
PL1 matches. Under load, balanced held 52 W on its own limits and 63–66 W for
the minute after the row was written; quiet 40 W against 55–70 W, at up to
90°C rather than 75. Every daemon start on a stock profile wrote the row last,
so every login ran the stock profile hotter than the firmware does.
`safety.Engine.HandBackToFirmware` (via `device.HandBackToFirmware`) is now
how a stock profile is entered — `applyStockHW`, `tdp --reset` (daemon and
CLI), `profile --set` without the daemon, `applyCustomHW` for a profile with
no TDP, and startup. It writes the row (`RestoreStock`, exact values — the row
is what lowers a high custom limit before the fans lose their floor, and what
the attributes then show), refuses to release while hardware still reads
above the safe maximum, and releases last; the release is what re-applies the
firmware's limits. A redundant release on fans already at auto does not dip
them (measured), unlike the same-value `platform_profile` write `Run()` guards
against, so startup may release — but only when the fans are already on auto
or the curve is the custom profile it just left; a foreign curve gets the row
alone (`restoreStockPPT`). `applyCustomHW` hands back *before* writing the
profile's curve, because `pwm_enable=1` leaves the limits alone and the
release would drop the curve. The saved custom TDP is never cleared on a
profile switch. The engine test rig's firmware deliberately differs from its
row so a test ending on the row fails.
