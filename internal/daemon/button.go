package daemon

// button.go — Armoury Crate button watcher via Linux evdev.
//
// Finds the "Asus WMI hotkeys" input device by sysfs name, reads it
// non-exclusively, and forwards KEY_PROG3 key-down events to a channel. Retries
// automatically after device loss (e.g. suspend/resume).

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/holoplot/go-evdev"
)

// inputClassDir is the sysfs directory listing input event devices.
// Declared as a var so tests can redirect it; nothing else should assign to it.
var inputClassDir = "/sys/class/input"

// buttonDeviceName is the sysfs device name of the node carrying KEY_PROG3.
const buttonDeviceName = "Asus WMI hotkeys"

// eventDevice is the subset of *evdev.InputDevice the watcher needs.
//
// It deliberately omits Grab. The "Asus WMI hotkeys" node carries SW_TABLET_MODE
// as well as KEY_PROG3, so an exclusive EVIOCGRAB on it takes the tablet-mode
// transitions away from libinput too. When the detachable cover is attached after
// login, libinput never sees tablet mode turn off, stays convinced the machine is
// a tablet, and suppresses the cover keyboard entirely — ordinary keys stop
// reaching applications until the session restarts (issue #10). Reading shared
// costs nothing: evdev delivers events to every non-exclusive reader.
//
// Keeping Grab off this interface makes reintroducing the grab a compile error.
type eventDevice interface {
	ReadOne() (*evdev.InputEvent, error)
	Close() error
}

// openEventDevice opens an evdev node. Indirected so tests can substitute a fake.
var openEventDevice = func(path string) (eventDevice, error) { return evdev.Open(path) }

// findButtonDevice returns the /dev/input/eventN path for the "Asus WMI hotkeys"
// input device by scanning sysfs device names. Sysfs reads require no device-open
// permissions, so this works even when most /dev/input/event* nodes are restricted.
func findButtonDevice() string {
	entries, err := os.ReadDir(inputClassDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		namePath := inputClassDir + "/" + e.Name() + "/device/name"
		nameBytes, err := os.ReadFile(namePath)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(nameBytes)) == buttonDeviceName {
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
		dev, err := openEventDevice(path)
		if err != nil {
			slog.Info("button watcher stopped; retrying", "err", err, "delay", "1s")
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		// Shared, not exclusive — see the eventDevice doc comment. If some other
		// process holds an EVIOCGRAB on this node the kernel routes events only
		// to it, and this loop will sit idle with no error to report.
		slog.Info("watching Armoury Crate button (shared, non-exclusive)", "path", path)
		if err := runButtonLoop(ctx, dev, ch); err != nil {
			slog.Info("button watcher stopped; retrying", "err", err, "delay", "1s")
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

// runButtonLoop reads dev and forwards KEY_PROG3 key-down events to ch until ctx
// is done or a read error occurs. It takes ownership of dev and closes it.
//
// Every other event — including EV_SW/SW_TABLET_MODE — is ignored here and, because
// the device is not grabbed, still reaches libinput and the desktop.
func runButtonLoop(ctx context.Context, dev eventDevice, ch chan<- struct{}) error {
	// Closing the device unblocks ReadOne when ctx is done.
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = dev.Close()
		case <-stop:
			_ = dev.Close()
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
