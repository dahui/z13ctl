# Sleep/resume hook

**Sleep/resume hook**: `internal/daemon/resume.go` watches for DBus
`org.freedesktop.login1.Manager.PrepareForSleep` signals. On sleep
(`PrepareForSleep(true)`), turns off the lightbar via `aura.TurnOff()` (the
keyboard turns off automatically in hardware, but the lightbar does not) and
calls `releaseVolatileState`. On resume (`PrepareForSleep(false)`),
`restoreVolatileState` restores lighting (regardless of profile) and reapplies
volatile state through `applyCustomHW` (when a custom profile is active). Uses
`github.com/godbus/dbus/v5`.
