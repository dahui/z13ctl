package daemon

// button.go — Armoury Crate button watcher via Linux evdev.
//
// Scans /dev/input/event* for a device capable of KEY_PROG1 (the ASUS ROG
// Armoury Crate side button), grabs it exclusively, and forwards key-down
// events to a channel. Retries automatically after device loss (e.g. suspend).

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/holoplot/go-evdev"
)

// findButtonDevice scans /dev/input for an input device capable of KEY_PROG1
// (the ASUS Armoury Crate side button). Returns the device path or "".
func findButtonDevice() string {
	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := "/dev/input/" + e.Name()
		d, err := evdev.OpenWithFlags(path, os.O_RDONLY)
		if err != nil {
			continue
		}
		codes := d.CapableEvents(evdev.EV_KEY)
		_ = d.Close()
		for _, c := range codes {
			if c == evdev.KEY_PROG1 {
				return path
			}
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
		slog.Warn("could not grab button device exclusively; other apps may also see presses", "err", err)
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
		if evt.Type == evdev.EV_KEY && evt.Code == evdev.KEY_PROG1 && evt.Value == 1 {
			slog.Info("Armoury Crate button pressed")
			select {
			case ch <- struct{}{}:
			default: // non-blocking: discard if nobody consuming
			}
		}
	}
}
