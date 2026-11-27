# Daemon socket fallback

**Daemon socket fallback**: CLI commands try the Unix socket first (1 s timeout);
fall back to direct HID/sysfs if the daemon is not running. Detection is implicit:
connection refused → fall back.
