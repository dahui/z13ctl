package daemon

// drawerbutton_test.go — which hardware button opens voltaire: the user's
// stored choice, resolved against the device's declared buttons, and the
// button-set command that stores it. No hardware: the device carries only the
// button list.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/device"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// twoButtonDaemon is a device with two buttons, the second the default, as a
// handheld with a Home button and a vendor button might declare them.
func twoButtonDaemon(t *testing.T) *Daemon {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return &Daemon{hw: &device.Device{
		ButtonList:    []device.ButtonInfo{{ID: "home", Label: "Home button"}, {ID: "oxp", Label: "OneXPlayer button"}},
		DefaultButton: "oxp",
	}}
}

func TestResolveDrawerButton(t *testing.T) {
	d := twoButtonDaemon(t)
	for _, c := range []struct{ choice, want string }{
		{"", "oxp"},                      // no choice: the device default
		{"home", "home"},                 // a declared button
		{api.DrawerButtonNone, ""},       // the user turned buttons off
		{"armoury-crate", "oxp"},         // a choice this device file does not declare
		{api.DrawerButtonDefault, "oxp"}, // never stored, but resolves sanely if it were
	} {
		if got := d.resolveDrawerButton(c.choice); got != c.want {
			t.Errorf("resolveDrawerButton(%q) = %q, want %q", c.choice, got, c.want)
		}
	}
}

func TestButtonSetStoresTheChoice(t *testing.T) {
	d := twoButtonDaemon(t)
	for _, c := range []struct{ set, stored, resolved string }{
		{"home", "home", "home"},
		{api.DrawerButtonNone, api.DrawerButtonNone, ""},
		{api.DrawerButtonDefault, "", "oxp"},
	} {
		if resp := d.handleButtonSet(request{Set: c.set}); !resp.OK {
			t.Fatalf("button-set %q refused: %s", c.set, resp.Error)
		}
		if d.state.DrawerButton != c.stored {
			t.Errorf("after button-set %q: stored %q, want %q", c.set, d.state.DrawerButton, c.stored)
		}
		if got := d.drawerButton(); got != c.resolved {
			t.Errorf("after button-set %q: drawerButton = %q, want %q", c.set, got, c.resolved)
		}
	}
}

func TestButtonSetRefusesAnUndeclaredButton(t *testing.T) {
	d := twoButtonDaemon(t)
	d.state.DrawerButton = "home"
	resp := d.handleButtonSet(request{Set: "armoury-crate"})
	if resp.OK {
		t.Fatal("button-set accepted a button the device does not declare")
	}
	for _, want := range []string{"home", "oxp", "none", "default"} {
		if !strings.Contains(resp.Error, want) {
			t.Errorf("refusal %q does not list %q", resp.Error, want)
		}
	}
	if d.state.DrawerButton != "home" {
		t.Errorf("a refused button-set changed the choice to %q", d.state.DrawerButton)
	}
}

func TestButtonSetWithNoButtons(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d := &Daemon{hw: &device.Device{}}
	if resp := d.handleButtonSet(request{Set: api.DrawerButtonNone}); resp.OK {
		t.Error("button-set accepted on a device with no buttons")
	}
}

// noButtons is a watcher that never reports a press.
type noButtons struct{}

func (noButtons) Watch(ctx context.Context, _ chan<- driver.ButtonEvent) error {
	<-ctx.Done()
	return nil
}

// The wire names are the whole contract for a non-Go client, so they are
// checked on the JSON device-get and get-state actually produce.
func TestDrawerButtonWireKeys(t *testing.T) {
	d := twoButtonDaemon(t)
	d.hw.Buttons = noButtons{}
	d.hw.ButtonLabel = "OneXPlayer button"
	doc, err := json.Marshal(deviceInfoFor(d.hw))
	if err != nil {
		t.Fatal(err)
	}
	want := `"hardware_buttons":[{"id":"home","label":"Home button"},{"id":"oxp","label":"OneXPlayer button","default":true}]`
	if !strings.Contains(string(doc), want) {
		t.Errorf("device-get JSON %s lacks %s", doc, want)
	}
	state, err := json.Marshal(api.State{DrawerButton: "home"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state), `"drawer_button":"home"`) {
		t.Errorf("get-state JSON %s lacks drawer_button", state)
	}
}

// pressEach reports one press from each id, then waits for cancellation.
type pressEach []string

func (p pressEach) Watch(ctx context.Context, ch chan<- driver.ButtonEvent) error {
	for _, id := range p {
		ch <- driver.ButtonEvent{ID: id}
	}
	<-ctx.Done()
	return nil
}

// Only the chosen button's presses reach the press timing; the others are
// ignored, so a device that declares several buttons opens voltaire on one.
func TestOnlyTheChosenButtonOpensVoltaire(t *testing.T) {
	for _, c := range []struct {
		choice string
		want   int
	}{
		{"", 1},                   // the default, oxp
		{"home", 1},               // the other button
		{api.DrawerButtonNone, 0}, // none
	} {
		d := twoButtonDaemon(t)
		d.state.DrawerButton = c.choice
		d.hw.Buttons = pressEach{"home", "oxp", "unknown"}
		d.buttonCh = make(chan time.Time, 8)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { d.watchButtons(ctx); close(done) }()
		time.Sleep(50 * time.Millisecond)
		cancel()
		<-done
		if got := len(d.buttonCh); got != c.want {
			t.Errorf("choice %q: %d presses reached the drawer, want %d", c.choice, got, c.want)
		}
	}
}
