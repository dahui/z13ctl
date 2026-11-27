# Only a firmware profile *this device offers* may reach `platform_profile`

**Only a firmware profile *this device offers* may reach `platform_profile`**
(`d.isFirmwareProfile`, i.e. `hw.Profiles.Names()`; 2026-10-09). The driver
contract always said so and nothing enforced it — callers gated on the
hardcoded `api.IsStockProfileName`, so on another device `low-power` could
not be selected while `quiet` was accepted. `applyProfileLocked`, `Run()`'s
restore and autoswitch (target validation and `autoswitchTarget`) all check
the device's list. A state file naming a profile that is neither — deleted
by hand, lost in a downgrade, or carried from a machine with different
firmware profiles — is never written; `loadState` clears a name that is
neither a kernel name nor a custom profile, and `Run()` refuses a kernel name
the device does not offer.
