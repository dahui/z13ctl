package evdevkey

// evdevkey_test.go — button watcher: device discovery and the read loop,
// driven by a fake evdev device so no hardware is required. These tests came
// from internal/daemon with the driver extraction; every assertion carried
// over, including the issue #10 regression guard.

import (
	"context"
	"errors"
	"os"
	"strings"
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
	testID         = "armoury-crate"
)

// testButtons returns a watcher configured as the Z13 device file configures it.
func testButtons() *Buttons { return New(testDeviceName, testKeycode, testID, false) }

// fakeInputSysfs builds a /sys/class/input stand-in and points inputClassDir at it.
func fakeInputSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := inputClassDir
	inputClassDir = root
	t.Cleanup(func() { inputClassDir = orig })
	return root
}

// Real key capability bitmaps, read from a GZ302EA on 2026-11-15.
const (
	// hotkeysCaps is "Asus WMI hotkeys": vendor and media keys, no typing
	// keys. udev sets ID_INPUT_KEY on it but not ID_INPUT_KEYBOARD.
	hotkeysCaps = "400000000000000 10000000c0000 400400000 800000000000 0 0 181606f00900c00 380027801701000 e000000000000 0"
	// atKeyboardCaps is "AT Translated Set 2 keyboard", a typing keyboard by
	// udev's test (ID_INPUT_KEYBOARD=1).
	atKeyboardCaps = "402000007 ff803078f800d001 feffffdfffcfffff fffffffffffffffe"
	// coverConsumerCaps is the detachable cover's consumer-key interface: a
	// lot of keys, but not the typing block, and udev agrees.
	coverConsumerCaps = "733eff 0 20000 483ffff17aff32d bfd4444600000000 1 1b30c730b17cc00 9fa67bfad9715fed 9e168000004400 10000002"
)

// addInputNode creates <root>/<node>/device/name containing name, with the
// hotkey node's key capabilities (not a typing keyboard).
func addInputNode(t *testing.T, root, node, name string) {
	t.Helper()
	addInputNodeCaps(t, root, node, name, hotkeysCaps)
}

// addInputNodeCaps is addInputNode with the key capability bitmap given; ""
// writes none, which makes the bitmap unreadable.
func addInputNodeCaps(t *testing.T, root, node, name, caps string) {
	t.Helper()
	dir := root + "/" + node + "/device"
	if err := os.MkdirAll(dir+"/capabilities", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(dir+"/name", []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if caps != "" {
		if err := os.WriteFile(dir+"/capabilities/key", []byte(caps+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
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
				if ev := <-ch; ev.ID != testID {
					t.Errorf("event id = %q, want %q", ev.ID, testID)
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
		if ev.ID != testID {
			t.Errorf("event id = %q, want %q", ev.ID, testID)
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

// The Z13's "Asus WMI hotkeys" key bitmap, read from the machine (2026-10-09).
const z13KeyBitmap = "400000000000000 10000000c0000 400400000 800000000000 0 0 181606f00900c00 380027801701000 e000000000000 0"

func TestBitmapHas(t *testing.T) {
	for _, tt := range []struct {
		bitmap string
		bit    int
		want   bool
	}{
		{z13KeyBitmap, testKeycode, true}, // KEY_PROG3, the Armoury Crate key
		{z13KeyBitmap, 148, true},         // KEY_PROG1: this node reports it too
		{z13KeyBitmap, 1, false},          // KEY_ESC
		{z13KeyBitmap, 0, false},
		{"1", 0, true},
		{"1 0", 64, true},
		{"1", 64, false}, // beyond the bitmap
	} {
		got, err := bitmapHas(tt.bitmap, tt.bit)
		if err != nil || got != tt.want {
			t.Errorf("bitmapHas(%q, %d) = %v, %v; want %v", tt.bitmap, tt.bit, got, err, tt.want)
		}
	}
	if _, err := bitmapHas("", 1); err == nil {
		t.Error("empty bitmap: want an error")
	}
	if _, err := bitmapHas("zz", 1); err == nil {
		t.Error("non-hex word: want an error")
	}
}

// The keyboard guard, runtime half: udev's own test on the static bitmap,
// against the real nodes of a GZ302EA.
func TestIsTypingKeyboardAgreesWithUdev(t *testing.T) {
	root := fakeInputSysfs(t)
	for node, c := range map[string]struct {
		caps string
		want bool
	}{
		// udev classes event2 as a typing keyboard and the other two only as
		// having keys; the test must agree on all three.
		"event2": {atKeyboardCaps, true},
		"event4": {hotkeysCaps, false},
		"event5": {coverConsumerCaps, false},
	} {
		addInputNodeCaps(t, root, node, node, c.caps)
		got, err := isTypingKeyboard(node)
		if err != nil {
			t.Fatalf("%s: %v", node, err)
		}
		if got != c.want {
			t.Errorf("isTypingKeyboard(%s) = %v, want %v", node, got, c.want)
		}
	}
}

// watchOnce runs b.Watch until it returns or the timeout passes, and reports
// how many times the device was opened and its error.
func watchOnce(t *testing.T, b *Buttons) (int32, error) {
	t.Helper()
	fastRetries(t)
	calls := stubOpener(t, func(string) (eventDevice, error) {
		return &fakeEvdev{readErr: errors.New("closed")}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := b.Watch(ctx, make(chan driver.ButtonEvent, 1))
	return atomic.LoadInt32(calls), err
}

func TestWatchRefusesATypingKeyboard(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNodeCaps(t, root, "event2", testDeviceName, atKeyboardCaps)
	opens, err := watchOnce(t, testButtons())
	if err == nil || !strings.Contains(err.Error(), "typing keyboard") {
		t.Errorf("Watch on a typing keyboard = %v, want a refusal naming it", err)
	}
	if opens != 0 {
		t.Errorf("the keyboard was opened %d times, want 0", opens)
	}
}

func TestWatchRefusesWhenTheBitmapIsUnreadable(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNodeCaps(t, root, "event3", testDeviceName, "")
	opens, err := watchOnce(t, testButtons())
	if err == nil || opens != 0 {
		t.Errorf("Watch with no readable bitmap = %v after %d opens, want a refusal before any open", err, opens)
	}
}

// keyboard_node is the opt-in: device data has said this keyboard carries only
// button chords on its model.
func TestWatchReadsAKeyboardNodeWhenOptedIn(t *testing.T) {
	root := fakeInputSysfs(t)
	addInputNodeCaps(t, root, "event2", testDeviceName, atKeyboardCaps)
	b := New(testDeviceName, testKeycode, testID, true)
	if opens, _ := watchOnce(t, b); opens == 0 {
		t.Error("an opted-in keyboard node was never opened")
	}
}

// The OneXPlayer button as HHD and Jeff's own notes record it: Ctrl+Alt+Meta
// in one ~10 ms pulse on the AT keyboard.
var oxpChord = []int{int(evdev.KEY_LEFTCTRL), int(evdev.KEY_LEFTALT), int(evdev.KEY_LEFTMETA)}

// chordPresses feeds events through press and counts the presses reported.
func chordPresses(b *Buttons, events ...evdev.InputEvent) int {
	held, fired, n := map[evdev.EvCode]bool{}, false, 0
	for _, e := range events {
		if b.press(held, &fired, e.Code, e.Value) {
			n++
		}
	}
	return n
}

func TestChordFiresOnceOnTheKeyThatCompletesIt(t *testing.T) {
	b := NewChord(testDeviceName, oxpChord, "oxp", true)
	down := func(c evdev.EvCode) evdev.InputEvent { return keyEvent(c, 1) }
	up := func(c evdev.EvCode) evdev.InputEvent { return keyEvent(c, 0) }
	ctrl, alt, meta, o := evdev.EvCode(evdev.KEY_LEFTCTRL), evdev.EvCode(evdev.KEY_LEFTALT), evdev.EvCode(evdev.KEY_LEFTMETA), evdev.EvCode(evdev.KEY_O)
	for _, c := range []struct {
		desc   string
		events []evdev.InputEvent
		want   int
	}{
		{"one pulse", []evdev.InputEvent{down(ctrl), down(alt), down(meta), up(meta), up(alt), up(ctrl)}, 1},
		{"in any order", []evdev.InputEvent{down(meta), down(ctrl), down(alt)}, 1},
		{"two pulses", []evdev.InputEvent{
			down(ctrl), down(alt), down(meta), up(ctrl), up(alt), up(meta),
			down(ctrl), down(alt), down(meta), up(ctrl), up(alt), up(meta)}, 2},
		{"auto-repeat while held", []evdev.InputEvent{down(ctrl), down(alt), down(meta), keyEvent(meta, 2), keyEvent(alt, 2)}, 1},
		{"re-armed by releasing one key", []evdev.InputEvent{down(ctrl), down(alt), down(meta), up(alt), down(alt)}, 2},
		{"incomplete: the Keyboard button's Ctrl+Meta+O", []evdev.InputEvent{down(ctrl), down(meta), down(o)}, 0},
		{"one key of it typed alone", []evdev.InputEvent{down(alt), up(alt)}, 0},
	} {
		if got := chordPresses(b, c.events...); got != c.want {
			t.Errorf("%s: %d presses, want %d", c.desc, got, c.want)
		}
	}
}

func TestChordReachesTheChannelThroughTheReadLoop(t *testing.T) {
	b := NewChord(testDeviceName, oxpChord, "oxp", true)
	dev := &fakeEvdev{readErr: errors.New("done"), events: []evdev.InputEvent{
		keyEvent(evdev.KEY_LEFTCTRL, 1), keyEvent(evdev.KEY_LEFTALT, 1), keyEvent(evdev.KEY_LEFTMETA, 1),
		keyEvent(evdev.KEY_LEFTMETA, 0), keyEvent(evdev.KEY_LEFTALT, 0), keyEvent(evdev.KEY_LEFTCTRL, 0),
	}}
	ch := make(chan driver.ButtonEvent, 4)
	_ = b.runLoop(context.Background(), dev, ch)
	if len(ch) != 1 {
		t.Fatalf("%d presses reached the channel, want 1", len(ch))
	}
	if ev := <-ch; ev.ID != "oxp" {
		t.Errorf("press id = %q, want oxp", ev.ID)
	}
}
