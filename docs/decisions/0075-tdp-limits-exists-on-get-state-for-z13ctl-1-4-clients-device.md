# `tdp_limits` exists on get-state for z13ctl 1.4 clients; `device-get` is where 2.0 serves the same ranges

**`tdp_limits` exists on get-state for z13ctl 1.4 clients; `device-get` is
where 2.0 serves the same ranges.** z13ctl 1.4.0 (main, `api/v1.3.0`) added
`State.TDPLimits` — backend, PL1/PL2/PL3 ranges and `safe_max` — so clients
could bound sliders by what armoury accepts. v2 had already put those facts in
the capability document (`power.interface`, `tdp_min`/`tdp_max_forced`,
`pl2`/`pl3`, `tdp_max_safe`), and the port carried the daemon side without the
wire field, so a 1.4-era client lost its bounds on 2.0 with nothing saying so —
the opposite of the 2.x promise that 1.x wire fields survive. Both now come
from one projection of `d.env()` (`tdpLimitsFrom` beside `deviceInfoForEnv`),
so they cannot disagree. It sits inside get-state's existing `!wedged` PPT
block, because on armoury the bounds are `_PSR` calls too, and goes through
`d.env()`'s 30 s cache so a 1 Hz poll does not re-read them. Absent with no
known interface, as on main: a backend-less range would be device data
presented as the kernel's answer. `TestTDPLimitsWireKeys` pins the JSON names,
which are the entire contract for a non-Go client.
