# The firmware profile list is the kernel's, filtered by device data, and reads are normalized to it

**The firmware profile list is the kernel's, filtered by device data, and
reads are normalized to it** (`internal/drivers/asus`; 2026-10-09).
`Names()` is the owning handler's `choices` ∩ the TOML `[profiles] names`, in
the TOML's order. The handler is chosen by its `name` (`handler = "asus-wmi"`),
not by "whose choices contain quiet". The kernel's `custom` choice is never
offered. A declared name the kernel does not offer is dropped with a warning,
and is deliberately *not* translated to an equivalent (`quiet` → `low-power`):
the stock row is keyed `quiet`, so offering `low-power` would skip it before
the fan release. `Get()` maps a secondary handler's spelling back
(`low-power`/`cool` → the device's low-power-group name) and reports the
kernel's `custom` read (handlers disagree) as unknown, so no raw value ever
keys a stock table. Writes still go per class device, the low-power group
translated for secondary handlers. The legacy file was checked: with PPD
running, every switch leaves all handlers agreeing (PPD's own legacy write
sets amd-pmf's hidden `quiet`). Without PPD the legacy file could read
`custom`, which `Get` now treats as unknown. Writing the legacy file instead
would need a new grant on a root-owned attribute. `PPDProfile` is the one
kernel→PPD map, shared with the dry run.
