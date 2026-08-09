// Package evdevkey implements driver.Buttons over a single key on a Linux
// evdev input device — the "evdev-key" method in device data. The device is
// found by its sysfs name and parameterized by keycode, so the same driver
// serves any machine whose extra hardware button is an ordinary input event.
//
// The device is always opened shared, never with an exclusive grab. On the
// Z13 the "Asus WMI hotkeys" node carries SW_TABLET_MODE as well as the
// Armoury Crate key, so an EVIOCGRAB on it takes the tablet-mode transitions
// away from libinput too: attach the detachable cover after login and the
// desktop stays convinced the machine is a tablet, suppressing the cover
// keyboard until the session restarts (z13ctl issue #10). Reading shared
// costs nothing — evdev delivers events to every non-exclusive reader — and
// the eventDevice seam deliberately has no Grab method, so reintroducing the
// grab is a compile error.
package evdevkey

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/holoplot/go-evdev"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// inputClassDir is the sysfs directory listing input event devices.
// Declared as a var so tests can redirect it; nothing else should assign to it.
var inputClassDir = "/sys/class/input"

// Retry delays for the watcher loop. Vars so tests can shorten them.
var (
	buttonSearchDelay = 5 * time.Second // device not present yet
	buttonRetryDelay  = time.Second     // open failed, or the read loop ended
)

// eventDevice is the subset of *evdev.InputDevice the watcher needs. It
// deliberately omits Grab — see the package comment for why that omission is
// load-bearing.
type eventDevice interface {
	ReadOne() (*evdev.InputEvent, error)
	Close() error
}

// openEventDevice opens an evdev node. Indirected so tests can substitute a fake.
var openEventDevice = func(path string) (eventDevice, error) { return evdev.Open(path) }

// Buttons watches one key on one input device.
type Buttons struct {
	deviceName string // sysfs device name to find ("Asus WMI hotkeys")
	keycode    evdev.EvCode
	kind       string // ButtonEvent.Kind delivered for each press
}

// New returns a watcher for the named input device and keycode. Pure: the
// device is located and opened only once Watch runs.
func New(deviceName string, keycode int, kind string) *Buttons {
	return &Buttons{deviceName: deviceName, keycode: evdev.EvCode(keycode), kind: kind}
}

// findDevice returns the /dev/input/eventN path for the named input device by
// scanning sysfs device names. Sysfs reads require no device-open permissions,
// so this works even when most /dev/input/event* nodes are restricted.
func (b *Buttons) findDevice() string {
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
		if strings.TrimSpace(string(nameBytes)) == b.deviceName {
			return "/dev/input/" + e.Name()
		}
	}
	return ""
}

// Watch blocks until ctx is done, forwarding key presses to ch. It finds the
// device, runs the read loop, and retries on any error — device loss on
// suspend/resume, an unopenable node, the device not existing yet. Sends are
// non-blocking: a consumer that has stopped reading loses presses rather than
// stalling the watcher.
func (b *Buttons) Watch(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		path := b.findDevice()
		if path == "" {
			slog.Info("button device not found; will retry", "name", b.deviceName, "delay", buttonSearchDelay)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(buttonSearchDelay):
			}
			continue
		}
		dev, err := openEventDevice(path)
		if err != nil {
			slog.Info("button watcher stopped; retrying", "err", err, "delay", buttonRetryDelay)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(buttonRetryDelay):
			}
			continue
		}
		// Shared, not exclusive — see the package comment. If some other
		// process holds an EVIOCGRAB on this node the kernel routes events only
		// to it, and this loop will sit idle with no error to report.
		slog.Info("watching hardware button (shared, non-exclusive)", "path", path, "kind", b.kind)
		if err := b.runLoop(ctx, dev, ch); err != nil {
			slog.Info("button watcher stopped; retrying", "err", err, "delay", buttonRetryDelay)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(buttonRetryDelay):
			}
		}
	}
}

// runLoop reads dev and forwards key-down events for the watched keycode to ch
// until ctx is done or a read error occurs. It takes ownership of dev and
// closes it.
//
// Every other event — including EV_SW/SW_TABLET_MODE — is ignored here and,
// because the device is not grabbed, still reaches libinput and the desktop.
func (b *Buttons) runLoop(ctx context.Context, dev eventDevice, ch chan<- driver.ButtonEvent) error {
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
		if evt.Type == evdev.EV_KEY && evt.Value == 1 && evt.Code == b.keycode {
			slog.Info("hardware button pressed", "kind", b.kind)
			select {
			case ch <- driver.ButtonEvent{Kind: b.kind}:
			default: // non-blocking: discard if nobody consuming
			}
		}
	}
}
