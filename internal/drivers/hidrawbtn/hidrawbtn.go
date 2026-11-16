// Package hidrawbtn implements driver.Buttons over a vendor HID input report —
// the "hidraw-report" method in device data. Some buttons never become input
// events: the ROG Ally's Armoury Crate and Command Center buttons arrive only
// as a byte in a report on the controller's vendor interface (G-Helper and HHD
// both read them there), as does a OneXPlayer's Home button. The node is found
// by USB vendor:product and interface number, the same three values the udev
// grant is written from, so the reader and the grant cannot name different
// interfaces.
//
// The node is opened read-only and shared: hidraw delivers every input report
// to every reader, so the vendor tools and the kernel driver go on seeing them.
// A node whose report descriptor declares a typing keyboard (Generic Desktop /
// Keyboard or Keypad) is refused before it is opened — a keyboard's hidraw
// node carries its raw keystrokes, and naming the wrong interface in a device
// file must not turn the watcher into a keylogger.
package hidrawbtn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// sysHidrawDir lists hidraw nodes. A var so tests can redirect it; nothing
// else should assign to it.
var sysHidrawDir = "/sys/class/hidraw"

// Retry delays for the watcher loop. Vars so tests can shorten them.
var (
	searchDelay = 5 * time.Second // node not present yet
	retryDelay  = time.Second     // open failed, or the read loop ended
)

// openNode opens a hidraw node read-only. Indirected so tests can substitute
// a fake.
var openNode = func(path string) (io.ReadCloser, error) {
	return os.OpenFile(path, os.O_RDONLY, 0)
}

// Spec is one button: which node, and which report byte means a press.
type Spec struct {
	ID              string // ButtonEvent.ID
	Vendor, Product uint16
	Interface       int // USB bInterfaceNumber
	ReportID        int // first byte of the report; 0 for a device with unnumbered reports
	Byte            int // offset in the report as read, report ID included
	Value           byte
}

// Buttons watches one report byte on one hidraw node.
type Buttons struct{ spec Spec }

// New returns a watcher for spec. Pure: the node is located and opened only
// once Watch runs.
func New(spec Spec) *Buttons { return &Buttons{spec: spec} }

// pressed reports whether one input report is this button's press. Each
// matching report is a press: the reports seen from these buttons are sent
// once per press, and a duplicate within the daemon's 50 ms floor is
// swallowed there.
func (b *Buttons) pressed(report []byte) bool {
	s := b.spec
	if s.ReportID != 0 && (len(report) == 0 || int(report[0]) != s.ReportID) {
		return false
	}
	return s.Byte >= 0 && s.Byte < len(report) && report[s.Byte] == s.Value
}

// findNode returns the hidraw node's name ("hidraw3") for the spec's device
// and interface, "" when none is present.
func (b *Buttons) findNode() string {
	nodes, _ := filepath.Glob(sysHidrawDir + "/hidraw*")
	for _, n := range nodes {
		uevent, err := os.ReadFile(n + "/device/uevent")
		if err != nil || !hidIDMatches(string(uevent), b.spec.Vendor, b.spec.Product) {
			continue
		}
		iface, err := os.ReadFile(n + "/device/../bInterfaceNumber")
		if err != nil {
			continue
		}
		if v, err := strconv.ParseUint(strings.TrimSpace(string(iface)), 16, 8); err == nil && int(v) == b.spec.Interface {
			return filepath.Base(n)
		}
	}
	return ""
}

// hidIDMatches reports whether a HID uevent's HID_ID names vendor:product,
// on any bus.
func hidIDMatches(uevent string, vendor, product uint16) bool {
	want := fmt.Sprintf(":%08X:%08X", vendor, product)
	for _, line := range strings.Split(uevent, "\n") {
		if id, ok := strings.CutPrefix(line, "HID_ID="); ok {
			return strings.HasSuffix(strings.ToUpper(id), want)
		}
	}
	return false
}

// Watch blocks until ctx is done, forwarding presses to ch. It finds the node,
// reads reports, and retries on any error — the node going away on suspend or
// a controller detaching, an unopenable node, the node not existing yet. A
// node that is a typing keyboard ends the watch with an error instead: that
// is a property of the node, and retrying would not change it.
func (b *Buttons) Watch(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		node := b.findNode()
		if node == "" {
			slog.Info("button hidraw node not found; will retry", "button", b.spec.ID, "delay", searchDelay)
			if !sleep(ctx, searchDelay) {
				return nil
			}
			continue
		}
		desc, err := os.ReadFile(sysHidrawDir + "/" + node + "/device/report_descriptor")
		if err != nil {
			return fmt.Errorf("refusing to read /dev/%s: its report descriptor cannot be read, "+
				"so it cannot be shown not to be a keyboard: %w", node, err)
		}
		if IsKeyboardDescriptor(desc) {
			return fmt.Errorf("refusing to read /dev/%s: %w", node, errKeyboard)
		}
		f, err := openNode("/dev/" + node)
		if err != nil {
			slog.Info("button hidraw open failed; retrying", "button", b.spec.ID, "err", err, "delay", retryDelay)
			if !sleep(ctx, retryDelay) {
				return nil
			}
			continue
		}
		slog.Info("watching hardware button (hidraw, shared, read-only)", "path", "/dev/"+node, "button", b.spec.ID)
		if err := b.readLoop(ctx, f, ch); err != nil {
			slog.Info("button hidraw watcher stopped; retrying", "button", b.spec.ID, "err", err, "delay", retryDelay)
			if !sleep(ctx, retryDelay) {
				return nil
			}
		}
	}
}

// errKeyboard is the refusal for a keyboard interface.
var errKeyboard = errors.New("its report descriptor declares a keyboard, whose reports are keystrokes")

// readLoop reads reports from f until ctx is done or a read fails, forwarding
// presses non-blockingly. It owns f and closes it.
func (b *Buttons) readLoop(ctx context.Context, f io.ReadCloser, ch chan<- driver.ButtonEvent) error {
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-stop:
		}
		_ = f.Close() // unblocks Read
	}()
	defer close(stop)

	buf := make([]byte, 64) // a hidraw read returns one report; 64 is a full-speed report
	for {
		n, err := f.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if b.pressed(buf[:n]) {
			slog.Info("hardware button pressed", "button", b.spec.ID)
			select {
			case ch <- driver.ButtonEvent{ID: b.spec.ID}:
			default: // nobody consuming: discard rather than stall
			}
		}
	}
}

// sleep waits d or until ctx is done, reporting whether to carry on.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// IsKeyboardDescriptor reports whether a HID report descriptor declares a
// top-level application collection that is a keyboard or keypad (Generic
// Desktop usage 0x06 or 0x07). It walks the descriptor's short items, keeping
// the usage page and the last usage; a malformed descriptor counts as a
// keyboard, since a guard has to fail closed.
func IsKeyboardDescriptor(desc []byte) bool {
	var page, usage uint32
	depth := 0
	for i := 0; i < len(desc); {
		prefix := desc[i]
		if prefix == 0xFE { // long item: size in the next byte
			if i+1 >= len(desc) {
				return true
			}
			i += 3 + int(desc[i+1])
			continue
		}
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(desc) {
			return true
		}
		var data uint32
		for k := range size {
			data |= uint32(desc[i+1+k]) << (8 * k)
		}
		kind, tag := (prefix>>2)&0x03, prefix>>4
		switch {
		case kind == 1 && tag == 0x0: // global: Usage Page
			page = data
		case kind == 2 && tag == 0x0: // local: Usage (4 bytes carries its own page)
			usage = data
			if size == 4 {
				page, usage = data>>16, data&0xFFFF
			}
		case kind == 0 && tag == 0xA: // main: Collection
			if depth == 0 && data == 0x01 && page == 0x01 && (usage == 0x06 || usage == 0x07) {
				return true
			}
			depth++
		case kind == 0 && tag == 0xC: // main: End Collection
			if depth > 0 {
				depth--
			}
		}
		if kind == 0 { // a main item clears the local state
			usage = 0
		}
		i += 1 + size
	}
	return false
}
