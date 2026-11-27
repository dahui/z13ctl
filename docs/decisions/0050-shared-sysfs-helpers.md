# Shared sysfs helpers

**Shared sysfs helpers**: `internal/drivers/linux/profile.go` holds the path
discovery (`FindProfilePathFor`, `FindBatteryThresholdPath`, …); `cmd/` and the
daemon reach it through the assembled `device.Device`, not directly. Daemon
GET handlers read sysfs directly (not cached state) for accuracy when another
process has modified the setting.
