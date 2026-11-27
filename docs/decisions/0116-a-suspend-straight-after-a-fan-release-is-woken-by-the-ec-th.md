# A suspend straight after a fan release is woken by the EC; the hook holds it for the device's settle (3 s on the Z13) first

**A suspend straight after a fan release is woken by the EC; the hook holds it
for the device's settle (3 s on the Z13) first** (issue #24, ported from
z13ctl 1.4.2). The control run above was right
that z13ctl was not behind *that* wake, but it was on CachyOS and only checked
aborted suspends. A Fedora 44 user saw several sleep/wake rounds before the
machine stayed asleep, and it reproduces here once you look: RTC-timed 20 s
suspends on the GZ302EA (2026-10-09) woke after 1–2.5 s with 0.2 s of hardware
sleep in **7 of 15** cases when the suspend was handed to logind straight after
the `pwm_enable=2` release, **0 of 7** with a 3 s hold, and **0 of 10** with no
release. Turning a curve *on* (`pwm_enable=1`) raises no EC event at all, which is
why only the release side needs the hold. With `pm_debug_messages` on, the early wake is IRQ 9 (the EC's SCI)
and IRQ 1 firing together, then `ACPI: PM: Wakeup after ACPI Notify sync`: an
EC query's AML sent a Notify that a driver turned into a wakeup. It is not the
release's own event burst — three or four `_QDA` queries, over within ~230 ms
while awake, and a 300 ms wait for that burst still woke **4 of 6**. The
mechanism behind the cycling: the machine wakes, the resume puts the curve back,
logind re-suspends the lid-closed machine, the release runs again, and it wakes
again. The early wake comes in streaks (one batch of three slept fine), so any
re-measurement needs a dozen suspends, not two.
The duration is **device data** — the rule for any new number measured on one
machine, rather than a daemon constant: `fans.sleep_release_settle_ms` in the device
file, carried on `driver.FanShape.SleepReleaseSettle` and read by
`d.releaseSettle()`. A device with no measurement holds nothing; the
measurements are recorded next to the value. `Validate` keeps it under 5000 ms.
The hold is capped by logind's `InhibitDelayMaxUSec` (read over DBus, 5 s by
default) minus the work already done and `inhibitMargin`, because logind
suspends regardless once the budget is spent. `releaseHold` is the pure
arithmetic.
To diagnose a report, the daemon logs a **wake report** on every resume
(`internal/wakeup`): per-source `wakeup_count`/`event_count` deltas from
`/sys/class/wakeup` (opaque names like `device:44` labelled with their device),
SCI and GPE deltas from `/sys/firmware/acpi/interrupts` (the EC is GPE 0x0a
here; the kernel logs `ACPI: EC: GPE=` at boot), `pm_wakeup_irq` resolved
through `/proc/interrupts`, `last_hw_sleep`, and the suspend success/fail
deltas. The snapshot is taken after the hold, just before the inhibitor closes,
so anything counted happened after everything voltaire did. The sleep side logs
its work time, whether it wrote fans or PPT, and the hold. Every file read is
in-memory bookkeeping or a GPE status register, world-readable and safe while
`d.ecWedged` is set; deliberately not `/sys/power/wakeup_count`, whose read
blocks while events are in progress. `slept` strips the monotonic reading,
which does not advance in s2idle. Note that `woken_by` usually reads "not
counted" for this wake, since the Notify path does not charge a wakeup source;
`hw_sleep` and `slept` are what show it. Ask a reporter for `journalctl --user
-u voltaire -b | grep -E "entering sleep|held the suspend|handing the
suspend|wake report"`; for kernel-side attribution, `echo 1 >
/sys/power/pm_debug_messages` and `file drivers/acpi/ec.c +p` in
dynamic_debug.
