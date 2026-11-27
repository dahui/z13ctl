# The post-resume restore runs off the signal loop, and a sleep edge cancels it (`sleepCycle`)

**The post-resume restore runs off the signal loop, and a sleep edge cancels it
(`sleepCycle`).** The loop used to run `waitForEC` (3 s settle, up to 20 s of
probing) and the restore inline, holding the delay lock it had just re-taken. A
`PrepareForSleep(true)` in that window — the lid closed again, or logind
re-suspending a machine that woke with its lid shut — sat unread; logind gives a
lock `InhibitDelayMaxSec` (5 s) and then suspends anyway, so the restore's
`pwm_enable=1` and PPT writes could land at the freeze, leaving the curve driving
the fans through s2idle, and the stale sleep signal was handled only after the
next wake. Now the sleep edge cancels a restore still waiting for the EC and
*joins* one already writing (it holds `hwMu`; the release then undoes it), so
the release is always the last write. A brief wake therefore writes nothing:
the fans were released before the previous suspend and nothing restored them, so
`sleepTick` finds mode 2. The pre-sleep `Lighting.Off` still runs on that path —
it is hidraw, not the EC, and the lightbar may have come back lit on its own. A skipped
restore leaves `d.suspending` set, as it already is between any two signals.
