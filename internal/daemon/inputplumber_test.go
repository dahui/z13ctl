package daemon

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/drivers/inputplumber"
)

// fakeIP is InputPlumber with profiles in memory. Each load is counted, so a
// test can tell a pass that rewrote a profile from one that only looked.
type fakeIP struct {
	running  bool
	comps    []inputplumber.Composite
	profiles map[dbus.ObjectPath][]byte
	loads    map[dbus.ObjectPath]int
	reads    int
	readErr  error
}

func newFakeIP(t *testing.T) *fakeIP {
	t.Helper()
	def, err := os.ReadFile("../drivers/inputplumber/testdata/oxp-x2-mini-pro-default.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return &fakeIP{
		running: true,
		comps: []inputplumber.Composite{
			{Path: "/cd0", Name: "ONEXPLAYER X2 Mini Pro", ProfileName: "Default",
				SourceDevicePaths: []string{"/dev/input/event5", "/dev/input/event2", "/dev/hidraw4", "/dev/input/event13", "/dev/hidraw1"}},
			{Path: "/cd1", Name: "Some external pad", SourceDevicePaths: []string{"/dev/input/event30"}},
		},
		profiles: map[dbus.ObjectPath][]byte{"/cd0": def, "/cd1": def},
		loads:    map[dbus.ObjectPath]int{},
	}
}

func (f *fakeIP) Running() bool                                 { return f.running }
func (f *fakeIP) Composites() ([]inputplumber.Composite, error) { return f.comps, nil }
func (f *fakeIP) ProfileYAML(p dbus.ObjectPath) ([]byte, error) {
	f.reads++
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.profiles[p], nil
}
func (f *fakeIP) LoadProfileYAML(p dbus.ObjectPath, b []byte) error {
	f.loads[p]++
	f.profiles[p] = b
	return nil
}

func (f *fakeIP) mapped(p dbus.ObjectPath, action string) bool {
	return strings.Contains(string(f.profiles[p]), "dbus: "+action)
}

var oxpKeyboard = ipButton{ID: "keyboard", Capability: "QuickAccess2", Node: "/dev/input/event13"}

func TestIPReconcileMapsOnlyTheDeviceHoldingTheNode(t *testing.T) {
	f := newFakeIP(t)
	loaded, carried := ipReconcile(f, oxpKeyboard, nil, true)
	if !carried || !loaded["/cd0"] || len(loaded) != 1 {
		t.Fatalf("loaded %v carried %v; want only /cd0", loaded, carried)
	}
	if !f.mapped("/cd0", "voltaire_keyboard") || f.mapped("/cd1", "voltaire_keyboard") {
		t.Error("the mapping must be on the device that took the node over, and only there")
	}
	if f.loads["/cd1"] != 0 {
		t.Error("a device without the node, and without a mapping of ours, must not be reloaded")
	}

	// A re-check finds it in place: nothing loaded, so a signal storm or the
	// backstop cannot fight another client.
	before := f.loads["/cd0"]
	if _, carried := ipReconcile(f, oxpKeyboard, loaded, false); !carried || f.loads["/cd0"] != before {
		t.Errorf("re-check reloaded (%d → %d) or lost the route", before, f.loads["/cd0"])
	}
}

func TestIPReconcileFollowsTheChoiceAndCleansUp(t *testing.T) {
	f := newFakeIP(t)
	loaded, _ := ipReconcile(f, oxpKeyboard, nil, true)

	// The user picks Home (InputPlumber's Keyboard capability).
	home := ipButton{ID: "home", Capability: "Keyboard", Node: "/dev/hidraw4"}
	loaded, carried := ipReconcile(f, home, loaded, false)
	if !carried || !f.mapped("/cd0", "voltaire_home") || f.mapped("/cd0", "voltaire_keyboard") {
		t.Fatalf("moving to Home: carried %v\n%s", carried, f.profiles["/cd0"])
	}

	// "No button": the mapping comes out and the profile is the default again.
	loaded, carried = ipReconcile(f, ipButton{}, loaded, false)
	if carried || len(loaded) != 0 || strings.Contains(string(f.profiles["/cd0"]), "voltaire") {
		t.Errorf("removal: carried %v loaded %v\n%s", carried, loaded, f.profiles["/cd0"])
	}
}

// A mapping a crashed daemon left behind is found on the first pass, on any
// device, even one the new choice does not touch.
func TestIPReconcileFirstPassRemovesLeftovers(t *testing.T) {
	f := newFakeIP(t)
	ipReconcile(f, ipButton{ID: "oxp", Capability: "QuickAccess", Node: "/dev/input/event30"}, nil, true) // left on /cd1
	if !f.mapped("/cd1", "voltaire_oxp") {
		t.Fatal("setup: expected a mapping on /cd1")
	}
	ipReconcile(f, oxpKeyboard, nil, true) // a fresh daemon, choosing Keyboard
	if f.mapped("/cd1", "voltaire_oxp") {
		t.Error("the first pass must take a leftover mapping out")
	}
	reads := f.reads
	ipReconcile(f, oxpKeyboard, map[dbus.ObjectPath]bool{"/cd0": true}, false)
	if f.reads-reads != 1 {
		t.Errorf("a later pass read %d profiles; want only the device it maps", f.reads-reads)
	}
}

func TestIPReconcileWithoutInputPlumber(t *testing.T) {
	f := newFakeIP(t)
	f.running = false
	if loaded, carried := ipReconcile(f, oxpKeyboard, map[dbus.ObjectPath]bool{"/cd0": true}, false); carried || len(loaded) != 0 {
		t.Errorf("InputPlumber not running: loaded %v carried %v", loaded, carried)
	}
	f.running = true
	f.readErr = errors.New("not authorized")
	if _, carried := ipReconcile(f, oxpKeyboard, nil, true); carried {
		t.Error("a profile that cannot be read cannot carry the button")
	}
	if len(f.loads) != 0 {
		t.Error("nothing may be loaded without reading first")
	}
}

func TestIPTargetNeedsCapabilityAndNode(t *testing.T) {
	comps := newFakeIP(t).comps
	for _, b := range []ipButton{
		{ID: "keyboard", Node: "/dev/input/event13"},                             // no capability in device data
		{ID: "keyboard", Capability: "QuickAccess2"},                             // node absent
		{ID: "keyboard", Capability: "QuickAccess2", Node: "/dev/input/event99"}, // InputPlumber does not hold it
		{},
	} {
		if got := ipTarget(b, comps); len(got) != 0 {
			t.Errorf("ipTarget(%+v) = %v, want none", b, got)
		}
	}
}

func TestPressRoute(t *testing.T) {
	direct := driver.ButtonEvent{ID: "keyboard"}
	viaIP := driver.ButtonEvent{ID: "keyboard", Via: inputplumber.Via}
	for _, c := range []struct {
		desc    string
		ev      driver.ButtonEvent
		carried string
		want    bool
	}{
		{"direct, InputPlumber carries nothing", direct, "", true},
		{"direct, InputPlumber carries it", direct, "keyboard", false},
		{"via InputPlumber, carried", viaIP, "keyboard", true},
		{"via InputPlumber, not carried (a leftover mapping)", viaIP, "", false},
		{"via InputPlumber, another button carried", viaIP, "home", false},
	} {
		if got := pressRoute(c.ev, c.carried); got != c.want {
			t.Errorf("%s: pressRoute = %v, want %v", c.desc, got, c.want)
		}
	}
}
