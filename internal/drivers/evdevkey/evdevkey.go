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
//
// It also never reads a typing keyboard unless the device data opts that node
// in (keyboard_node), and opens read-only. The first is the runtime half of
// the guard whose other half is the udev grant (ENV{ID_INPUT_KEYBOARD}!="1"):
// a grant widened by hand still cannot turn the watcher into a keylogger. The
// second matters because a writable evdev node accepts injected events.
package evdevkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// openEventDevice opens an evdev node read-only: the watcher only reads, and a
// writable evdev fd can inject events. Indirected so tests can substitute a
// fake.
var openEventDevice = func(path string) (eventDevice, error) {
	return evdev.OpenWithFlags(path, os.O_RDONLY)
}

// Buttons watches one key, or one chord of keys, on one input device.
type Buttons struct {
	deviceName    string // sysfs device name to find ("Asus WMI hotkeys")
	keycode       evdev.EvCode
	chord         []evdev.EvCode // non-empty: a chord (evdev-chord), and keycode is unused
	id            string         // ButtonEvent.ID delivered for each press
	allowKeyboard bool           // device data opted a typing-keyboard node in (keyboard_node)
	usb           *USBNode       // non-nil: the node must also be this USB interface
	checked       bool           // checkKey has run; Watch is the only goroutine touching it
}

// USBNode names an input node by the USB interface it belongs to.
type USBNode struct {
	Vendor, Product uint16
	Interface       int // bInterfaceNumber
}

// WithUSB narrows the watcher to the node of that name that is also this USB
// interface. A name alone is not always unique: the OneXPlayer X2 Mini Pro's
// controller registers "HID 1a86:fe00" on interface 0 (the button chords) and
// again on interface 1 (mouse and consumer keys), and sysfs order lists
// interface 1 first. Returns b.
func (b *Buttons) WithUSB(n USBNode) *Buttons {
	b.usb = &n
	return b
}

// New returns a watcher for the named input device and keycode, delivering id
// with each press. allowKeyboard is the device data's keyboard_node opt-in;
// without it a node that is a typing keyboard is refused. Pure: the device is
// located and opened only once Watch runs.
func New(deviceName string, keycode int, id string, allowKeyboard bool) *Buttons {
	return &Buttons{deviceName: deviceName, keycode: evdev.EvCode(keycode), id: id, allowKeyboard: allowKeyboard}
}

// NewChord returns a watcher for a button that arrives as a chord: several
// keys pressed together, as a OneXPlayer's buttons send Ctrl+Alt+Meta on its
// AT keyboard. A press is reported once, on the key-down that completes the
// chord, and the chord re-arms when any of its keys is released. Every key
// must be held: matching on one distinguishing key, as HHD does, would fire on
// that key typed alone, and voltaire never grabs the node to stop the desktop
// seeing the same keys.
func NewChord(deviceName string, keys []int, id string, allowKeyboard bool) *Buttons {
	b := &Buttons{deviceName: deviceName, id: id, allowKeyboard: allowKeyboard}
	for _, k := range keys {
		b.chord = append(b.chord, evdev.EvCode(k))
	}
	return b
}

// press decides whether one key event completes the button, updating the
// chord state it is given (unused for a single key). Pure, so the chord rules
// are tested without a device.
func (b *Buttons) press(held map[evdev.EvCode]bool, fired *bool, code evdev.EvCode, value int32) bool {
	if len(b.chord) == 0 {
		// Value 1 = key-down; ignore auto-repeat (2) and key-up (0).
		return value == 1 && code == b.keycode
	}
	if !slices.Contains(b.chord, code) {
		return false
	}
	switch value {
	case 0:
		held[code] = false
		*fired = false // any key up re-arms the chord
	case 1:
		held[code] = true
		if *fired {
			return false
		}
		for _, k := range b.chord {
			if !held[k] {
				return false
			}
		}
		*fired = true
		return true
	}
	return false // auto-repeat
}

// keys is every key the button needs: the chord, or the single key.
func (b *Buttons) keys() []evdev.EvCode {
	if len(b.chord) > 0 {
		return b.chord
	}
	return []evdev.EvCode{b.keycode}
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
		if strings.TrimSpace(string(nameBytes)) != b.deviceName {
			continue
		}
		if b.usb != nil {
			if n, ok := NodeUSB(e.Name()); !ok || n != *b.usb {
				continue
			}
		}
		return "/dev/input/" + e.Name()
	}
	return ""
}

// NodeUSB reports the USB interface an input node belongs to, from sysfs
// alone: the input device's id/vendor and id/product, and the
// bInterfaceNumber of the nearest USB interface above it (directly above for
// a driver bound to the interface, like xpad; one level further for a HID
// device). ok is false for a node not on USB.
func NodeUSB(event string) (n USBNode, ok bool) {
	dev := inputClassDir + "/" + event + "/device"
	bus, err1 := readHex(dev + "/id/bustype")
	vendor, err2 := readHex(dev + "/id/vendor")
	product, err3 := readHex(dev + "/id/product")
	if err1 != nil || err2 != nil || err3 != nil || bus != 0x03 { // BUS_USB
		return USBNode{}, false
	}
	dir, err := filepath.EvalSymlinks(dev)
	if err != nil {
		return USBNode{}, false
	}
	for range 4 { // input -> HID device -> USB interface, with room to spare
		dir = filepath.Dir(dir)
		if iface, err := readHex(dir + "/bInterfaceNumber"); err == nil {
			return USBNode{Vendor: uint16(vendor), Product: uint16(product), Interface: int(iface)}, true
		}
	}
	return USBNode{}, false
}

// readHex reads a sysfs attribute holding one hexadecimal number.
func readHex(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 16, 16)
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
		// Before opening, never after: the refusal is about what the node is,
		// and a static sysfs bitmap answers it without touching the device.
		if !b.allowKeyboard {
			if kbd, err := isTypingKeyboard(filepath.Base(path)); kbd || err != nil {
				return fmt.Errorf("refusing to read %s (%s): %w", b.deviceName, path, keyboardRefusal(err))
			}
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
		if !b.checked {
			b.checked = true
			b.checkKey(filepath.Base(path))
		}
		slog.Info("watching hardware button (shared, read-only)", "path", path, "button", b.id)
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

	held, fired := map[evdev.EvCode]bool{}, false // chord state, fresh per open
	for {
		evt, err := dev.ReadOne()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if evt.Type == evdev.EV_KEY && b.press(held, &fired, evt.Code, evt.Value) {
			slog.Info("hardware button pressed", "button", b.id)
			select {
			case ch <- driver.ButtonEvent{ID: b.id}:
			default: // non-blocking: discard if nobody consuming
			}
		}
	}
}

// checkKey warns when the device does not report the configured keycode in its
// static key capabilities — the device data names a key this node can never
// send, so the watcher would wait forever for a press. It reads a sysfs bitmap,
// never the device, and only warns: the watch goes on regardless, since an
// unreadable bitmap says nothing either way.
func (b *Buttons) checkKey(event string) {
	data, err := os.ReadFile(inputClassDir + "/" + event + "/device/capabilities/key")
	if err != nil {
		slog.Debug("button key capabilities unreadable", "event", event, "err", err)
		return
	}
	for _, k := range b.keys() {
		has, err := bitmapHas(strings.TrimSpace(string(data)), int(k))
		if err != nil {
			slog.Debug("button key capabilities unparseable", "event", event, "err", err)
			return
		}
		if !has {
			slog.Warn("button device does not report a configured key; presses will not be seen",
				"name", b.deviceName, "event", event, "keycode", int(k))
		}
	}
}

// errTypingKeyboard is the refusal for a node that is a typing keyboard.
var errTypingKeyboard = errors.New("it is a typing keyboard; reading it would see every keystroke " +
	"(a device file may opt such a node in with keyboard_node, scoped to its model)")

// keyboardRefusal is the reason isTypingKeyboard's answer gives for refusing.
func keyboardRefusal(err error) error {
	if err != nil {
		return fmt.Errorf("its key capabilities cannot be read, so it cannot be shown not to be a keyboard: %w", err)
	}
	return errTypingKeyboard
}

// IsTypingKeyboard reports whether the input node event ("event4") is a typing
// keyboard, by the test the watcher refuses on. voltaire setup applies it
// before its direct grant, so the two cannot disagree.
func IsTypingKeyboard(event string) (bool, error) { return isTypingKeyboard(event) }

// isTypingKeyboard applies udev's own test (input_id's ID_INPUT_KEYBOARD) to
// the node's static key bitmap: a device reporting every key in the first
// 32-bit word but KEY_RESERVED — KEY_ESC (1) through KEY_S (31), the escape
// key, the number row and most of two letter rows — is a typing keyboard. An unreadable bitmap is an error, which the caller treats as a
// refusal: the guard fails closed.
func isTypingKeyboard(event string) (bool, error) {
	data, err := os.ReadFile(inputClassDir + "/" + event + "/device/capabilities/key")
	if err != nil {
		return false, err
	}
	bitmap := strings.TrimSpace(string(data))
	for key := 1; key < 32; key++ { // udev's mask: 0xFFFFFFFE over the first word
		has, err := bitmapHas(bitmap, key)
		if err != nil {
			return false, err
		}
		if !has {
			return false, nil
		}
	}
	return true, nil
}

// bitmapHas reports whether bit is set in a sysfs input capability bitmap:
// space-separated hex words, most significant first, each one kernel long
// (64 bits here) wide — the format of capabilities/key.
func bitmapHas(bitmap string, bit int) (bool, error) {
	words := strings.Fields(bitmap)
	if len(words) == 0 || bit < 0 {
		return false, fmt.Errorf("empty bitmap")
	}
	const wordBits = 64
	i := len(words) - 1 - bit/wordBits // the last word holds bits 0–63
	if i < 0 {
		return false, nil
	}
	w, err := strconv.ParseUint(words[i], 16, 64)
	if err != nil {
		return false, err
	}
	return w&(1<<uint(bit%wordBits)) != 0, nil
}
