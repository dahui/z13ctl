package daemon

// button_test.go — Armoury Crate button watcher: device discovery and the read
// loop, driven by a fake evdev device so no hardware is required.

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/holoplot/go-evdev"
)

// fakeInputSysfs builds a /sys/class/input stand-in and points inputClassDir at it.
func fakeInputSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := inputClassDir
	inputClassDir = root
	t.Cleanup(func() { inputClassDir = orig })
	return root
}

// addInputNode creates <root>/<node>/device/name containing name.
func addInputNode(t *testing.T, root, node, name string) {
	t.Helper()
	dir := root + "/" + node + "/device"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(dir+"/name", []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestFindButtonDeviceByName(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event0", "AT Translated Set 2 keyboard")
	addInputNode(t, root, "event7", buttonDeviceName)
	addInputNode(t, root, "event9", "GZ302EAC cover keyboard")

	if got, want := findButtonDevice(), "/dev/input/event7"; got != want {
		t.Errorf("findButtonDevice() = %q, want %q", got, want)
	}
}

func TestFindButtonDeviceSkipsNonEventEntries(t *testing.T) {
	root := fakeInputSysfs(t)
	// "mice" and "js0" live alongside eventN in /sys/class/input.
	addInputNode(t, root, "mice", buttonDeviceName)
	addInputNode(t, root, "js0", buttonDeviceName)

	if got := findButtonDevice(); got != "" {
		t.Errorf("findButtonDevice() = %q, want \"\" — only event* nodes are usable", got)
	}
}

func TestFindButtonDeviceToleratesUnreadableNodes(t *testing.T) {
	root := fakeInputSysfs(t)
	// A node with no device/name file at all must not abort the scan.
	if err := os.MkdirAll(root+"/event0", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	addInputNode(t, root, "event1", buttonDeviceName)

	if got, want := findButtonDevice(), "/dev/input/event1"; got != want {
		t.Errorf("findButtonDevice() = %q, want %q", got, want)
	}
}

func TestFindButtonDeviceAbsent(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event0", "AT Translated Set 2 keyboard")
	if got := findButtonDevice(); got != "" {
		t.Errorf("findButtonDevice() = %q, want \"\"", got)
	}

	inputClassDir = root + "/does-not-exist"
	if got := findButtonDevice(); got != "" {
		t.Errorf("findButtonDevice() with a missing sysfs root = %q, want \"\"", got)
	}
}

// fakeEvdev replays a fixed script of events, then returns readErr forever.
type fakeEvdev struct {
	mu      sync.Mutex
	events  []evdev.InputEvent
	pos     int
	readErr error
	closed  bool
	// block, when non-nil, is received from before returning the exhausted error,
	// letting a test hold the loop open while it cancels the context.
	block chan struct{}
}

func (f *fakeEvdev) ReadOne() (*evdev.InputEvent, error) {
	f.mu.Lock()
	if f.pos < len(f.events) {
		e := f.events[f.pos]
		f.pos++
		f.mu.Unlock()
		return &e, nil
	}
	f.mu.Unlock()
	if f.block != nil {
		<-f.block
	}
	return nil, f.readErr
}

func (f *fakeEvdev) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.block != nil {
		select {
		case <-f.block:
		default:
			close(f.block)
		}
	}
	return nil
}

func keyEvent(code evdev.EvCode, value int32) evdev.InputEvent {
	return evdev.InputEvent{Type: evdev.EV_KEY, Code: code, Value: value}
}

func TestRunButtonLoopForwardsOnlyButtonKeyDown(t *testing.T) {
	tests := []struct {
		name  string
		event evdev.InputEvent
		want  bool
	}{
		{"button key-down notifies", keyEvent(evdev.KEY_PROG3, 1), true},
		{"button key-up ignored", keyEvent(evdev.KEY_PROG3, 0), false},
		{"button auto-repeat ignored", keyEvent(evdev.KEY_PROG3, 2), false},
		{"a different key ignored", keyEvent(evdev.KEY_PROG1, 1), false},
		{"letter key ignored", keyEvent(evdev.KEY_A, 1), false},
		{"EV_SYN ignored", evdev.InputEvent{Type: evdev.EV_SYN, Value: 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &fakeEvdev{events: []evdev.InputEvent{tt.event}, readErr: errors.New("eof")}
			ch := make(chan struct{}, 1)

			if err := runButtonLoop(context.Background(), dev, ch); err == nil {
				t.Fatal("runButtonLoop() = nil, want the read error that ends the loop")
			}
			got := len(ch) == 1
			if got != tt.want {
				t.Errorf("notified = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRunButtonLoopIgnoresTabletModeSwitch is the regression guard for issue #10.
// The watcher must pass over SW_TABLET_MODE without acting on it — and, because
// the device is never grabbed (eventDevice has no Grab method), libinput still
// receives the transition and the detachable cover keyboard keeps working.
func TestRunButtonLoopIgnoresTabletModeSwitch(t *testing.T) {
	dev := &fakeEvdev{
		events: []evdev.InputEvent{
			{Type: evdev.EV_SW, Code: evdev.SW_TABLET_MODE, Value: 1},
			{Type: evdev.EV_SW, Code: evdev.SW_TABLET_MODE, Value: 0},
			keyEvent(evdev.KEY_PROG3, 1), // still works after the switch events
		},
		readErr: errors.New("eof"),
	}
	ch := make(chan struct{}, 4)

	if err := runButtonLoop(context.Background(), dev, ch); err == nil {
		t.Fatal("runButtonLoop() = nil, want the read error that ends the loop")
	}
	if len(ch) != 1 {
		t.Errorf("notifications = %d, want exactly 1 (only the button press)", len(ch))
	}
}

// TestRunButtonLoopDropsPressWhenNobodyListening covers the non-blocking send:
// a full channel must not stall the watcher.
func TestRunButtonLoopDropsPressWhenNobodyListening(t *testing.T) {
	dev := &fakeEvdev{
		events: []evdev.InputEvent{
			keyEvent(evdev.KEY_PROG3, 1),
			keyEvent(evdev.KEY_PROG3, 1),
			keyEvent(evdev.KEY_PROG3, 1),
		},
		readErr: errors.New("eof"),
	}
	ch := make(chan struct{}, 1) // room for one; the rest must be discarded

	done := make(chan error, 1)
	go func() { done <- runButtonLoop(context.Background(), dev, ch) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("runButtonLoop() = nil, want the read error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runButtonLoop() blocked on a full channel instead of dropping the press")
	}
	if len(ch) != 1 {
		t.Errorf("buffered notifications = %d, want 1", len(ch))
	}
}

func TestRunButtonLoopReturnsNilOnContextCancel(t *testing.T) {
	dev := &fakeEvdev{readErr: errors.New("closed"), block: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runButtonLoop(ctx, dev, make(chan struct{}, 1)) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runButtonLoop() = %v, want nil on context cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runButtonLoop() did not return after context cancellation")
	}
}

func TestRunButtonLoopClosesDevice(t *testing.T) {
	dev := &fakeEvdev{readErr: errors.New("eof")}
	_ = runButtonLoop(context.Background(), dev, make(chan struct{}, 1))

	// The closer goroutine runs on the deferred close(stop); give it a moment.
	for range 20 {
		dev.mu.Lock()
		closed := dev.closed
		dev.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("runButtonLoop() returned without closing the device — the fd leaks on every retry")
}
