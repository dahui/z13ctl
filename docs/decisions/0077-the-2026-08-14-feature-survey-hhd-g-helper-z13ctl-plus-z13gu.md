# The 2026-08-14 feature survey (HHD, G-Helper, z13ctl-plus/z13gui-plus) — what was rejected, so it is not re-proposed

**The 2026-08-14 feature survey (HHD, G-Helper, z13ctl-plus/z13gui-plus) —
what was rejected, so it is not re-proposed.** Shipped from it: tuning-reset,
named fan presets (with the coupling to profile switches removed), the curve
editor's operating-point dot, `status --watch`, `pending_reboot`, `charge_mode`.
Rejected, each for a checked reason: the fork's **EPP control** (entry above);
its **tablet-kit** features (Jeff: dubious value); its **NPU power modes**
(measured by the fork itself at 10–20 mW across all three — below sensor noise
— and removed there); its **"more Aura modes"** (not present in its tree; the
README claimed them); its **zero-RPM** claim (a bug its Quiet-profile curve
introduced, fixed by deletion in `293fdff` — the reason presets are user-only).
The lesson that shaped the survey: a fork's README is a claim, its tree is the
evidence, and our own CLAUDE.md prose can go stale the same way — two "gaps"
(live operating point, floor on the chart) turned out to be already built.
