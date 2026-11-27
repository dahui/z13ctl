# CPU boost is a capability of its own, not a firmware toggle, and the difference is who keeps the setting

**CPU boost is a capability of its own, not a firmware toggle, and the
difference is who keeps the setting** (`driver.CPUBoost`; Jeff, 2026-08-14).
It looks exactly like one: a 0/1 sysfs file, an on/off switch, a `--set 0`.
But a firmware toggle is a BIOS setting the *machine* keeps, which is why
nothing persists one — while cpufreq comes up boosting on every boot, so a
user's "off" survives only if the daemon replays it. Rendering it beside the
BIOS switches would have said the opposite about who is responsible, and
nothing would have restored it. It sits with fan curves, PPT and the Curve
Optimizer instead: recorded in state, restored in `Run()`.
**Only a stored `false` is replayed.** `State.CPUBoost` is a `*bool`, and nil
means the user never expressed a preference — writing the default back there
would be voltaire claiming a setting it was never given, and it is the same
absent-is-not-false rule `State.Features` follows for an unreadable toggle.
`get-state` reads the kernel rather than serving the stored value, because
anything with the grant can write that file; the stored value is an
*instruction*, not a cache. New pointer on `api.State` ⇒ `cloneState` got it.
**The write verifies.** `SetCPUBoost` reads back and errors when the value
did not take — amd-pstate refuses it in some modes and the write succeeds
anyway, which is the `asus.setFanCurves` lesson by another route. A failed
*readback* is deliberately not an error: the write probably landed, and the
caller's fallback is "unknown" regardless.
**The grant is service-only, and it is the first one that has to be.** Every
other target has a udev rule as a best-effort first pass with the perms unit
behind it; `/sys/devices/system/cpu/cpufreq/boost` is a plain kobject rather
than a device, so `udevadm info` answers "Unknown device" and no rule can
match it at all. `cmd/setup_test.go`'s grant table records that asymmetry,
and the negative control was run — deleting the line from the packaged unit
fails the guard. **Existing installs need `sudo voltaire setup` again**, or
the switch is there and every write is EACCES.
Measured on this machine: boost off drops `scaling_max_freq` from 5187500 to
3000000 across all 32 policies, and one write to the global file moves every
one of them.
