package daemon

// button.go — Armoury Crate button watcher via Linux evdev.
//
// Finds the "Asus WMI hotkeys" input device by sysfs name, grabs it exclusively,
// and forwards KEY_PROG3 key-down events to a channel. Retries automatically
// after device loss (e.g. suspend/resume).

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/holoplot/go-evdev"
)

// findButtonDevice returns the /dev/input/eventN path for the "Asus WMI hotkeys"
// input device by scanning sysfs device names. Sysfs reads require no device-open
// permissions, so this works even when most /dev/input/event* nodes are restricted.
func findButtonDevice() string {
	entries, err := os.ReadDir("/sys/class/input")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		namePath := "/sys/class/input/" + e.Name() + "/device/name"
		nameBytes, err := os.ReadFile(namePath)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(nameBytes)) == "Asus WMI hotkeys" {
			return "/dev/input/" + e.Name()
		}
	}
	return ""
}

// watchButton runs until ctx is done, forwarding Armoury Crate button presses
// to ch. It finds the device, runs the read loop, and retries on any error.
func watchButton(ctx context.Context, ch chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		path := findButtonDevice()
		if path == "" {
			slog.Info("Armoury Crate button device not found; will retry", "delay", "5s")
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		if err := runButtonLoop(ctx, path, ch); err != nil {
			slog.Info("button watcher stopped; retrying", "err", err, "delay", "1s")
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

// runButtonLoop opens the device at path, grabs it exclusively, and forwards
// KEY_PROG1 key-down events to ch until ctx is done or a read error occurs.
func runButtonLoop(ctx context.Context, path string, ch chan<- struct{}) error {
	dev, err := evdev.Open(path)
	if err != nil {
		return err
	}
	if err := dev.Grab(); err != nil {
		// If grab fails (e.g. EBUSY: another process holds an exclusive grab),
		// the kernel routes all events to that process and our reads would block
		// forever. Return an error so watchButton retries after a delay.
		_ = dev.Close()
		return fmt.Errorf("exclusive grab failed: %w", err)
	}
	slog.Info("watching Armoury Crate button", "path", path)

	// Closing the device unblocks ReadOne when ctx is done.
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = dev.Close()
		case <-stop:
		}
	}()
	defer close(stop)

	for {
		evt, err := dev.ReadOne()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// Value 1 = key-down; ignore auto-repeat (2) and key-up (0).
		// KEY_PROG3 (202) is the Armoury Crate button on the 2025 ROG Flow Z13.
		if evt.Type == evdev.EV_KEY && evt.Value == 1 && evt.Code == evdev.KEY_PROG3 {
			slog.Info("Armoury Crate button pressed")
			select {
			case ch <- struct{}{}:
			default: // non-blocking: discard if nobody consuming
			}
		}
	}
}
