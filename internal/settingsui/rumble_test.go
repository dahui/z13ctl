package settingsui

import (
	"testing"

	"github.com/dahui/voltaire/api/v2"
)

func TestRumbleRange(t *testing.T) {
	if _, _, ok := RumbleRange(nil); ok {
		t.Error("a nil document built the card")
	}
	if _, _, ok := RumbleRange(&api.DeviceInfo{}); ok {
		t.Error("a device without rumble built the card")
	}
	if _, _, ok := RumbleRange(&api.DeviceInfo{Rumble: &api.RumbleInfo{Min: 3, Max: 3}}); ok {
		t.Error("a one-step scale built a slider")
	}
	lo, hi, ok := RumbleRange(&api.DeviceInfo{Rumble: &api.RumbleInfo{Min: 0, Max: 5}})
	if !ok || lo != 0 || hi != 5 {
		t.Errorf("RumbleRange = %d, %d, %v; want 0, 5, true", lo, hi, ok)
	}
}

func TestRumbleText(t *testing.T) {
	for v, want := range map[int]string{0: "Off", 1: "1 of 5", 5: "5 of 5"} {
		if got := RumbleText(v, 5); got != want {
			t.Errorf("RumbleText(%d) = %q, want %q", v, got, want)
		}
	}
}

func TestRumbleValue(t *testing.T) {
	if _, ok := RumbleValue(&api.State{}); ok {
		t.Error("an unread strength is known")
	}
	three := 3
	if v, ok := RumbleValue(&api.State{Rumble: &three}); !ok || v != 3 {
		t.Errorf("RumbleValue = %d, %v", v, ok)
	}
}
