# A firmware profile reports its stock row, never the readback, and only for display: `safety.Engine.ReadDisplayed`

**A firmware profile reports its stock row, never the readback, and only for
display: `safety.Engine.ReadDisplayed`** (Jeff, 2026-11-15). On a firmware
profile the firmware runs its own limits (the entry above), and on
asus-armoury `current_value` is the kernel's cache of the last value written
*while on the current power source*. The smoke test found the consequence: a
custom profile at 35 W ran on battery, balanced was later selected on AC, and
back on battery `status` showed 35/35/45 while RAPL measured 47.1 W under
load. `ReadDisplayed(env, profile)` returns the device's `StockProfilePPT`
row for a profile that has one, as the interface holds it
(`EffectiveTDP`: armoury has no APU/Platform sPPT, so the row's 70s would
print as limits that do not exist — caught on hardware), reading no hardware
beyond the caller's envelope; `ReadEffective` otherwise. That is also what
`HandBackToFirmware`'s row write leaves in the readback, so on AC the output
is unchanged.
Its callers are the ones that *show* a limit: get-state's `tdp` and
`tdp-get` (through `d.displayTDP()`, which keys the row on
`platform_profile` when Fn+F5 moved it behind the daemon), and the CLI's
no-daemon `tdp --get`/`status`, which also label the values as the profile's
stock limits (`stockRowFor`). **Everything that decides stays on
`ReadEffective`**: `CheckFanFloorRelease`, and through it
`HandBackToFirmware`, must refuse to release the fans while hardware reads
above the safe maximum, however stale that reading is.
`TestFanFloorCheckIgnoresTheDisplayedRow` and the existing
`TestHandBackToFirmwareKeepsTheFloorWhenTheRowFails` both fail when the check
is pointed at `ReadDisplayed` (negative control run). Known gaps, accepted:
on battery the row (an AC measurement) overstates what the firmware holds,
and a limit written out of band on a firmware profile is no longer shown.
Battery rows would be device data, measured under load, if ever wanted.
