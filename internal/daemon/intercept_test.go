package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
)

// fakeIntercept is InputPlumber as gamepad-intercept sees it.
type fakeIntercept struct {
	mu      sync.Mutex
	running bool
	owner   string
	comps   []inputplumber.Composite
	modes   map[dbus.ObjectPath]uint32
	setErr  error
	sets    []string // "path=mode", in order
	events  chan inputplumber.GamepadEvent
	changes chan struct{}
	closed  bool
}

func newFakeIntercept(comps ...inputplumber.Composite) *fakeIntercept {
	f := &fakeIntercept{running: true, owner: ":1.8", comps: comps, modes: map[dbus.ObjectPath]uint32{},
		events: make(chan inputplumber.GamepadEvent, 8), changes: make(chan struct{}, 1)}
	for _, c := range comps {
		f.modes[c.Path] = c.InterceptMode
	}
	return f
}

var (
	allyPad = inputplumber.Composite{Path: "/org/shadowblip/InputPlumber/CompositeDevice0", Name: "ASUS ROG Xbox Ally",
		Capabilities: []string{"Gamepad:Button:South", "Keyboard:KeyF16"}}
	keyboardOnly = inputplumber.Composite{Path: "/org/shadowblip/InputPlumber/CompositeDevice1", Name: "Asus WMI hotkeys",
		Capabilities: []string{"Keyboard:KeyProg1"}}
)

func (f *fakeIntercept) Running() bool { return f.running }
func (f *fakeIntercept) Owner() string { f.mu.Lock(); defer f.mu.Unlock(); return f.owner }
func (f *fakeIntercept) Composites() ([]inputplumber.Composite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]inputplumber.Composite, len(f.comps))
	for i, c := range f.comps {
		c.InterceptMode = f.modes[c.Path]
		out[i] = c
	}
	return out, nil
}
func (f *fakeIntercept) InterceptMode(p dbus.ObjectPath) (uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modes[p], nil
}
func (f *fakeIntercept) SetInterceptMode(p dbus.ObjectPath, m uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.modes[p] = m
	f.sets = append(f.sets, string(p)+"="+string(rune('0'+m)))
	return nil
}
func (f *fakeIntercept) WatchGamepad(ctx context.Context, ch chan<- inputplumber.GamepadEvent) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-f.events:
			ch <- ev
		}
	}
}
func (f *fakeIntercept) Changes(context.Context) (<-chan struct{}, error) { return f.changes, nil }
func (f *fakeIntercept) Close() error                                     { f.closed = true; return nil }
func (f *fakeIntercept) mode(p dbus.ObjectPath) uint32                    { m, _ := f.InterceptMode(p); return m }

// mustEngage engages and fails the test on a refusal.
func mustEngage(t *testing.T, i *interceptor, f *fakeIntercept) {
	t.Helper()
	if n, err := i.engage(f); n == 0 || err != nil {
		t.Fatalf("engage = %d, %v", n, err)
	}
}

// useFakeIntercept points the stream at f and the marker at a temp file.
func useFakeIntercept(t *testing.T, f *fakeIntercept) string {
	t.Helper()
	marker := t.TempDir() + "/voltaire/intercept.json"
	origConnect, origMarker := connectIntercept, interceptMarker
	connectIntercept = func() (interceptClient, error) { return f, nil }
	interceptMarker = func() string { return marker }
	t.Cleanup(func() { connectIntercept, interceptMarker = origConnect, origMarker })
	return marker
}

func TestInterceptTakesOnlyGamepadsAndGivesThemBack(t *testing.T) {
	f := newFakeIntercept(allyPad, keyboardOnly)
	marker := useFakeIntercept(t, f)
	var i interceptor

	n, err := i.engage(f)
	if n != 1 || err != nil {
		t.Fatalf("engage = %d, %v; want the one gamepad", n, err)
	}
	if f.mode(allyPad.Path) != inputplumber.InterceptAlways || f.mode(keyboardOnly.Path) != 0 {
		t.Errorf("modes: pad %d, keyboard %d; a keyboard composite must never be intercepted", f.mode(allyPad.Path), f.mode(keyboardOnly.Path))
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("no marker while intercepting: %v", err)
	}

	// A second client shares it; the first leaving keeps it.
	if n, _ := i.engage(f); n != 1 {
		t.Fatalf("second engage = %d", n)
	}
	i.release(f)
	if f.mode(allyPad.Path) != inputplumber.InterceptAlways {
		t.Error("the intercept was given back while a client still held it")
	}
	i.release(f)
	if f.mode(allyPad.Path) != inputplumber.InterceptNone {
		t.Errorf("last release left mode %d, want the original 0", f.mode(allyPad.Path))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("marker left after the last release: %v", err)
	}
	i.release(f) // an extra release is harmless
}

func TestInterceptRefusals(t *testing.T) {
	f := newFakeIntercept(keyboardOnly)
	useFakeIntercept(t, f)
	var i interceptor
	if n, err := i.engage(f); n != 0 || !errors.Is(err, errNoInterceptable) {
		t.Errorf("no gamepad: engage = %d, %v", n, err)
	}

	f = newFakeIntercept(allyPad)
	f.setErr = errors.New("org.freedesktop.DBus.Error.AccessDenied")
	if n, err := i.engage(f); n != 0 || err == nil {
		t.Errorf("polkit refusal: engage = %d, %v; want 0 and the error", n, err)
	}
	if i.clients != 0 {
		t.Errorf("a refused engage counted a client: %d", i.clients)
	}
}

func TestInterceptRestoresOnlyWhatIsStillOurs(t *testing.T) {
	t.Run("someone else changed it", func(t *testing.T) {
		f := newFakeIntercept(allyPad)
		useFakeIntercept(t, f)
		var i interceptor
		mustEngage(t, &i, f)
		_ = f.SetInterceptMode(allyPad.Path, 3) // OpenGamepadUI, say
		i.release(f)
		if f.mode(allyPad.Path) != 3 {
			t.Errorf("mode %d; a mode another client set must be left", f.mode(allyPad.Path))
		}
	})
	t.Run("it was already intercepting", func(t *testing.T) {
		pad := allyPad
		pad.InterceptMode = inputplumber.InterceptAlways
		f := newFakeIntercept(pad)
		useFakeIntercept(t, f)
		var i interceptor
		if n, _ := i.engage(f); n != 1 {
			t.Fatalf("engage = %d", n)
		}
		i.release(f)
		if f.mode(pad.Path) != inputplumber.InterceptAlways {
			t.Error("an intercept that predates us was undone")
		}
	})
	t.Run("InputPlumber restarted, then came back with the same paths", func(t *testing.T) {
		f := newFakeIntercept(allyPad)
		useFakeIntercept(t, f)
		var i interceptor
		mustEngage(t, &i, f)
		f.mu.Lock()
		f.owner, f.modes[allyPad.Path] = ":1.99", 0 // a restart resets every mode
		f.mu.Unlock()
		i.ensure(f) // the stream's change watch
		if f.mode(allyPad.Path) != inputplumber.InterceptAlways {
			t.Error("after an InputPlumber restart the controller was not taken again")
		}
		i.release(f)
		if f.mode(allyPad.Path) != 0 {
			t.Errorf("mode %d after release, want 0", f.mode(allyPad.Path))
		}
	})
}

func TestServeInterceptStreamsAndReleasesOnClose(t *testing.T) {
	f := newFakeIntercept(allyPad)
	useFakeIntercept(t, f)
	d := &Daemon{}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { d.serveIntercept(server); close(done) }()

	sc := bufio.NewScanner(client)
	var ack api.Response
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &ack) != nil || !ack.OK || ack.Value != "1" {
		t.Fatalf("ack = %+v (%s)", ack, sc.Bytes())
	}
	f.events <- inputplumber.GamepadEvent{Action: "accept", Pressed: true}
	var ev api.Response
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &ev) != nil {
		t.Fatalf("no event: %s", sc.Bytes())
	}
	if !ev.OK || ev.Event != api.StreamGamepad || ev.Gamepad == nil || *ev.Gamepad != (api.GamepadEvent{Action: "accept", Pressed: true}) {
		t.Errorf("event = %+v", ev)
	}

	_ = client.Close() // the GUI hides, or crashes
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end when the client closed")
	}
	if f.mode(allyPad.Path) != 0 || !f.closed {
		t.Errorf("after the client closed: mode %d, bus closed %v; want the controller given back", f.mode(allyPad.Path), f.closed)
	}
}

func TestServeInterceptRefusesWithoutAController(t *testing.T) {
	f := newFakeIntercept(keyboardOnly)
	useFakeIntercept(t, f)
	d := &Daemon{}
	server, client := net.Pipe()
	go d.serveIntercept(server)
	var resp api.Response
	sc := bufio.NewScanner(client)
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &resp) != nil {
		t.Fatal("no reply")
	}
	if resp.OK || resp.Error != errNoInterceptable.Error() {
		t.Errorf("reply = %+v", resp)
	}
}

func TestStopGivesTheControllerBack(t *testing.T) {
	f := newFakeIntercept(allyPad)
	useFakeIntercept(t, f)
	d := &Daemon{}
	server, client := net.Pipe()
	go d.serveIntercept(server)
	sc := bufio.NewScanner(client)
	sc.Scan() // ack
	d.icpt.stop(2 * time.Second)
	if f.mode(allyPad.Path) != 0 {
		t.Errorf("daemon shutdown left mode %d", f.mode(allyPad.Path))
	}
}

func TestRestoreStaleIntercept(t *testing.T) {
	f := newFakeIntercept(allyPad)
	marker := useFakeIntercept(t, f)
	var i interceptor
	mustEngage(t, &i, f) // a daemon that is then killed: no release
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	restoreStaleIntercept()
	if f.mode(allyPad.Path) != 0 {
		t.Errorf("the successor left mode %d", f.mode(allyPad.Path))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("marker not removed")
	}

	// A marker from before an InputPlumber restart restores nothing.
	i = interceptor{}
	mustEngage(t, &i, f)
	f.mu.Lock()
	f.owner = ":1.99"
	f.mu.Unlock()
	restoreStaleIntercept()
	if f.mode(allyPad.Path) != inputplumber.InterceptAlways {
		t.Error("a marker from another InputPlumber instance changed a mode")
	}
}
