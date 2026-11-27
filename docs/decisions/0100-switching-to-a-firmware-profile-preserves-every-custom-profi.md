# Switching to a firmware profile preserves every custom profile

**Switching to a firmware profile preserves every custom profile**; it clears
the undervolt and calls `device.HandBackToFirmware` (that profile's
`StockProfilePPT` row, then the fans to auto). `profile --set <custom>` errors if that profile has no
settings at all — there would be nothing to apply.
