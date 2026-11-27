package inputplumber

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/dahui/voltaire/v2/internal/driver"
)

// InputPlumber's names on the system bus.
const (
	BusName         = "org.shadowblip.InputPlumber"
	rootPath        = "/org/shadowblip/InputPlumber"
	compositeIface  = "org.shadowblip.Input.CompositeDevice"
	dbusDeviceIface = "org.shadowblip.Input.DBusDevice"
)

// Via marks a ButtonEvent that arrived through InputPlumber, so the daemon can
// keep one press from counting twice when a direct watcher also sees it.
const Via = "inputplumber"

// Composite is one InputPlumber composite device, as far as voltaire needs it.
type Composite struct {
	Path        dbus.ObjectPath
	Name        string
	ProfileName string
	ProfilePath string
	// SourceDevicePaths are the device nodes InputPlumber took over for it
	// ("/dev/input/event13", "/dev/hidraw4").
	SourceDevicePaths []string
	// Capabilities are its input capabilities ("Gamepad:Button:South", ...);
	// HasGamepad reads them.
	Capabilities []string
	// InterceptMode is its intercept mode when listed (intercept.go).
	InterceptMode uint32
}

// Client talks to InputPlumber over a private system-bus connection, so its
// signal subscriptions never mix with the daemon's other DBus users.
type Client struct{ conn *dbus.Conn }

// Connect opens the connection. It succeeds whether or not InputPlumber is
// running; Running says which.
func Connect() (*Client, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Running reports whether InputPlumber owns its bus name.
func (c *Client) Running() bool {
	var has bool
	err := c.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, BusName).Store(&has)
	return err == nil && has
}

// Composites lists InputPlumber's composite devices: the children of its root
// object named CompositeDevice<N>. It has no ObjectManager to ask instead.
func (c *Client) Composites() ([]Composite, error) {
	node, err := introspect.Call(c.conn.Object(BusName, rootPath))
	if err != nil {
		return nil, err
	}
	var out []Composite
	for _, child := range node.Children {
		if !strings.HasPrefix(child.Name, "CompositeDevice") {
			continue
		}
		path := dbus.ObjectPath(rootPath + "/" + child.Name)
		var props map[string]dbus.Variant
		if err := c.conn.Object(BusName, path).Call("org.freedesktop.DBus.Properties.GetAll", 0, compositeIface).Store(&props); err != nil {
			slog.Debug("inputplumber: composite device unreadable", "path", path, "err", err)
			continue
		}
		cd := Composite{Path: path}
		_ = props["Name"].Store(&cd.Name)
		_ = props["ProfileName"].Store(&cd.ProfileName)
		_ = props["ProfilePath"].Store(&cd.ProfilePath)
		_ = props["SourceDevicePaths"].Store(&cd.SourceDevicePaths)
		_ = props["Capabilities"].Store(&cd.Capabilities)
		_ = props["InterceptMode"].Store(&cd.InterceptMode)
		out = append(out, cd)
	}
	return out, nil
}

// ProfileYAML returns the composite device's current profile.
func (c *Client) ProfileYAML(path dbus.ObjectPath) ([]byte, error) {
	var y string
	if err := c.conn.Object(BusName, path).Call(compositeIface+".GetProfileYaml", 0).Store(&y); err != nil {
		return nil, err
	}
	return []byte(y), nil
}

// LoadProfileYAML loads a profile into the composite device. InputPlumber
// keeps it in memory only; nothing is written to disk.
func (c *Client) LoadProfileYAML(path dbus.ObjectPath, profile []byte) error {
	return c.conn.Object(BusName, path).Call(compositeIface+".LoadProfileFromYaml", 0, string(profile)).Err
}

// Changes delivers a value whenever something that can undo or move voltaire's
// mapping may have happened: InputPlumber starting or stopping (its bus name
// changing hands), or any property changing under its tree — a profile loaded
// (ProfileName/ProfilePath), source devices added or removed, a composite
// device appearing (the Manager's GamepadOrder). Coalesced: a burst is one
// value. The channel closes when ctx is done.
func (c *Client) Changes(ctx context.Context) (<-chan struct{}, error) {
	if err := c.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, BusName),
	); err != nil {
		return nil, err
	}
	if err := c.conn.AddMatchSignal(
		dbus.WithMatchSender(BusName),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchPathNamespace(rootPath),
	); err != nil {
		return nil, err
	}
	sigs := make(chan *dbus.Signal, 16)
	c.conn.Signal(sigs)
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		defer c.conn.RemoveSignal(sigs)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-sigs:
				if !ok {
					return
				}
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out, nil
}

// Buttons is the driver.Buttons half of the route: it turns voltaire's
// InputEvent actions into presses. Only signals from the process that owns
// InputPlumber's name count, so another local process cannot fake a press.
type Buttons struct{}

// NewButtons returns the watcher. Pure: it connects once Watch runs.
func NewButtons() *Buttons { return &Buttons{} }

// retryDelay is how long Watch waits before reconnecting a lost bus.
var retryDelay = 5 * time.Second

// Watch blocks until ctx is done, delivering a ButtonEvent (Via set) for each
// press of a voltaire action: value 1 is a press, 0 its release. It runs
// whether or not InputPlumber is: the match rule is held by the bus, so
// presses flow as soon as InputPlumber starts and a profile maps one.
func (b *Buttons) Watch(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	for ctx.Err() == nil {
		if err := b.watchOnce(ctx, ch); err != nil {
			slog.Info("inputplumber: button watcher stopped; retrying", "err", err, "delay", retryDelay)
			select {
			case <-ctx.Done():
			case <-time.After(retryDelay):
			}
		}
	}
	return nil
}

func (b *Buttons) watchOnce(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface(dbusDeviceIface),
		dbus.WithMatchMember("InputEvent"),
	); err != nil {
		return err
	}
	sigs := make(chan *dbus.Signal, 16)
	conn.Signal(sigs)
	owner := nameOwner(conn)
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-sigs:
			if !ok {
				return fmt.Errorf("bus connection closed")
			}
			id, pressed := pressOf(sig)
			if id == "" || !pressed {
				continue
			}
			if sig.Sender != owner {
				owner = nameOwner(conn) // InputPlumber may have restarted
				if sig.Sender != owner {
					slog.Warn("inputplumber: ignoring a voltaire action not sent by InputPlumber", "sender", sig.Sender)
					continue
				}
			}
			slog.Info("hardware button pressed", "button", id, "via", Via)
			select {
			case ch <- driver.ButtonEvent{ID: id, Via: Via}:
			default:
			}
		}
	}
}

// pressOf reads an InputEvent signal: the button it names, if it is one of
// voltaire's actions, and whether it is a press rather than a release.
func pressOf(sig *dbus.Signal) (id string, pressed bool) {
	if sig == nil || sig.Name != dbusDeviceIface+".InputEvent" || len(sig.Body) != 2 {
		return "", false
	}
	action, ok1 := sig.Body[0].(string)
	value, ok2 := sig.Body[1].(float64)
	if !ok1 || !ok2 {
		return "", false
	}
	id, ok := ButtonID(action)
	if !ok {
		return "", false
	}
	return id, value > 0.5
}

// nameOwner is the unique name currently owning InputPlumber's bus name, ""
// when it is not running.
func nameOwner(conn *dbus.Conn) string {
	var owner string
	if conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, BusName).Store(&owner) != nil {
		return ""
	}
	return owner
}
