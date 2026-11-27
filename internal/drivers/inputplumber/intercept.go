package inputplumber

// intercept.go — InputPlumber's intercept mode, which voltaire-gui uses to take
// the controller while the drawer is open (.claude/plans/gui-gamepad-inputplumber.md).
//
// While a composite device is in InterceptAlways it stops feeding the virtual
// controller games read (measured on the Ally, 2026-11-26: the deck-uhid target
// kept sending one frozen idle report while every press went elsewhere) and
// sends each input to its DBus target as a named action instead. That works the
// same for every target type: an evdev pad (the OXP's xbox-elite) the GUI could
// have grabbed, and a hidraw-only one (the Ally's deck-uhid) it cannot open at
// all. Actions mapped to a DBus target, voltaire's button among them, arrive in
// every mode.
//
// polkit allows SetInterceptMode for wheel and inputplumber members and asks
// anyone else for an admin password; voltaire never adds anyone to
// inputplumber (that group also grants keystroke injection).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Intercept modes (CompositeDevice.InterceptMode): none passes input to the
// virtual controller, always sends all of it to DBus. (1 passes until an
// activation chord and 3 intercepts the gamepad only; voltaire uses neither.)
const (
	InterceptNone   uint32 = 0
	InterceptAlways uint32 = 2
)

// HasGamepad reports whether the composite device carries a gamepad, and so
// whether intercepting it takes a controller from games.
func (c Composite) HasGamepad() bool {
	for _, capability := range c.Capabilities {
		if strings.HasPrefix(capability, "Gamepad:") {
			return true
		}
	}
	return false
}

// InterceptMode reads a composite device's intercept mode.
func (c *Client) InterceptMode(path dbus.ObjectPath) (uint32, error) {
	v, err := c.conn.Object(BusName, path).GetProperty(compositeIface + ".InterceptMode")
	if err != nil {
		return 0, err
	}
	var mode uint32
	if err := v.Store(&mode); err != nil {
		return 0, err
	}
	return mode, nil
}

// SetInterceptMode sets a composite device's intercept mode.
func (c *Client) SetInterceptMode(path dbus.ObjectPath, mode uint32) error {
	return c.conn.Object(BusName, path).SetProperty(compositeIface+".InterceptMode", dbus.MakeVariant(mode))
}

// Owner is the unique bus name of the running InputPlumber, "" when it is not
// running. A different owner means InputPlumber restarted, which resets every
// composite device to InterceptNone.
func (c *Client) Owner() string { return nameOwner(c.conn) }

// GamepadEvent is one navigation press or release from an intercepted
// controller. Action is one of the values gamepadActions maps to.
type GamepadEvent struct {
	Action  string
	Pressed bool
}

// gamepadActions maps the DBus target's action names to the navigation actions
// the drawer uses: exactly what its evdev reader maps (the D-pad, A, B and the
// bumpers). The left stick arrives as the same four directions. Everything else
// an intercepted controller sends is dropped, so the stream is navigation, not
// a record of the controller.
var gamepadActions = map[string]string{
	"ui_up":     "up",
	"ui_down":   "down",
	"ui_left":   "left",
	"ui_right":  "right",
	"ui_accept": "accept",
	"ui_back":   "back",
	"ui_l1":     "l1",
	"ui_r1":     "r1",
}

// GamepadAction maps a DBus target action name to a navigation action.
func GamepadAction(name string) (string, bool) {
	a, ok := gamepadActions[name]
	return a, ok
}

// gamepadEventOf reads an InputEvent signal as a navigation event.
func gamepadEventOf(sig *dbus.Signal) (GamepadEvent, bool) {
	if sig == nil || sig.Name != dbusDeviceIface+".InputEvent" || len(sig.Body) != 2 {
		return GamepadEvent{}, false
	}
	name, ok1 := sig.Body[0].(string)
	value, ok2 := sig.Body[1].(float64)
	if !ok1 || !ok2 {
		return GamepadEvent{}, false
	}
	action, ok := GamepadAction(name)
	if !ok {
		return GamepadEvent{}, false
	}
	return GamepadEvent{Action: action, Pressed: value > 0.5}, true
}

// WatchGamepad delivers navigation events until ctx is done or the bus
// connection is lost. Only signals from the process owning InputPlumber's name
// count, so another local process cannot drive the drawer. A full channel
// drops the event rather than stalling the bus connection.
func (c *Client) WatchGamepad(ctx context.Context, ch chan<- GamepadEvent) error {
	if err := c.conn.AddMatchSignal(
		dbus.WithMatchSender(BusName),
		dbus.WithMatchInterface(dbusDeviceIface),
		dbus.WithMatchMember("InputEvent"),
	); err != nil {
		return err
	}
	sigs := make(chan *dbus.Signal, 32)
	c.conn.Signal(sigs)
	defer c.conn.RemoveSignal(sigs)
	owner := nameOwner(c.conn)
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-sigs:
			if !ok {
				return fmt.Errorf("bus connection closed")
			}
			ev, ok := gamepadEventOf(sig)
			if !ok {
				continue
			}
			if sig.Sender != owner {
				owner = nameOwner(c.conn) // InputPlumber may have restarted
				if sig.Sender != owner {
					slog.Warn("inputplumber: ignoring a controller event not sent by InputPlumber", "sender", sig.Sender)
					continue
				}
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}
