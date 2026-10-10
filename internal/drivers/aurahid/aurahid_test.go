package aurahid

// aurahid_test.go — the unopened-driver contract. The HID and Aura layers
// have their own tests; what this package adds — and what these tests pin —
// is the skippable-error convention: every method on a driver with no open
// device reports driver.ErrUnsupported with the exact "no HID device
// available" text the socket protocol has always used, which is what lets the
// daemon's lighting restore skip quietly instead of failing, and its handlers
// answer clients with the message they have always received.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
	"github.com/dahui/voltaire/v2/internal/hid"
)

// z13 is the Z13 device file's zone table.
var z13 = Config{Zones: []Zone{
	{Name: "keyboard", Label: "Keyboard", Vendor: 0x0b05, Product: 0x1a30, Byte: 0},
	{Name: "lightbar", Label: "Lightbar", Vendor: 0x0b05, Product: 0x18c6, Byte: 1},
}}

func newZ13(t *testing.T) *Lighting {
	t.Helper()
	l, err := New(z13)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestUnopenedDriverReportsSkippableNoDevice(t *testing.T) {
	l := newZ13(t)

	ops := map[string]func() error{
		"Apply": func() error {
			return l.Apply("keyboard", api.LightingState{Enabled: true, Mode: "static", Speed: "normal", Brightness: 3})
		},
		"Off":           func() error { return l.Off("") },
		"SetBrightness": func() error { return l.SetBrightness("", 2) },
	}
	for name, op := range ops {
		err := op()
		if err == nil {
			t.Fatalf("%s on an unopened driver = nil, want an error", name)
		}
		if !errors.Is(err, driver.ErrUnsupported) {
			t.Errorf("%s error does not wrap driver.ErrUnsupported: %v", name, err)
		}
		if got, want := err.Error(), "no HID device available"; got != want {
			t.Errorf("%s error = %q, want %q (the protocol's established message)", name, got, want)
		}
	}
}

func TestZonesReturnsACopy(t *testing.T) {
	l := newZ13(t)
	z := l.Zones()
	z[0] = "mutated"
	if got := l.Zones(); got[0] != "keyboard" {
		t.Errorf("Zones() shares its backing array with callers: %v", got)
	}
}

func TestCloseOnUnopenedDriverIsANoOp(t *testing.T) {
	l, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close() on an unopened driver = %v, want nil", err)
	}
}

// TestReopenGuardsPresentZones is the regression test for the keyboard-reattach
// smoke failure: the keyboard's sysfs entry appears before udev applies
// permissions to the new /dev node, hid.FindDevice silently drops the node it
// cannot open, and Reopen returned the lightbar-only result as success — so the
// hotplug watcher latched, never retried, and the keyboard stayed dark until
// the next physical detach. A zone present in sysfs but missing from the
// opened set must be a Reopen error; a zone absent from sysfs (a detached
// cover) must not be.
func TestReopenGuardsPresentZones(t *testing.T) {
	// A device backed by a plain temp file: one open node with no zone name —
	// exactly the shape FindDevice returns when a zone's node exists in sysfs
	// but could not be opened, since the unopenable node is simply missing.
	tmp := filepath.Join(t.TempDir(), "hidraw")
	if err := os.WriteFile(tmp, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	origFind, origHas := hidFind, hidHas
	t.Cleanup(func() { hidFind, hidHas = origFind, origHas })
	hidFind = func(string, []hid.Known) (*hid.Device, error) { return hid.FindDevice(tmp, nil) }

	l := newZ13(t)
	t.Cleanup(func() { _ = l.Close() })

	hidHas = func(string, []hid.Known) bool { return true } // both zones present in sysfs
	err := l.Reopen()
	if err == nil {
		t.Fatal("Reopen() = nil while a present zone is missing from the opened set")
	}
	if !strings.Contains(err.Error(), "keyboard") {
		t.Errorf("Reopen() error %q does not name the missing zone", err)
	}
	if l.dev != nil {
		t.Error("failed Reopen installed the incomplete device")
	}

	// The same opened set with every zone absent from sysfs is a detached
	// cover, not a failure: there is nothing to verify against.
	hidHas = func(string, []hid.Known) bool { return false }
	if err := l.Reopen(); err != nil {
		t.Fatalf("Reopen() with all zones detached = %v, want nil", err)
	}
	if l.dev == nil {
		t.Error("successful Reopen did not install the device")
	}
}

func TestReopenPropagatesDiscoveryError(t *testing.T) {
	origFind := hidFind
	t.Cleanup(func() { hidFind = origFind })
	boom := errors.New("no ASUS Aura devices found")
	hidFind = func(string, []hid.Known) (*hid.Device, error) { return nil, boom }

	l := newZ13(t)
	if err := l.Reopen(); !errors.Is(err, boom) {
		t.Errorf("Reopen() = %v, want the discovery error", err)
	}
}

// The capabilities are the device data's zones and the protocol's modes,
// speeds and levels — and a mode the data names that the protocol cannot send
// is refused at construction rather than failing at every apply.
func TestCaps(t *testing.T) {
	c := newZ13(t).Caps()
	if len(c.Zones) != 2 || c.Zones[1] != (driver.LightingZone{Name: "lightbar", Label: "Lightbar"}) {
		t.Errorf("zones = %+v", c.Zones)
	}
	if b, ok := c.Mode("breathe"); !ok || !b.Color || !b.Color2 || !b.Speed || b.Label != "Breathe" {
		t.Errorf("breathe = %+v, %v; want both colours and a speed", b, ok)
	}
	if st, _ := c.Mode("static"); st.Speed || st.Color2 || !st.Color {
		t.Errorf("static = %+v; want one colour and no speed", st)
	}
	if cy, _ := c.Mode("cycle"); cy.Color || !cy.Speed {
		t.Errorf("cycle = %+v; want no colour and a speed", cy)
	}
	if c.BrightnessMax != 3 || len(c.Speeds) != 3 || len(c.Modes) != 5 {
		t.Errorf("caps = %+v", c)
	}

	sub, err := New(Config{Zones: z13.Zones, Modes: []string{"static", "rainbow"}, Speeds: []string{"normal"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := sub.Caps(); len(got.Modes) != 2 || got.Modes[1].Name != "rainbow" || len(got.Speeds) != 1 {
		t.Errorf("subset caps = %+v", got)
	}
	if _, err := New(Config{Modes: []string{"disco"}}); err == nil {
		t.Error("a mode the protocol cannot send was accepted")
	}
	if _, err := New(Config{Speeds: []string{"ludicrous"}}); err == nil {
		t.Error("a speed the protocol cannot send was accepted")
	}
}

// PresentZones reports each zone on its own, so a zone that never appears
// cannot hide the keyboard returning.
func TestPresentZones(t *testing.T) {
	origHas := hidHas
	t.Cleanup(func() { hidHas = origHas })
	hidHas = func(name string, _ []hid.Known) bool { return name == "lightbar" }
	if got := newZ13(t).PresentZones(); len(got) != 1 || got[0] != "lightbar" {
		t.Errorf("PresentZones = %v, want [lightbar]", got)
	}
}
