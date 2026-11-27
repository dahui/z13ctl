# Keyboard hotplug watcher

**Keyboard hotplug watcher**: `internal/daemon/hotplug.go` handles the
detachable keyboard. The keyboard (`0b05:1a30`) is its own HID device that
loses power when detached; on reattach the firmware does not restore the
previous RGB effect. The lighting driver opens the HID device once at startup and
holds the handle, so after a detach/reattach cycle the keyboard appears as a *new*
hidraw node the stale handle never references. `watchHotplug()` polls the
driver's `PresentZones()` (sysfs-only presence check, no device open) every 2s;
when a zone appears that was not latched it calls `reopenAndRestore()` (daemon.go),
which runs `d.hw.Lighting.Reopen()` under `d.mu` (the driver swaps in the new
device and closes the old one) and re-applies saved lighting via the existing `applyLightingState()` (honoring
per-device overrides). If the reopen fails — e.g. udev has not yet chmod'd the new
hidraw node — the watcher does not latch the present state and retries on the next
tick. No action is taken on detach (the keyboard powers off in hardware). Run()'s
close goes through the driver, so it closes whichever handle is current. **No zone is declared detachable** (2026-10-09): the watcher latches
the *set* of present zones and reopens when it gains one, so the keyboard
returning triggers it, and a declared zone whose hardware never appears (a SKU
without it) cannot block it — a single "all present" bool would have hung on
exactly that zone.
