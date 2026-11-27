# Button watcher

**Button watcher** (`driver.Buttons`, implemented by `internal/drivers/evdevkey`;
the device name, keycode and label are device data): finds the "Asus WMI hotkeys" input device by sysfs name
(`/sys/class/input/*/device/name`), opens it, and listens for KEY_PROG3 (code 202)
key-down events. KEY_PROG3 is the Armoury Crate button keycode on the 2025 ROG
Flow Z13 (differs from older ASUS models that use KEY_PROG1). Forwards press
events to subscribed GUI connections via long-lived socket connections. Disabled
with `--no-button`.
