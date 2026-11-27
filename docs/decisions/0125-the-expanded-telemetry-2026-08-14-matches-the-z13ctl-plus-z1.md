# The expanded telemetry (2026-08-14) matches the z13ctl-plus/z13gui-plus data set, through the daemon

**The expanded telemetry (2026-08-14) matches the z13ctl-plus/z13gui-plus
data set, through the daemon.** GPU edge temp, busy %, sclk, GFX power and
UCLK (amdgpu sysfs + a pure gpu_metrics parser; see the revision entry), CPU
utilisation counters + average core clock + system memory (procfs/cpufreq),
VRAM carveout, and NPU power/util/clock (amdxdna DRM ioctls, layouts ported
from the -plus fork, decoded at explicit offsets because 168-byte records
misalign struct casts). The forks read sensors live inside get-state with no
history; ours flow driver `Sample()` → 1 Hz sampler → ring → wire, so every
quantity charts. Three rules earned their comments: (1) **the NPU is queried
only while `runtime_status` reads active** — opening the accel node resumes
a suspended NPU, so an unconditional 1 Hz query would pin it awake forever, a
power cost imposed by the power graph; a suspended NPU reports *zeros with
Known=true*, since suspended genuinely means drawing nothing and a chart
that gapped whenever the NPU slept would look broken on every machine not
running inference. (2) CPU utilisation crosses the boundary as **cumulative
jiffie counters** on the energy-counter pattern (`cpuUtilPct` in the sampler,
`prevJiffies` owned by its goroutine); unlike energy it needs no gap ceiling
— jiffies only advance awake. (3) Presence per field by what zero means:
utils and GPU/NPU power are wire *pointers* (idle is genuinely 0), temps and
clocks omit zero (never a reading). `telemetry.{gpu,cpu_stats,npu,net}` are
declared in the device TOML and the declaration guard now holds all five
sources both directions. `State.Telemetry` is the full live edge as one
nested sample (freshness-bounded; derived rates grafted from the ring) so
the every-quantity-live rule holds without a top-level field per quantity —
the pre-2.0 named fields stay forever. New State pointer ⇒ `cloneState` got
`cloneTelemetrySample`.
**Network throughput (the eighth card; Jeff, 2026-08-14, picked to even the
grid) is the same shape end to end**: `/proc/net/dev` byte counters cross
the driver boundary cumulative (`netRateMBps` in the sampler, `prevNet`
owned by its goroutine, `maxEnergyGap` ceiling — wall-clock interval, unlike
jiffies), and the rates are wire pointers because an idle link's 0.0 MB/s is
a reading. The part with judgement in it is *which interfaces count*:
`ReadNetBytes` sums only interfaces with a `/sys/class/net/<name>/device`
link — hardware-backed — because summing everything counts VPN and bridge
traffic twice (once on the tunnel, once on the hardware beneath it), and no
physical interface at all is an error, never a zero.
