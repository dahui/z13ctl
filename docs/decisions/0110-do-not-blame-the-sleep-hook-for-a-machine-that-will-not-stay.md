# Do not blame the sleep hook for a machine that will not stay asleep

**Do not blame the sleep hook for a machine that will not stay asleep.** That
was diagnosed twice, wrongly, during development: first as an SD card whose
`mmc_bus_suspend` returns -84 (real, but present in only one of eight failures
and harmless on its own), then as our `ppt_*`/`pwm_enable` writes provoking a
delayed EC notification. The control run killed both — suspend aborted
identically with the daemon **stopped**. The inference that led there was
"suspends were minutes-long in earlier boots and seconds-long today, and today
is when the hook landed"; it was unsound because there were no suspends that day
*before* the change, so the comparison varied in more than the code. A suspend
that aborts before `Freezing user space processes` had a wakeup event already
pending; `/sys/power/pm_wakeup_irq` and `/sys/kernel/debug/wakeup_sources`
(the `wakeup_count` column) name the source, and on the Z13 the touchscreen
(`i2c-ELAN9008:00`) and the detachable cover are both wakeup-enabled.
