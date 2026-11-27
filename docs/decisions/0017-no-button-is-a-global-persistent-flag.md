# `--no-button` is a global persistent flag;

`--no-button` is a global persistent flag; only affects the daemon subcommand.
When set, the button watcher goroutine is not started and voltaire does not open
the Armoury Crate button device at all — for users who would rather the keypress
reach only their desktop, or who need another tool to manage the device.
