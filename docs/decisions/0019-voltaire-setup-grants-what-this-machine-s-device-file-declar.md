# `voltaire setup` grants what this machine's device file declares, and nothing else

**`voltaire setup` grants what this machine's device file declares, and
nothing else** (Jeff, 2026-11-26; `internal/perms`, `.claude/plans/quality-pass.md`).
`perms.For(cfg)` maps each declared section and method to its grant
(lighting zones' hidraw interfaces read-write, buttons' input or hidraw nodes
read-only, profiles, fans, PPT, toggles, the charge limit, cpufreq boost,
ryzen_smu for amd-smu power or the undervolt, the RAPL counter read-only,
hid-oxp rumble). A section method with no case is an error, so a new driver
cannot ship without its grants. Toggles are the allowlist minus the file's
`hidden` entries (the driver offers every allowlisted attribute present,
declared or not). The machine is found by DMI; `--machine <id>` names a file
instead (a `keyboard_node` grant still applies only on a real DMI match). An
unknown machine gets nothing, never another machine's file: Z13 PPT and fan
grants on unknown hardware would let the daemon's fallback push Z13 limits
onto it. This replaced granting every embedded file's targets on every
machine, which existed only so one static rules file could be packaged.
`perms.Known(all files)` is still the universe for revoking a stale hidraw
grant. Tests: one rules golden per device file (`-update`, reviewed), the
safety guards over every device's output, a negative control per capability,
cross-device checks, and the boot pass against a temporary tree with real
chmod (`root`/`rootGID` are its seams; `TestMain` points root nowhere).
