# `gpu_metrics` is read through a per-revision offset table, and GFX power only where it is the GFX rail

**`gpu_metrics` is read through a per-revision offset table, and GFX power
only where it is the GFX rail** (2026-10-09). The offsets are `offsetof` over
the kernel's `struct gpu_metrics_vF_C` (`kgd_pp_interface.h`), compiled
rather than counted, and the 3.0 result matched the existing 124/186. UCLK is
`MemclkFrequency` in MHz from every producer, so 2.0–2.4 read it. GFX power
is *not* read from 2.1–2.3, although the field is there: which rail fills it
varies by chip within one revision — Rembrandt and Phoenix (2.1) copy
`Power[0]`, VDDCR_VDD, the rail the CPU cores share with graphics; Renoir
(2.2) leaves it at the driver's all-ones fill — so charting it as GPU power
would be false on exactly the machines it would appear on. 2.4 (Van Gogh,
`Power[2]` = VDDCR_GFX) and 3.0 ("time filtered GFX power") are the GFX rail.
An unknown revision is refused, and all-ones in either width is
"unavailable". Everything else on the GPU comes from **one card**: the first
amdgpu DRM card, and the hwmon *under that card's device* rather than the
first amdgpu hwmon on the system; temperature and clock channels are chosen by
label (`edge`, `sclk`; channel 1 when nothing is labelled). The clock read used
to look for `device/freq1_input`, which does not exist — the file is
`device/hwmon/hwmonN/freq1_input`, in Hz — so the P-state fallback was the only
path that ever ran. The NPU is the accel device bound to `amdxdna` (its ioctls
are amdxdna's), not any device with AMD's vendor ID.
