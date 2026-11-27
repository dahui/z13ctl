# A capability whose out-of-tree interface is missing is *unavailable*, not broken, and says why

**A capability whose out-of-tree interface is missing is *unavailable*,
not broken, and says why** (2026-11-17; the plan's "ryzen_smu when present,
platform profiles as the fallback, with an install note"). Without
ryzen_smu the OXP drew a TDP slider whose every write failed with "no such
file". A factory now returns `device.UnavailableError{Reason}` when the
interface could be installed (amd-smu: `amd.SMUMissingReason`, which names
the fork from `coPlatforms`' `ForkNote`). `Assemble` leaves the capability nil
and records `Device.Unavailable`, and any other factory error still fails
the device. The reason reaches every surface: a startup WARN, device-get's
`unavailable` list, `hw.Missing("power", …)` in the daemon's and CLI's
refusals and `status`, and a note in the Dashboard's POWER card
(`controls.UnavailableNote`). **The pin holds only while the power control
it protects exists**: with power assembled the profile driver gets
`names = [pin]`, and without it every name in `profiles.names`, so the
OXP's file lists low-power/balanced/performance (≈33/61/61 W measured) and
pins balanced. Validation: a pin needs a `[power]` block, and only the pin
needs a stock row. **Custom follows the Profiles tab** (`mainwin.Offers`):
a device with profiles but no power/fans/undervolt has no Profiles tab, so
neither surface offers a Custom control that would lead nowhere. The daemon
assembles once, so an installed module counts after a restart; the note
says so. Verified on the OXP with `voltaire-hwtest`, whose SMU path points
nowhere (exactly a missing module): WARN, `status`/`tdp`/device-get
reasons, the three profiles selectable with low-power held, the Dashboard
note with no Custom button; back on the normal build, the slider, the pin
and the remembered 35 W returned. `TestUnavailablePowerDropsThePin`
(negative control run: the unconditional pin fails it).
