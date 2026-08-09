package evdevkey

// evdevkey_test.go — button watcher: device discovery and the read loop,
// driven by a fake evdev device so no hardware is required. These tests came
// from internal/daemon with the driver extraction; every assertion carried
// over, including the issue #10 regression guard.

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holoplot/go-evdev"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// The Z13's values, as the device file declares them.
const (
	testDeviceName = "Asus WMI hotkeys"
	testKeycode    = 202 // KEY_PROG3
	testKind       = "armoury-crate"
)

// testButtons returns a watcher configured as the Z13 device file configures it.
func testButtons() *Buttons { return New(testDeviceName, testKeycode, testKind) }

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

func TestFindDeviceByName(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event0", "AT Translated Set 2 keyboard")
	addInputNode(t, root, "event7", testDeviceName)
	addInputNode(t, root, "event9", "GZ302EAC cover keyboard")

	if got, want := testButtons().findDevice(), "/dev/input/event7"; got != want {
		t.Errorf("findDevice() = %q, want %q", got, want)
	}
}

func TestFindDeviceSkipsNonEventEntries(t *testing.T) {
	root := fakeInputSysfs(t)
	// "mice" and "js0" live alongside eventN in /sys/class/input.
	addInputNode(t, root, "mice", testDeviceName)
	addInputNode(t, root, "js0", testDeviceName)

	if got := testButtons().findDevice(); got != "" {
		t.Errorf("findDevice() = %q, want \"\" — only event* nodes are usable", got)
	}
}

func TestFindDeviceToleratesUnreadableNodes(t *testing.T) {
	root := fakeInputSysfs(t)
	// A node with no device/name file at all must not abort the scan.
	if err := os.MkdirAll(root+"/event0", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	addInputNode(t, root, "event1", testDeviceName)

	if got, want := testButtons().findDevice(), "/dev/input/event1"; got != want {
		t.Errorf("findDevice() = %q, want %q", got, want)
	}
}

func TestFindDeviceAbsent(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event0", "AT Translated Set 2 keyboard")
	if got := testButtons().findDevice(); got != "" {
		t.Errorf("findDevice() = %q, want \"\"", got)
	}

	inputClassDir = root + "/does-not-exist"
	if got := testButtons().findDevice(); got != "" {
		t.Errorf("findDevice() with a missing sysfs root = %q, want \"\"", got)
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

func TestRunLoopForwardsOnlyButtonKeyDown(t *testing.T) {
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
			ch := make(chan driver.ButtonEvent, 1)

			if err := testButtons().runLoop(context.Background(), dev, ch); err == nil {
				t.Fatal("runLoop() = nil, want the read error that ends the loop")
			}
			got := len(ch) == 1
			if got != tt.want {
				t.Errorf("notified = %v, want %v", got, tt.want)
			}
			if got && tt.want {
				if ev := <-ch; ev.Kind != testKind {
					t.Errorf("event kind = %q, want %q", ev.Kind, testKind)
				}
			}
		})
	}
}

// TestRunLoopIgnoresTabletModeSwitch is the regression guard for issue #10.
// The watcher must pass over SW_TABLET_MODE without acting on it — and, because
// the device is never grabbed (eventDevice has no Grab method), libinput still
// receives the transition and the detachable cover keyboard keeps working.
func TestRunLoopIgnoresTabletModeSwitch(t *testing.T) {
	dev := &fakeEvdev{
		events: []evdev.InputEvent{
			{Type: evdev.EV_SW, Code: evdev.SW_TABLET_MODE, Value: 1},
			{Type: evdev.EV_SW, Code: evdev.SW_TABLET_MODE, Value: 0},
			keyEvent(evdev.KEY_PROG3, 1), // still works after the switch events
		},
		readErr: errors.New("eof"),
	}
	ch := make(chan driver.ButtonEvent, 4)

	if err := testButtons().runLoop(context.Background(), dev, ch); err == nil {
		t.Fatal("runLoop() = nil, want the read error that ends the loop")
	}
	if len(ch) != 1 {
		t.Errorf("notifications = %d, want exactly 1 (only the button press)", len(ch))
	}
}

// TestRunLoopDropsPressWhenNobodyListening covers the non-blocking send:
// a full channel must not stall the watcher.
func TestRunLoopDropsPressWhenNobodyListening(t *testing.T) {
	dev := &fakeEvdev{
		events: []evdev.InputEvent{
			keyEvent(evdev.KEY_PROG3, 1),
			keyEvent(evdev.KEY_PROG3, 1),
			keyEvent(evdev.KEY_PROG3, 1),
		},
		readErr: errors.New("eof"),
	}
	ch := make(chan driver.ButtonEvent, 1) // room for one; the rest must be discarded

	done := make(chan error, 1)
	go func() { done <- testButtons().runLoop(context.Background(), dev, ch) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("runLoop() = nil, want the read error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runLoop() blocked on a full channel instead of dropping the press")
	}
	if len(ch) != 1 {
		t.Errorf("buffered notifications = %d, want 1", len(ch))
	}
}

func TestRunLoopReturnsNilOnContextCancel(t *testing.T) {
	dev := &fakeEvdev{readErr: errors.New("closed"), block: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- testButtons().runLoop(ctx, dev, make(chan driver.ButtonEvent, 1)) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runLoop() = %v, want nil on context cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runLoop() did not return after context cancellation")
	}
}

func TestRunLoopClosesDevice(t *testing.T) {
	dev := &fakeEvdev{readErr: errors.New("eof")}
	_ = testButtons().runLoop(context.Background(), dev, make(chan driver.ButtonEvent, 1))

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
	t.Error("runLoop() returned without closing the device — the fd leaks on every retry")
}

// --- Watch: the retry loop that runs for the daemon's lifetime ---

// fastRetries shrinks the watcher's backoff so tests do not sleep for seconds.
func fastRetries(t *testing.T) {
	t.Helper()
	origSearch, origRetry := buttonSearchDelay, buttonRetryDelay
	buttonSearchDelay, buttonRetryDelay = time.Millisecond, time.Millisecond
	t.Cleanup(func() { buttonSearchDelay, buttonRetryDelay = origSearch, origRetry })
}

// stubOpener replaces openEventDevice and records how many times it was called.
func stubOpener(t *testing.T, fn func(path string) (eventDevice, error)) *int32 {
	t.Helper()
	var calls int32
	orig := openEventDevice
	openEventDevice = func(path string) (eventDevice, error) {
		atomic.AddInt32(&calls, 1)
		return fn(path)
	}
	t.Cleanup(func() { openEventDevice = orig })
	return &calls
}

func TestWatchRetriesWhenDeviceMissing(t *testing.T) {
	fakeInputSysfs(t) // empty: findDevice returns ""
	fastRetries(t)
	calls := stubOpener(t, func(string) (eventDevice, error) {
		t.Error("openEventDevice called even though no device was discovered")
		return nil, errors.New("unreachable")
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = testButtons().Watch(ctx, make(chan driver.ButtonEvent, 1)) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancellation")
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Errorf("openEventDevice calls = %d, want 0", got)
	}
}

// TestWatchReopensAfterReadError covers the recovery path used on
// suspend/resume: the read loop ends with an error and the watcher must reopen
// the device rather than give up.
func TestWatchReopensAfterReadError(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event3", testDeviceName)
	fastRetries(t)

	var mu sync.Mutex
	var opened []string
	calls := stubOpener(t, func(path string) (eventDevice, error) {
		mu.Lock()
		opened = append(opened, path)
		mu.Unlock()
		// Each device immediately fails its read, forcing a reopen.
		return &fakeEvdev{readErr: errors.New("device disappeared")}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = testButtons().Watch(ctx, make(chan driver.ButtonEvent, 1)) }()

	// Wait for several reopen cycles.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(calls) < 3 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancellation")
	}

	if got := atomic.LoadInt32(calls); got < 3 {
		t.Errorf("openEventDevice calls = %d, want >= 3 (watcher stopped reopening)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range opened {
		if p != "/dev/input/event3" {
			t.Errorf("opened %q, want /dev/input/event3", p)
		}
	}
}

// TestWatchRetriesWhenOpenFails is the InputPlumber case: the node exists
// but cannot be opened. The watcher must keep retrying, not exit.
func TestWatchRetriesWhenOpenFails(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event3", testDeviceName)
	fastRetries(t)
	calls := stubOpener(t, func(string) (eventDevice, error) {
		return nil, os.ErrPermission
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = testButtons().Watch(ctx, make(chan driver.ButtonEvent, 1)) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(calls) < 3 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancellation")
	}
	if got := atomic.LoadInt32(calls); got < 3 {
		t.Errorf("openEventDevice calls = %d, want >= 3 (watcher gave up on open failure)", got)
	}
}

func TestWatchForwardsPressThroughWatcher(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNode(t, root, "event3", testDeviceName)
	fastRetries(t)
	stubOpener(t, func(string) (eventDevice, error) {
		return &fakeEvdev{
			events:  []evdev.InputEvent{keyEvent(evdev.KEY_PROG3, 1)},
			readErr: errors.New("eof"),
		}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan driver.ButtonEvent, 1)
	done := make(chan struct{})
	go func() { defer close(done); _ = testButtons().Watch(ctx, ch) }()

	select {
	case ev := <-ch:
		if ev.Kind != testKind {
			t.Errorf("event kind = %q, want %q", ev.Kind, testKind)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("button press did not reach the channel through Watch")
	}

	// Wait for the watcher to exit before the test returns: its cleanup restores
	// the package-level delay vars this goroutine is still reading.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch did not return after cancellation")
	}
}
