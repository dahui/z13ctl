# On some machines a custom fan curve must be released before sleep, or the fans never stop

**On some machines a custom fan curve must be released before sleep, or the
fans never stop.** The Z13 has no `deep` in `/sys/power/mem_sleep` — only
`s2idle` — so the EC keeps running its fan loop for the whole suspend. Firmware
auto (`pwm_enable=2`) is what makes it stop the fans; where `pwm_enable=1`
survives into the suspend, the EC goes on driving them from the curve and never
learns the machine is asleep. Any curve with non-zero low-temperature points then
runs the fans all night, and `FloorCurve` guarantees it — every point is at
or above `HighTDPMinPWM`. Reported as "fans not turning off when I close the lid"
on the issue #15 thread (Fedora), and the docs asserted the *opposite* ("custom
PWM curves reset to firmware defaults on sleep") for as long as the bug existed.
**"On some machines" is load-bearing, and was established the hard way.** On the
CachyOS development machine the curve is dropped across the suspend regardless,
so the fans spin down with the daemon stopped and the release changes nothing
observable. The release is therefore written to be *harmless where unnecessary*
rather than conditional on detecting which case applies — there is no reliable
way to ask "will this kernel keep my curve through s2idle" ahead of time.
`--no-sleep-release` is the escape hatch.
