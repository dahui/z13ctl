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

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/driver"
	"github.com/dahui/z13ctl/internal/hid"
)

func TestUnopenedDriverReportsSkippableNoDevice(t *testing.T) {
	l := New([]string{"keyboard", "lightbar"})

	ops := map[string]func() error{
		"Apply":         func() error { return l.Apply("keyboard", api.LightingState{Enabled: true}) },
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
	l := New([]string{"keyboard", "lightbar"})
	z := l.Zones()
	z[0] = "mutated"
	if got := l.Zones(); got[0] != "keyboard" {
		t.Errorf("Zones() shares its backing array with callers: %v", got)
	}
}

func TestCloseOnUnopenedDriverIsANoOp(t *testing.T) {
	if err := New(nil).Close(); err != nil {
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
	hidFind = func(string) (*hid.Device, error) { return hid.FindDevice(tmp) }

	l := New([]string{"keyboard", "lightbar"})
	t.Cleanup(func() { _ = l.Close() })

	hidHas = func(string) bool { return true } // both zones present in sysfs
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
	hidHas = func(string) bool { return false }
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
	hidFind = func(string) (*hid.Device, error) { return nil, boom }

	l := New([]string{"keyboard"})
	if err := l.Reopen(); !errors.Is(err, boom) {
		t.Errorf("Reopen() = %v, want the discovery error", err)
	}
}
