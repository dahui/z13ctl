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
