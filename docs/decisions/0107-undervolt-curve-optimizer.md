# Undervolt (Curve Optimizer)

**Undervolt (Curve Optimizer)**: CPU voltage reduction via AMD Curve Optimizer,
using direct SMU communication through the `ryzen_smu` kernel module's sysfs
interface at `/sys/kernel/ryzen_smu_drv/`. The all-core CO command is the
CPU's, keyed on `/proc/cpuinfo` family/model (`coPlatforms`, transcribed from
ryzenadj 0.19.0's `set_coall` + `cpuid.c`; 2026-10-09): MP1 0x55 for
Renoir/Lucienne/Cezanne, MP1 0x4C for Van Gogh, Rembrandt, Phoenix, Hawk Point,
Strix/Krackan Point and Strix Halo (the only row run here). Dragon/Fire Range
(ryzenadj's PSMU 0x7) are left out until ryzen_smu's file for that mailbox is
checked. **A CPU not in the table is refused before the probe**, since the
probe is itself a CO write; `TestUnknownCPUNeverWritesTheMailbox` pins zero
mailbox writes, and its negative control (guess 0x4C) was run. The fork hint
on a 0xFE answer is per platform (`ForkNote`), and the dry run names the
command through the optional `driver.UndervoltCommander`. iGPU CO is not
offered (ryzenadj excludes Strix Halo from `set_cogfx`). Optional
dependency — gracefully disabled when the module is not installed.
`amd.smuProbeUndervolt()` sends a CO-0 probe before the first offset write to
detect whether the installed `ryzen_smu` fork actually supports CO commands on
this platform (the amkillam fork is required for Strix Halo; the leogx9r fork does
not work). CO values are volatile (reset on reboot/sleep); the daemon
reapplies them on startup and resume when the custom profile is active.
Safety limit: CPU 0 to -40. `--get` returns saved values from daemon state
plus the current profile (no sysfs readback exists for CO). `UndervoltState`
includes an `Active bool` field indicating whether the offset is currently
applied. Switching to a stock profile resets CO in hardware but preserves
saved values in state for recall; the CLI displays "(not active)" when a
stock profile is active. The `get-state` response includes
`undervolt_available` (`Undervolter.Available()`: module presence until
something has probed, the probe's answer after) so GUIs can hide controls when
ryzen_smu is not installed, or the wrong fork is present once a write has
found out.
