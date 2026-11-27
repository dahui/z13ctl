# State persistence

**State persistence**: `$XDG_STATE_HOME/voltaire/state.json` (atomic write via temp +
rename). On first run, a pre-2.0 `$XDG_STATE_HOME/z13ctl/state.json` is
**copied, never moved** to the new path (`migrateOldStateFile`): the old file
staying put is what keeps a 1.x downgrade safe through all of 2.x, and the
copy is byte-for-byte so a corrupt old file still hits `loadState`'s own
corrupt-file preservation instead of being judged during migration.
Daemon restores lighting, fan curves, and TDP on start. Fan curves and
custom TDP are only restored when `profile == "custom"`; a saved *stock* profile
goes through `device.HandBackToFirmware` instead (row, then a fan release that
re-applies the firmware's own limits), which also replaces the stale 5W cache
the kernel's PPT attributes come up holding after boot. Only the row is written
when a curve the daemon does not own is in force.
