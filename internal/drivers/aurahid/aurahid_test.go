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
	"testing"

	"github.com/dahui/z13ctl/api"
	"github.com/dahui/z13ctl/internal/driver"
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
