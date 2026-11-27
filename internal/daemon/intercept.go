package daemon

// intercept.go — gamepad-intercept: the controller taken from games while
// voltaire-gui is open, on machines where InputPlumber manages it
// (.claude/plans/gui-gamepad-inputplumber.md, docs/decisions).
//
// InputPlumber hides the controller it manages and presents a virtual one to
// games, which on the Ally is hidraw only: the GUI's evdev reader has nothing
// to read or grab, and Steam keeps the input. Its intercept mode stops feeding
// the virtual controller and sends navigation over DBus instead, whatever the
// target type, which this streams to the client.
//
// The daemon owns the mode, not the GUI, because the failure that matters is a
// client vanishing with intercept on: games would get no controller until
// InputPlumber restarted. So the intercept lives exactly as long as a client's
// connection: the first client engages it, the last one to close (or crash)
// restores the mode found, and so does the daemon stopping. A daemon killed
// outright leaves a marker in its runtime directory, and the next one restores
// from it at startup.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
)

// interceptClient is what gamepad-intercept needs from InputPlumber;
// *inputplumber.Client in the daemon, a fake in tests.
type interceptClient interface {
	Running() bool
	Owner() string
	Composites() ([]inputplumber.Composite, error)
	InterceptMode(path dbus.ObjectPath) (uint32, error)
	SetInterceptMode(path dbus.ObjectPath, mode uint32) error
	WatchGamepad(ctx context.Context, ch chan<- inputplumber.GamepadEvent) error
	Changes(ctx context.Context) (<-chan struct{}, error)
	Close() error
}

// connectIntercept opens a private InputPlumber connection per stream. A var
// so tests can supply a fake.
var connectIntercept = func() (interceptClient, error) { return inputplumber.Connect() }

// interceptMarker is where the held modes are recorded while intercepting, so
// a daemon that did not get to restore them (killed, or exiting on a session
// change) has its successor do it. In the runtime directory: InputPlumber
// resets every mode when it restarts, and the runtime directory goes at
// reboot with it. A var so tests can redirect it.
var interceptMarker = func() string { return filepath.Dir(api.SocketPath()) + "/intercept.json" }

// errNoInterceptable is the refusal when InputPlumber manages no controller.
var errNoInterceptable = errors.New("InputPlumber manages no controller on this machine")

// interceptor is the shared intercept state: how many clients hold it, and
// the mode each intercepted composite device had before.
type interceptor struct {
	mu      sync.Mutex
	clients int
	owner   string                     // InputPlumber's unique bus name when the modes were recorded
	held    map[dbus.ObjectPath]uint32 // composite device → its mode before ours
	conns   map[net.Conn]bool          // open streams, closed on shutdown
}

// heldRecord is the marker's content.
type heldRecord struct {
	Owner string            `json:"owner"`
	Held  map[string]uint32 `json:"held"`
}

// engage counts a client in and makes sure every composite device carrying a
// gamepad is intercepted. It returns how many are held; with none the client
// is not counted.
func (i *interceptor) engage(c interceptClient) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	n, err := i.ensureLocked(c)
	if n == 0 {
		if err == nil {
			err = errNoInterceptable
		}
		return 0, err
	}
	i.clients++
	return n, nil
}

// ensure re-checks while held: InputPlumber restarting (every mode reset) or a
// controller appearing. A no-op once nobody holds the intercept.
func (i *interceptor) ensure(c interceptClient) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.clients > 0 {
		_, _ = i.ensureLocked(c)
	}
}

// ensureLocked intercepts every gamepad composite device not yet held,
// recording the mode it had. A device already held is left alone even if its
// mode has moved: another client (OpenGamepadUI) changed it, and fighting it
// would be worse than a controller that reaches the game.
func (i *interceptor) ensureLocked(c interceptClient) (int, error) {
	if owner := c.Owner(); owner != i.owner {
		i.owner, i.held = owner, nil // a restarted InputPlumber holds nothing of ours
	}
	if i.held == nil {
		i.held = map[dbus.ObjectPath]uint32{}
	}
	comps, err := c.Composites()
	if err != nil {
		return len(i.held), err
	}
	var firstErr error
	for _, cd := range comps {
		if !cd.HasGamepad() {
			continue // a keyboard composite: intercepting it would swallow typing
		}
		if _, ok := i.held[cd.Path]; ok {
			continue
		}
		if cd.InterceptMode != inputplumber.InterceptAlways {
			if err := c.SetInterceptMode(cd.Path, inputplumber.InterceptAlways); err != nil {
				slog.Warn("inputplumber: cannot take the controller (is this user in wheel?)", "device", cd.Name, "err", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		if cd.InterceptMode == inputplumber.InterceptAlways {
			slog.Info("inputplumber: the controller was already intercepted; sharing it", "device", cd.Name)
		} else {
			slog.Info("inputplumber: took the controller for the drawer", "device", cd.Name, "mode_was", cd.InterceptMode)
		}
		i.held[cd.Path] = cd.InterceptMode
	}
	i.writeMarkerLocked()
	return len(i.held), firstErr
}

// release counts a client out; the last one restores every held device.
func (i *interceptor) release(c interceptClient) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.clients == 0 {
		return
	}
	i.clients--
	if i.clients > 0 {
		return
	}
	restoreHeld(c, i.owner, i.held)
	i.held = nil
	if err := os.Remove(interceptMarker()); err != nil && !os.IsNotExist(err) {
		slog.Warn("inputplumber: cannot remove the intercept marker", "err", err)
	}
}

// restoreHeld puts each held device back to the mode it had, but only where
// the mode is still ours: same InputPlumber, still InterceptAlways, and not
// already intercepting before we came. Anything else was changed by someone
// else since, or reset by a restart, and is left as it is.
func restoreHeld(c interceptClient, owner string, held map[dbus.ObjectPath]uint32) {
	if len(held) == 0 || c.Owner() != owner || owner == "" {
		return
	}
	for path, was := range held {
		if was == inputplumber.InterceptAlways {
			continue
		}
		now, err := c.InterceptMode(path)
		if err != nil || now != inputplumber.InterceptAlways {
			continue
		}
		if err := c.SetInterceptMode(path, was); err != nil {
			slog.Warn("inputplumber: cannot give the controller back", "path", path, "err", err)
			continue
		}
		slog.Info("inputplumber: gave the controller back", "path", path, "mode", was)
	}
}

// writeMarkerLocked records the held modes for a successor daemon.
func (i *interceptor) writeMarkerLocked() {
	rec := heldRecord{Owner: i.owner, Held: map[string]uint32{}}
	for p, m := range i.held {
		rec.Held[string(p)] = m
	}
	data, _ := json.Marshal(rec)
	path := interceptMarker()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		err = os.WriteFile(path, data, 0o600)
		if err == nil {
			return
		}
	}
	slog.Warn("inputplumber: cannot record the intercept; a killed daemon would leave it on", "path", path)
}

// track and untrack keep the open streams, for stop.
func (i *interceptor) track(conn net.Conn) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.conns == nil {
		i.conns = map[net.Conn]bool{}
	}
	i.conns[conn] = true
}

func (i *interceptor) untrack(conn net.Conn) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.conns, conn)
}

// stop closes every open stream and waits, bounded, for the last release to
// give the controller back. Shutdown never hangs on DBus.
func (i *interceptor) stop(timeout time.Duration) {
	i.mu.Lock()
	for conn := range i.conns {
		_ = conn.Close()
	}
	i.mu.Unlock()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		i.mu.Lock()
		n := i.clients
		i.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	slog.Warn("inputplumber: shutdown did not finish giving the controller back in time")
}

// interceptWriteTimeout bounds one event write to a stream; a client that
// stops reading is dropped, which gives the controller back. A var for tests.
var interceptWriteTimeout = 2 * time.Second

// serveIntercept runs one gamepad-intercept stream until the client closes the
// connection, the bus goes, or the daemon stops.
func (d *Daemon) serveIntercept(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	refuse := func(msg string) {
		slog.Info("gamepad-intercept refused", "reason", msg)
		writeResponse(conn, response{OK: false, Error: msg})
	}
	c, err := connectIntercept()
	if err != nil {
		refuse("cannot reach the system bus: " + err.Error())
		return
	}
	defer func() { _ = c.Close() }()
	if !c.Running() {
		refuse("InputPlumber is not running")
		return
	}
	n, err := d.icpt.engage(c)
	if n == 0 {
		refuse(err.Error())
		return
	}
	defer d.icpt.release(c)
	d.icpt.track(conn)
	defer d.icpt.untrack(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan inputplumber.GamepadEvent, 32)
	go func() {
		if werr := c.WatchGamepad(ctx, events); werr != nil {
			slog.Info("gamepad-intercept: event watch ended", "err", werr)
		}
		cancel()
	}()
	changes, err := c.Changes(ctx)
	if err != nil {
		slog.Info("gamepad-intercept: cannot watch InputPlumber for restarts", "err", err)
	}
	go func() { // the client sends nothing more; a read returning means it closed
		_, _ = io.Copy(io.Discard, conn)
		cancel()
	}()
	writeResponse(conn, response{OK: true, Value: strconv.Itoa(n)})

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changes:
			if !ok {
				changes = nil
				continue
			}
			d.icpt.ensure(c)
		case ev := <-events:
			data, _ := json.Marshal(response{OK: true, Event: api.StreamGamepad,
				Gamepad: &api.GamepadEvent{Action: ev.Action, Pressed: ev.Pressed}})
			_ = conn.SetWriteDeadline(time.Now().Add(interceptWriteTimeout))
			_, werr := conn.Write(append(data, '\n'))
			_ = conn.SetWriteDeadline(time.Time{})
			if werr != nil {
				return
			}
		}
	}
}

// restoreStaleIntercept gives the controller back from a predecessor that did
// not: it found a marker, so a daemon held the intercept and never released
// it. Only modes still ours under the same InputPlumber are restored.
func restoreStaleIntercept() {
	path := interceptMarker()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(path) }()
	var rec heldRecord
	if json.Unmarshal(data, &rec) != nil {
		return
	}
	c, err := connectIntercept()
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()
	held := map[dbus.ObjectPath]uint32{}
	for p, m := range rec.Held {
		held[dbus.ObjectPath(p)] = m
	}
	slog.Info("inputplumber: a previous daemon left the controller taken; giving it back", "devices", len(held))
	restoreHeld(c, rec.Owner, held)
}
