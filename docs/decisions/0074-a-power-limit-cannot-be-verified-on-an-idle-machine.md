# A power limit cannot be verified on an idle machine

**A power limit cannot be verified on an idle machine.** Twice in one session
the pm_table PPT rails were read at idle and concluded to be inert — first that
they did not track our writes at all, then that `ppt_pl1_spl` specifically
reached nothing. Both were artifacts of the method: a *sustained* limit has
nothing to bind against when the package draws 12 W, so the SMU's limit
registers report firmware defaults (84/84/70 here) regardless of what was
written. Under 32-worker load with RAPL as ground truth the same attribute is
unambiguous — `pl1_spl` 52 → 35 → 20 measured 70.0 → 48.9 → 27.3 W package —
so `safety.Engine` reasons about the right number and the fan floor is sound.
Measured power sits *above* the set PL1 because STAPM is a moving average and
RAPL package covers SoC/iGPU/uncore beyond the rail STAPM governs; direction
and magnitude are the signal, not equality. Any future PPT or SMU
investigation starts by putting the machine under sustained load.
**What is known about `pm_table` here**, for the telemetry work that is
planned but not started (`.claude/plans/pm-table-telemetry.md`):
`/sys/kernel/ryzen_smu_drv/pm_table` is version `0x64020C`, 3664 bytes
(`0xE50`, 916 float32), mode `0444` — the one SMU path needing no grant.
STAPM, PPT-fast and PPT-slow limits sit at float index 0/2/4 (byte
`0x0`/`0x8`/`0x10`); those names come from Jeff's prior OneXPlayer work
(GitHub `dahui/onexplayer-x2-mini-pro-cachyos`, `docs/ryzen-smu.md`, whose
`0x64010C` differs only in the middle byte), and the first attempt here
declared the table unnameable for want of checking it. Under load
`ppt_pl1_spl` and `ppt_fppt` drive stapm/fast and `ppt_platform_sppt` drives
the slow rail. A pm_table read is an SMU transaction through the same mailbox
as the Curve Optimizer and must be serialised with it — that driver shares one
argument buffer across mailboxes.
**Every read is an SMU message** (ryzen_smu `smu_read_pm_table`: a read more
than 1 ms after the last sends `TransferTableSmu2Dram` over RSMU), so 2.0 ships
**the parser only, with no sampler** (Jeff, 2026-10-09):
`amd/pmtable.go`, per-version layout, unknown version and short buffer
refused, implausible fields dropped alone. The fixtures in `testdata/` were
captured under `yes` ×32 on armoury: indices 1/3/5 are the readings (all
within 0.1 W of RAPL), index 4 — the slow limit — is the sustained limit in
force (RAPL settles on it; 30/32/45 written reads 32, armoury's floor), and
at 50/50/50 all three limits read 50. The **first read returns the previous
transfer's table** (stale against RAPL and the next read), so any future
sampler discards it. The negative control (swapping two indices) fails
`TestPMTableAgreesWithRAPL`.
