# The autoswitch UI uses cycle buttons and a debounced single send

**The autoswitch UI uses cycle buttons and a debounced single send.**
Cycle-on-tap instead of a dropdown for the standing gamescope reason;
empty custom profiles are unusable targets even though the daemon accepts
them (a target that fails at every transition is a trap) — shown **greyed
with an "(empty)" label** rather than hidden, because silently omitting them
read as "my profiles aren't offered", a broken list rather than a rule
(`profileui.TargetRows`; `TargetOptions` remains the selectable subset, and
the two are derived from one function so they cannot disagree; Jeff,
2026-08-14); and the 300ms debounce is about ordering, not chattiness —
per-click goroutines can land on the daemon out of order and store an
intermediate choice.
