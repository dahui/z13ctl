package daemon

// hotplug.go — detachable-keyboard reattach watcher.
//
// The 2025 ROG Flow Z13 keyboard is detachable and its RGB lighting is lost when
// it is removed. On reattach the firmware does not restore the previous effect, so
// the daemon polls sysfs for the keyboard hidraw node reappearing and re-applies
// the saved lighting state via reopenAndRestore.

import (
	"context"
	"time"
)

// hotplugPollInterval is how often watchHotplug checks for the keyboard's presence.
const hotplugPollInterval = 2 * time.Second

// watchHotplug runs until ctx is done, watching for the detachable keyboard being
// reattached. On an absent → present transition it reopens the HID device and
// re-applies saved lighting. If the reopen fails (e.g. udev has not yet applied
// hidraw permissions), it does not latch the present state, so the next tick
// retries.
//
// Present is sysfs-only by the driver contract, so polling it opens nothing.
func (d *Daemon) watchHotplug(ctx context.Context) {
	if d.hw == nil || d.hw.Lighting == nil {
		return
	}
	present := d.hw.Lighting.Present() // already restored at startup if present
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(hotplugPollInterval):
		}
		present = hotplugTick(present, d.hw.Lighting.Present, d.reopenAndRestore)
	}
}

// hotplugTick advances the reattach watcher by one observation and returns the
// new latched-present state. observe reports the keyboard's current presence;
// onReattach is invoked only on an absent → present transition and returns
// whether the reopen+restore succeeded. Presence is latched on success only, so
// a failed reopen (returning false) is retried on the next tick.
func hotplugTick(present bool, observe, onReattach func() bool) bool {
	now := observe()
	if now && !present {
		return onReattach()
	}
	return now
}
