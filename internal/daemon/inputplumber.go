package daemon

// The InputPlumber route for hardware buttons (.claude/plans/inputplumber-buttons.md).
//
// InputPlumber grabs and hides the nodes of a device it manages, so the button
// that opens voltaire reaches us there only as a DBus action its profile maps
// the button to. This watcher keeps that mapping in the running profile of
// every composite device that took the chosen button's node over, and out of
// every other, and tells the press path which buttons InputPlumber carries so
// a press counts from one route only.
//
// It re-applies whenever the mapping may have been lost: InputPlumber
// restarting (a fresh daemon starts every device on its default profile),
// another client loading a profile, a composite device appearing, the user
// choosing another button — and every ipBackstop as a safety net. A pass that
// finds the mapping already in place loads nothing, so re-checking is cheap
// and cannot fight anyone. On shutdown the mapping is taken out again, giving
// the button back its previous job.

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
)

// Timing for the route watcher. Vars so tests can shorten them.
var (
	ipSettle   = 300 * time.Millisecond // let a burst of property changes finish
	ipBackstop = 30 * time.Second       // re-check with no signal at all
	ipRetry    = 10 * time.Second       // bus unavailable
	ipCleanup  = 2 * time.Second        // what shutdown waits for the mapping to come out
)

// ipClient is what the route needs from InputPlumber; *inputplumber.Client
// in the daemon, a fake in tests.
type ipClient interface {
	Running() bool
	Composites() ([]inputplumber.Composite, error)
	ProfileYAML(path dbus.ObjectPath) ([]byte, error)
	LoadProfileYAML(path dbus.ObjectPath, profile []byte) error
}

// ipButton is the chosen button as the route sees it: its InputPlumber
// capability and the node its own driver reads.
type ipButton struct {
	ID, Capability, Node string
}

// ipTarget decides which composite devices should carry the mapping: those
// whose source devices include the chosen button's node. A chosen button with
// no capability or no node present goes nowhere.
func ipTarget(chosen ipButton, comps []inputplumber.Composite) map[dbus.ObjectPath]bool {
	want := map[dbus.ObjectPath]bool{}
	if chosen.ID == "" || chosen.Capability == "" || chosen.Node == "" {
		return want
	}
	for _, c := range comps {
		if slices.Contains(c.SourceDevicePaths, chosen.Node) {
			want[c.Path] = true
		}
	}
	return want
}

// ipReconcile makes InputPlumber's profiles agree with the choice: the mapping
// on every target device, voltaire's mappings off every device it was loaded
// on before (or, on the first pass, off every device, for a mapping a crashed
// daemon left behind). It returns the devices now carrying the mapping and
// whether the chosen button is carried.
func ipReconcile(c ipClient, chosen ipButton, loaded map[dbus.ObjectPath]bool, first bool) (map[dbus.ObjectPath]bool, bool) {
	now := map[dbus.ObjectPath]bool{}
	if !c.Running() {
		return now, false
	}
	comps, err := c.Composites()
	if err != nil {
		slog.Warn("inputplumber: listing composite devices failed", "err", err)
		return loaded, len(loaded) > 0 // keep the last known route

	}
	want := ipTarget(chosen, comps)
	for _, cd := range comps {
		capability, action := "", ""
		if want[cd.Path] {
			capability, action = chosen.Capability, inputplumber.Action(chosen.ID)
		} else if !first && !loaded[cd.Path] {
			continue // never ours: leave its profile unread
		}
		profile, err := c.ProfileYAML(cd.Path)
		if err != nil {
			slog.Warn("inputplumber: reading the profile failed (is this user in wheel or the inputplumber group?)",
				"device", cd.Name, "err", err)
			continue
		}
		out, changed, err := inputplumber.SetMapping(profile, capability, action)
		if err != nil {
			slog.Warn("inputplumber: profile not understood; left alone", "device", cd.Name, "err", err)
			continue
		}
		if changed {
			if err := c.LoadProfileYAML(cd.Path, out); err != nil {
				slog.Warn("inputplumber: loading the profile failed", "device", cd.Name, "err", err)
				continue
			}
			if capability != "" {
				slog.Info("inputplumber: mapped the button that opens voltaire", "device", cd.Name,
					"button", chosen.ID, "capability", capability, "profile", cd.ProfileName)
			} else {
				slog.Info("inputplumber: removed voltaire's mapping", "device", cd.Name)
			}
		}
		if capability != "" {
			now[cd.Path] = true
		}
	}
	return now, len(now) > 0
}

// chosenIPButton is the button that opens voltaire, with what the route needs.
func (d *Daemon) chosenIPButton() ipButton {
	id := d.drawerButton()
	if id == "" {
		return ipButton{}
	}
	for _, b := range d.hw.ButtonList {
		if b.ID == id {
			node := ""
			if b.Node != nil {
				node = b.Node()
			}
			return ipButton{ID: b.ID, Capability: b.InputPlumber, Node: node}
		}
	}
	return ipButton{}
}

// usesInputPlumber reports whether any button the device declares can route
// through InputPlumber; without one the watcher is not started.
func (d *Daemon) usesInputPlumber() bool {
	for _, b := range d.hw.ButtonList {
		if b.InputPlumber != "" {
			return true
		}
	}
	return false
}

// kickInputPlumber asks the route watcher to re-check now (the choice of
// button changed). Non-blocking; a no-op when the watcher is not running.
func (d *Daemon) kickInputPlumber() {
	if d.ipKick == nil {
		return
	}
	select {
	case d.ipKick <- struct{}{}:
	default:
	}
}

// setIPCarried records which button InputPlumber carries, for the press path.
func (d *Daemon) setIPCarried(id string, carried bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !carried {
		id = ""
	}
	if d.ipCarried != id {
		slog.Info("inputplumber: button route", "carried", id != "", "button", id)
	}
	d.ipCarried = id
}

// watchInputPlumber keeps the mapping in place until ctx is done, then takes
// it out and closes d.ipDone.
func (d *Daemon) watchInputPlumber(ctx context.Context) {
	defer close(d.ipDone)
	for ctx.Err() == nil {
		c, err := inputplumber.Connect()
		if err != nil {
			slog.Info("inputplumber: system bus unavailable; retrying", "err", err, "delay", ipRetry)
			select {
			case <-ctx.Done():
				return
			case <-time.After(ipRetry):
			}
			continue
		}
		d.runInputPlumber(ctx, c)
		_ = c.Close()
	}
}

// runInputPlumber is one connection's lifetime of watchInputPlumber.
func (d *Daemon) runInputPlumber(ctx context.Context, c *inputplumber.Client) {
	changes, err := c.Changes(ctx)
	if err != nil {
		slog.Info("inputplumber: cannot watch for changes; retrying", "err", err, "delay", ipRetry)
		select {
		case <-ctx.Done():
		case <-time.After(ipRetry):
		}
		return
	}
	loaded, first := map[dbus.ObjectPath]bool{}, true
	pass := func() {
		chosen := d.chosenIPButton()
		var carried bool
		loaded, carried = ipReconcile(c, chosen, loaded, first)
		first = false
		d.setIPCarried(chosen.ID, carried)
	}
	pass()
	backstop := time.NewTicker(ipBackstop)
	defer backstop.Stop()
	for {
		select {
		case <-ctx.Done():
			if len(loaded) > 0 {
				ipReconcile(c, ipButton{}, loaded, false) // take ours out
			}
			return
		case _, ok := <-changes:
			if !ok {
				return
			}
			select { // settle, then one pass for the whole burst
			case <-ctx.Done():
				continue
			case <-time.After(ipSettle):
			}
			drain(changes)
			pass()
		case <-d.ipKick:
			pass()
		case <-backstop.C:
			pass()
		}
	}
}

// drain empties a coalescing channel without blocking.
func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// waitInputPlumberCleanup gives the route watcher a moment to take the mapping
// out after ctx is cancelled, so a stopped daemon does not leave the button
// mapped to an action nobody hears. Bounded: shutdown never hangs on DBus.
func (d *Daemon) waitInputPlumberCleanup() {
	if d.ipDone == nil {
		return
	}
	select {
	case <-d.ipDone:
	case <-time.After(ipCleanup):
		slog.Warn("inputplumber: shutdown did not finish removing the mapping in time")
	}
}

// pressRoute decides whether a press of the chosen button counts, by the route
// it came: through InputPlumber exactly when InputPlumber carries the button
// (carried is that button's id), and from the button's own driver otherwise.
// A press can arrive both ways — a hidraw node opened before InputPlumber hid
// it keeps delivering, and a mapping left behind keeps sending — and both
// would toggle the drawer.
func pressRoute(ev driver.ButtonEvent, carried string) bool {
	viaIP := ev.Via == inputplumber.Via
	return viaIP == (carried != "" && carried == ev.ID)
}
