package inputplumber

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

// The InputEvent sequence captured on the OXP (2026-11-17): a press is
// (action, 1.0) and its release (action, 0.0); InputPlumber also emits actions
// that are not voltaire's (here a test marker on the A button).
func TestPressOf(t *testing.T) {
	sig := func(action any, value any) *dbus.Signal {
		return &dbus.Signal{Name: dbusDeviceIface + ".InputEvent", Body: []any{action, value}}
	}
	for _, c := range []struct {
		desc    string
		sig     *dbus.Signal
		id      string
		pressed bool
	}{
		{"press", sig("voltaire_keyboard", 1.0), "keyboard", true},
		{"release", sig("voltaire_keyboard", 0.0), "keyboard", false},
		{"someone else's action", sig("marker_a", 1.0), "", false},
		{"wrong value type", sig("voltaire_home", "1"), "", false},
		{"wrong member", &dbus.Signal{Name: dbusDeviceIface + ".TouchEvent", Body: []any{"voltaire_home", 1.0}}, "", false},
		{"short body", &dbus.Signal{Name: dbusDeviceIface + ".InputEvent", Body: []any{"voltaire_home"}}, "", false},
		{"nil", nil, "", false},
	} {
		id, pressed := pressOf(c.sig)
		if id != c.id || pressed != c.pressed {
			t.Errorf("%s: pressOf = %q, %v; want %q, %v", c.desc, id, pressed, c.id, c.pressed)
		}
	}
}

func TestGamepadEventOf(t *testing.T) {
	sig := func(name string, value float64) *dbus.Signal {
		return &dbus.Signal{Name: dbusDeviceIface + ".InputEvent", Body: []any{name, value}}
	}
	for _, c := range []struct {
		sig  *dbus.Signal
		want GamepadEvent
		ok   bool
	}{
		{sig("ui_up", 1), GamepadEvent{"up", true}, true},
		{sig("ui_up", 0), GamepadEvent{"up", false}, true},
		{sig("ui_accept", 1), GamepadEvent{"accept", true}, true},
		{sig("ui_back", 1), GamepadEvent{"back", true}, true},
		{sig("ui_l1", 1), GamepadEvent{"l1", true}, true},
		{sig("ui_r1", 0), GamepadEvent{"r1", false}, true},
		// Not navigation: dropped, so the stream is not a record of the controller.
		{sig("ui_guide", 1), GamepadEvent{}, false},
		{sig("ui_action", 1), GamepadEvent{}, false},
		{sig("voltaire_armoury-crate", 1), GamepadEvent{}, false},
		{&dbus.Signal{Name: dbusDeviceIface + ".TouchEvent", Body: []any{"ui_up", 1.0}}, GamepadEvent{}, false},
		{&dbus.Signal{Name: dbusDeviceIface + ".InputEvent", Body: []any{"ui_up"}}, GamepadEvent{}, false},
		{nil, GamepadEvent{}, false},
	} {
		got, ok := gamepadEventOf(c.sig)
		if got != c.want || ok != c.ok {
			t.Errorf("%v: got %+v, %v; want %+v, %v", c.sig, got, ok, c.want, c.ok)
		}
	}
}

func TestHasGamepad(t *testing.T) {
	if !(Composite{Capabilities: []string{"Keyboard:KeyA", "Gamepad:Button:South"}}).HasGamepad() {
		t.Error("a composite with a gamepad button has a gamepad")
	}
	if (Composite{Capabilities: []string{"Keyboard:KeyA", "Mouse:Motion"}}).HasGamepad() {
		t.Error("a keyboard-and-mouse composite has no gamepad: intercepting it would swallow typing")
	}
}
