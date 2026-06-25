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

	"github.com/dahui/z13ctl/internal/hid"
)

// hotplugPollInterval is how often watchHotplug checks for the keyboard's presence.
const hotplugPollInterval = 2 * time.Second

// watchHotplug runs until ctx is done, watching for the detachable keyboard being
// reattached. On an absent → present transition it reopens the HID device and
// re-applies saved lighting. If the reopen fails (e.g. udev has not yet applied
// hidraw permissions), it does not latch the present state, so the next tick
// retries.
func (d *Daemon) watchHotplug(ctx context.Context) {
	present := hid.HasDevice("keyboard") // already restored at startup if present
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(hotplugPollInterval):
		}
		now := hid.HasDevice("keyboard")
		switch {
		case now && !present:
			if d.reopenAndRestore() {
				present = true
			}
			// else: leave present=false so the next tick retries.
		default:
			present = now
		}
	}
}
