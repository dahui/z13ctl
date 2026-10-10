package daemon

// hotplug.go — lighting-zone reattach watcher.
//
// The 2025 ROG Flow Z13 keyboard is detachable and its RGB lighting is lost when
// it is removed. On reattach the firmware does not restore the previous effect, so
// the daemon polls sysfs for zones reappearing and re-applies the saved lighting
// state via reopenAndRestore. Which zone is the detachable one is not declared
// anywhere: any zone that appears triggers the reopen.

import (
	"context"
	"slices"
	"time"
)

// hotplugPollInterval is how often watchHotplug checks which zones are present.
const hotplugPollInterval = 2 * time.Second

// watchHotplug runs until ctx is done, watching for a lighting zone (the
// detachable keyboard) being reattached. When a zone appears that was not
// present, it reopens the HID device and re-applies saved lighting. If the
// reopen fails (e.g. udev has not yet applied hidraw permissions), the new zone
// is not latched, so the next tick retries.
//
// opened reports whether the startup Reopen succeeded. Without it nothing is
// latched: a keyboard present in sysfs whose node failed to open at startup
// (the daemon racing udev on a freshly attached cover) must look like a
// pending reattach, or the failure is latched until the next physical detach.
//
// PresentZones is sysfs-only by the driver contract, so polling it opens nothing.
func (d *Daemon) watchHotplug(ctx context.Context, opened bool) {
	if d.hw == nil || d.hw.Lighting == nil {
		return
	}
	var present []string
	if opened {
		present = d.hw.Lighting.PresentZones() // already restored at startup
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(hotplugPollInterval):
		}
		present = hotplugTick(present, d.hw.Lighting.PresentZones, d.reopenAndRestore)
	}
}

// hotplugTick advances the reattach watcher by one observation and returns the
// new latched set. observe reports the zones present now; onReattach is invoked
// only when one of them was not latched, and returns whether the reopen and
// restore succeeded. A new zone is latched on success only, so a failed reopen
// is retried on the next tick; a zone that leaves is unlatched at once, so its
// return is seen.
func hotplugTick(present []string, observe func() []string, onReattach func() bool) []string {
	now := observe()
	for _, z := range now {
		if slices.Contains(present, z) {
			continue
		}
		if onReattach() {
			return now
		}
		// Keep only what was already latched and is still here.
		var kept []string
		for _, p := range now {
			if slices.Contains(present, p) {
				kept = append(kept, p)
			}
		}
		return kept
	}
	return now
}
