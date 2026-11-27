# A resume that times out on a wedged EC latches `d.ecWedged`, and every path into the EC stands down on it — not just the resume apply

**A resume that times out on a wedged EC latches `d.ecWedged`, and every
path into the EC stands down on it — not just the resume apply.** (Ported from
main's v1.3.3: `c92ec8e`, PR #26, and `b3b3c77`.) `d.probeEC()` classifies the
error from `d.hw.Battery.Status()`, whose first read is the capacity attribute
and comes back unwrapped: `ENODEV` on an existing attribute is a wedged EC,
`ENOENT` is no battery, and a device with no battery capability reports ready.
`restoreVolatileState` skips `applyCustomHW` when wedged: a WMI write into an EC
that is not answering blocks in `acpi_evaluate_object` holding the ACPI global
mutex, and the machine hard-locks. Skipping that one call was PR #26's first
version and was not enough. The sleep hook had released the curve, so two
seconds later `reconcileTick` would have written it straight back into the
stalled EC. The latch is consulted by `reconcileTick`, `powerSourceOnce`
(returns `prev`, as for `suspending`), `sleepTick` and — v2 only —
`telemetryTick`. That last gate is a **safety** property, unlike the suspend
gate beside it: `Telemetry.Sample()` reads fan RPM over asus-wmi once a second,
which would make the sampler the most frequent thing walking into the EC.
`dispatch` refuses the commands in `ecGuarded` with `errECWedged` (v2 adds
`feature`, `feature-get` and `tuning-reset` to main's list). `get-state` skips
`batteryStatus`, `readFeatures`, `Telemetry.Sample`, the `Fans.ReadRPM`
fallback, and the PPT readback. **asus-armoury PPT reads are live `_PSR`
calls** (see the firmware-attributes note), so with the armoury port `tdp-get`
joined `ecGuarded`, `reconcileOnce` returns before observing, the sleep hook
skips its PPT read, and `d.env()` stops asking the driver for the envelope — it
serves the last one read while the EC answered, or the device data alone
(`driver.DeviceEnvelope`); `device-get` goes through it. z13ctl 1.4.0 shipped
without these; 1.4.1 fixed it. **The CLI must not read the limits
itself while a daemon is up**: `tdp --get` and `status` ask the daemon
(`tdp-get`, refused while latched), `tdp --set` and the dry runs validate
against the daemon's `device-get` envelope (`powerEnvFor`), and the
`fancurve --set`/`--reset` floor checks run only on the dry-run and no-daemon
paths, the daemon making the same check itself. A CLI read would walk into the
stalled EC the daemon is avoiding. The reconcile watcher also reads the limits
only for a custom profile it is not standing down for: `suspending` covers the
post-resume `waitForEC` window, up to 20 s before the latch is set, during
which the tick stands down anyway. Reads count because the PR #26 trace's mutex holder was asusd
*reading*. `Run()` probes before `restoreHardwareAtStartup`, because a daemon
restarted after a bad resume would otherwise write everything with the
in-memory latch gone. There is no ceiling: `reconcileOnce` probes once per tick
while latched, and only a clean read clears it and runs the held-back
`applyCustomHW`. **`ENOENT` is "no battery" only if there was none before the
suspend.** `watchResume` records `d.probeEC() != ecAbsent` on each
`PrepareForSleep(true)`, and `ecStatusAfterSleep` turns a post-resume
`ecAbsent` into `ecWedged` when it was true. That is the probe itself rather
than main's `cli.HasBattery()`, so the driver interface needed nothing new.
`autoswitch-get` reports the source as unknown while latched rather than
calling `acPower()` (AC `_PSR`), as main has since v1.3.4. It was once left
ungated on the grounds that `Battery.Status()` fails on the capacity read
before it reaches AC — true of today's driver, but an accident of read order,
and `get-state` already skips `batteryStatus` while latched, so the two now
agree. Audit by *what is read*, not by whether a command mutates. A new
watcher, socket command or `get-state` field that reaches the EC needs the
gate.
