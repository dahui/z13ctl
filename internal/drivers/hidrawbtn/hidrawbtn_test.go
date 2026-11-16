package hidrawbtn

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// Report descriptors read from a GZ302EA on 2026-11-15.
var (
	// The cover's interface 0: vendor-defined (the Aura report, 0x5d), plus
	// Consumer and Generic Desktop / Wireless Radio Controls collections.
	vendorDesc = mustHex("06 31 ff 09 76 a1 01 85 5a 19 00 2a ff 00 15 00 26 ff 00 75 08 95 01 81 00 19 00 2a ff 00 15 00 26 ff 00 75 08 95 3f b1 00 c0 05 0c 09 01 a1 01 85 02 19 00 2a 3c 02 15 00 26 3c 02 75 10 95 02 81 00 c0 05 01 09 0c a1 01 85 03 15 00 25 01 09 c6 95 01 75 01 81 06 75 07 81 03 c0 06 31 ff 09 79 a1 01 85 5d 19 00 2a ff 00 15 00 26 ff 00 75 08 95 1f 81 00 19 00 2a ff 00 15 00 26 ff 00 75 08 95 3f 91 00 19 00 2a ff 00 15 00 26 ff 00 75 08 95 3f b1 00 c0")
	// The cover's interface 2: the keyboard it types through.
	keyboardDesc = mustHex("05 01 09 06 a1 01 85 01 75 01 95 08 05 07 19 e0 29 e7 15 00 25 01 81 02 95 01 75 08 81 03 95 05 75 01 05 08 19 01 29 05 91 02 95 01 75 03 91 03 95 06 75 08 15 00 26 ff 00 05 07 19 00 2a ff 00 81 00 95 c0 75 01 05 07 19 00 29 ef 15 00 25 01 81 02 c0")
	// The cover's interface 3, its touchpad (Generic Desktop / Mouse first).
	mouseDesc = mustHex("05 01 09 02 a1 01 85 01 09 01 a1 00 05 09 19 01 29 02 15 00 25 01 75 01 95 02 81 02 95 06 81 03 c0 c0")
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

func TestIsKeyboardDescriptor(t *testing.T) {
	for _, c := range []struct {
		desc string
		d    []byte
		want bool
	}{
		{"vendor interface", vendorDesc, false},
		{"typing keyboard", keyboardDesc, true},
		{"touchpad", mouseDesc, false},
		{"keypad", mustHex("05 01 09 07 a1 01 c0"), true},
		{"keyboard usage in a 4-byte usage", mustHex("0b 06 00 01 00 a1 01 c0"), true},
		{"keyboard nested inside another collection is not top-level", mustHex("06 31 ff 09 01 a1 01 05 01 09 06 a1 02 c0 c0"), false},
		{"truncated item fails closed", mustHex("05 01 09"), true},
		{"empty", nil, false},
	} {
		if got := IsKeyboardDescriptor(c.d); got != c.want {
			t.Errorf("%s: IsKeyboardDescriptor = %v, want %v", c.desc, got, c.want)
		}
	}
}

// The ROG Ally's buttons as G-Helper reads them: report ID 0x5a, the button's
// code in byte 1 (0xA6 the Armoury Crate button, 0x38 Command Center).
func allyButton(value byte) *Buttons {
	return New(Spec{ID: "armoury-crate", Vendor: 0x0b05, Product: 0x1abe, Interface: 2, ReportID: 0x5a, Byte: 1, Value: value})
}

func TestPressed(t *testing.T) {
	ac := allyButton(0xa6)
	for _, c := range []struct {
		desc   string
		report []byte
		want   bool
	}{
		{"its code", []byte{0x5a, 0xa6, 0, 0}, true},
		{"another button's code", []byte{0x5a, 0x38, 0, 0}, false},
		{"its code under another report ID", []byte{0x5b, 0xa6}, false},
		{"too short to hold the byte", []byte{0x5a}, false},
		{"empty", nil, false},
	} {
		if got := ac.pressed(c.report); got != c.want {
			t.Errorf("%s: pressed = %v, want %v", c.desc, got, c.want)
		}
	}
	unnumbered := New(Spec{ID: "home", Byte: 0, Value: 0x24})
	if !unnumbered.pressed([]byte{0x24, 0x00}) || unnumbered.pressed([]byte{0x22}) {
		t.Error("an unnumbered report matches on its byte alone")
	}
}

// fakeHidraw builds a /sys/class/hidraw stand-in and points sysHidrawDir at it.
func fakeHidraw(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := sysHidrawDir
	sysHidrawDir = root + "/class"
	t.Cleanup(func() { sysHidrawDir = orig })
	return root
}

// addNode adds hidrawN under a USB interface directory, the layout the
// kernel's sysfs has: class/hidrawN/device -> …/<interface>/<hid device>.
func addNode(t *testing.T, root, node, hidID, iface string, desc []byte) {
	t.Helper()
	ifDir := root + "/usb/" + node + "-if"
	hidDir := ifDir + "/hid"
	for _, d := range []string{hidDir, root + "/class/" + node} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(ifDir+"/bInterfaceNumber", iface+"\n")
	write(hidDir+"/uevent", "DRIVER=hid-generic\nHID_ID="+hidID+"\nHID_NAME=test\n")
	write(hidDir+"/report_descriptor", string(desc))
	if err := os.Symlink(hidDir, root+"/class/"+node+"/device"); err != nil {
		t.Fatal(err)
	}
}

func TestFindNodeByDeviceAndInterface(t *testing.T) {
	root := fakeHidraw(t)
	addNode(t, root, "hidraw1", "0003:00000B05:00001ABE", "00", vendorDesc)
	addNode(t, root, "hidraw2", "0003:00000B05:00001ABE", "02", vendorDesc)
	addNode(t, root, "hidraw3", "0003:00000B05:00001A30", "02", keyboardDesc)
	if got := allyButton(0xa6).findNode(); got != "hidraw2" {
		t.Errorf("findNode = %q, want hidraw2 (the device's interface 2)", got)
	}
}

// watchFor runs Watch until it returns or the timeout passes.
func watchFor(t *testing.T, b *Buttons, ch chan driver.ButtonEvent, d time.Duration) error {
	t.Helper()
	origS, origR := searchDelay, retryDelay
	searchDelay, retryDelay = time.Millisecond, time.Millisecond
	t.Cleanup(func() { searchDelay, retryDelay = origS, origR })
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return b.Watch(ctx, ch)
}

func TestWatchRefusesAKeyboardInterface(t *testing.T) {
	root := fakeHidraw(t)
	addNode(t, root, "hidraw2", "0003:00000B05:00001ABE", "02", keyboardDesc)
	opened := false
	orig := openNode
	openNode = func(string) (io.ReadCloser, error) { opened = true; return nil, errors.New("unreachable") }
	t.Cleanup(func() { openNode = orig })

	err := watchFor(t, allyButton(0xa6), make(chan driver.ButtonEvent, 1), 200*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "keyboard") {
		t.Errorf("Watch on a keyboard interface = %v, want a refusal naming it", err)
	}
	if opened {
		t.Error("the keyboard interface was opened")
	}
}

// reports is a fake hidraw node: one report per Read, then it blocks until
// closed, as a real node does between presses.
type reports struct {
	mu     sync.Mutex
	queue  [][]byte
	closed chan struct{}
}

func (r *reports) Read(p []byte) (int, error) {
	r.mu.Lock()
	if len(r.queue) > 0 {
		n := copy(p, r.queue[0])
		r.queue = r.queue[1:]
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()
	<-r.closed
	return 0, os.ErrClosed
}

func (r *reports) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

func TestWatchForwardsPresses(t *testing.T) {
	root := fakeHidraw(t)
	addNode(t, root, "hidraw2", "0003:00000B05:00001ABE", "02", vendorDesc)
	node := &reports{closed: make(chan struct{}), queue: [][]byte{
		{0x5a, 0x38}, // Command Center: not this button
		{0x5a, 0xa6}, // Armoury Crate
		{0x5a, 0x00}, // release
	}}
	orig := openNode
	openNode = func(path string) (io.ReadCloser, error) {
		if path != "/dev/hidraw2" {
			t.Errorf("opened %s, want /dev/hidraw2", path)
		}
		return node, nil
	}
	t.Cleanup(func() { openNode = orig })

	ch := make(chan driver.ButtonEvent, 4)
	_ = watchFor(t, allyButton(0xa6), ch, 200*time.Millisecond)
	if len(ch) != 1 {
		t.Fatalf("%d presses, want 1", len(ch))
	}
	if ev := <-ch; ev.ID != "armoury-crate" {
		t.Errorf("press id = %q, want armoury-crate", ev.ID)
	}
}
