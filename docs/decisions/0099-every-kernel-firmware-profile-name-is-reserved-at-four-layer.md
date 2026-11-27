# Every kernel firmware profile name is reserved, at four layers

**Every kernel firmware profile name is reserved, at four layers** —
`api.KernelProfileNames` (low-power, cool, quiet, balanced,
balanced-performance, performance, max-power) plus `custom`, on every device
rather than only the names this one offers, so a profile made on one machine
stays valid on another (widened 2026-10-09 from the Z13's three;
`api.StockProfiles`/`IsStockProfileName` remain exported, deprecated, for 1.x
clients). Selecting a reserved name always reaches the firmware profile:
(1) `api.ValidateProfileName` rejects them, (2) `applyProfileLocked` tests
`isFirmwareProfile` *before* it consults the map, (3)
`api.State.IsCustomProfile` returns false for any kernel name ahead of the
lookup, and (4) `loadState` **renames** a `custom_profiles` entry carrying one
to `user-<name>` (numbered if taken; Jeff, 2026-10-09: the settings are the
user's, never dropped). References follow the rename only for the four names
that were legal custom names before 2.0; `quiet`/`balanced`/`performance` were
already reserved, so a reference to one always meant the firmware profile.
Layer 4 is not paranoia — `state.json` is a plain file a user can edit, and it
parses fine, so the other three never see it. The reservation is load-bearing beyond
aesthetics: `safety.Engine.ReadEffective` disables its stale-5W fallback for any name
absent from `StockProfilePPT`, which is right for a custom profile and wrong
for a firmware one, so a custom "balanced" would misreport the power limits.
Name validation is strict on write (`profile-create`/`profile-save` reject
`Gaming` rather than folding it to `gaming`, or the user looks for a profile
under a name that is not there) and lenient on lookup (`--set` and `--profile`
do fold case).
