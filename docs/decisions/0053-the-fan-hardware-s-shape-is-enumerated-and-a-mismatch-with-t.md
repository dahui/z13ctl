# The fan hardware's shape is enumerated, and a mismatch with the device data fails closed

**The fan hardware's shape is enumerated, and a mismatch with the device
data fails closed** (2026-10-09). The driver lists hwmon attribute names —
`pwm<N>_auto_point<k>_temp` on the curve device, `fan<N>_input` on the
readings device — so how many curve channels, points per curve and fans
there are is the kernel's answer; listing a directory reads no attribute, so
none of it touches the EC. Channels that disagree on their point count are
an error, never a guess. `fanController.Shape()` lays the kernel's count over
the device data on first use; when the two differ, the *data's* shape stands
with a warning, because the presets and `floor_curve` are sized for it, and
`SetFanCurves` refuses any curve not sized for the kernel's — so every curve
write fails, and with it any TDP that needs the floor, while a release still
works. `FanShape.Labels` (served as `fans.labels`) names each fan: the TOML's
`fans.labels`, else the kernel's `fan<N>_label` tidied ("cpu_fan" → "CPU
fan"), else "Fan N". The Z13 declares "Fan 1"/"Fan 2" because asus-wmi's
`cpu_fan`/`gpu_fan` describe a discrete GPU it does not have.
On the GUI side `limits.Curve` is a slice whose length is the served
`fans.points`, with the editor's axis on the served `pwm_max`. `FitCurve` is
the one gate a curve from outside enters the editor through: the old
`copy(points[:], pts)` into a fixed `[8]` array silently truncated a longer
curve and left the previous curve's tail under a shorter one.
There is deliberately **no `fans.shared_curve` key**. The audit proposed one,
but nothing would read it — the interface writes one curve to every fan — and
a declared-but-unread field is the failure the "capability the document
declares must be one the driver actually reads" entry warns about. A device
needing per-fan curves needs an interface change.
