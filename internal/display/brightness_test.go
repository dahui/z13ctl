package display

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// fakeBacklight points the backlight class at a temp tree holding the given
// devices (name → type), each at 236000 of 472000, the OXP's scale.
func fakeBacklight(t *testing.T, devs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	old := sysBacklightDir
	sysBacklightDir = root
	t.Cleanup(func() { sysBacklightDir = old })
	for name, kind := range devs {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, v := range map[string]string{"type": kind, "brightness": "236000", "max_brightness": "472000"} {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// recordBacklight replaces the logind call and records what it was asked.
func recordBacklight(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	old := setBacklight
	setBacklight = func(name string, raw uint32) error {
		calls = append(calls, name+"="+itoa(int(raw)))
		return nil
	}
	t.Cleanup(func() { setBacklight = old })
	return &calls
}

func itoa(v int) string { return strconv.Itoa(v) }

func env(vals map[string]string) func(string) string {
	return func(k string) string { return vals[k] }
}

// On KDE the brightness goes through KWin, never the backlight behind it; off
// KDE (GNOME, Hyprland) and under gamescope it goes through logind.
func TestBrightnessBackendFollowsTheSession(t *testing.T) {
	fakeBacklight(t, map[string]string{"amdgpu_bl1": "raw"})
	withStubs(t, foundOnPath, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	cases := []struct {
		name string
		env  map[string]string
		want brightnessBackend
	}{
		{"plasma", map[string]string{"XDG_CURRENT_DESKTOP": "KDE"}, brightnessKWin},
		{"plasma via KDE_FULL_SESSION", map[string]string{"KDE_FULL_SESSION": "true"}, brightnessKWin},
		{"gnome", map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"}, brightnessLogind},
		{"hyprland", map[string]string{"XDG_CURRENT_DESKTOP": "Hyprland"}, brightnessLogind},
		{"gamescope on a KDE install", map[string]string{"XDG_CURRENT_DESKTOP": "KDE", "GAMESCOPE_WAYLAND_DISPLAY": "gamescope-0"}, brightnessLogind},
	}
	for _, tc := range cases {
		getenv = env(tc.env)
		if got := pickBrightnessBackend(); got != tc.want {
			t.Errorf("%s: backend %d, want %d", tc.name, got, tc.want)
		}
	}
	// No backlight and not KDE: nothing to offer.
	fakeBacklight(t, nil)
	getenv = env(map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"})
	if BrightnessAvailable() {
		t.Error("available with no backlight off KDE")
	}
}

// logind: read from sysfs as a percent, set through logind (never a sysfs
// write), clamped so the backlight is never asked for 0.
func TestLogindBrightness(t *testing.T) {
	fakeBacklight(t, map[string]string{"amdgpu_bl1": "raw"})
	withStubs(t, func(string) (string, error) { return "", errors.New("no") },
		func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("ran kscreen-doctor off KDE")
			return nil, nil
		})
	calls := recordBacklight(t)
	if pct, err := ReadBrightness(); err != nil || pct != 50 {
		t.Fatalf("ReadBrightness = %d, %v; want 50", pct, err)
	}
	for _, pct := range []int{75, 0, 150} {
		if err := SetBrightness(pct); err != nil {
			t.Fatalf("SetBrightness(%d): %v", pct, err)
		}
	}
	want := "amdgpu_bl1=354000 amdgpu_bl1=4720 amdgpu_bl1=472000"
	if got := strings.Join(*calls, " "); got != want {
		t.Errorf("logind calls %q, want %q", got, want)
	}
}

// A firmware backlight wins over the GPU's raw one, as systemd and the
// desktops choose.
func TestBacklightPreference(t *testing.T) {
	fakeBacklight(t, map[string]string{"amdgpu_bl1": "raw", "acpi_video0": "firmware", "vendor": "platform"})
	if b, ok := backlightDevice(); !ok || b.name != "acpi_video0" {
		t.Errorf("picked %+v, want acpi_video0", b)
	}
}

// KDE: read and set through KWin's output brightness, on the primary output.
func TestKWinBrightness(t *testing.T) {
	fakeBacklight(t, map[string]string{"amdgpu_bl1": "raw"})
	var args []string
	withStubs(t, foundOnPath, func(_ context.Context, _ string, a ...string) ([]byte, error) {
		args = append(args, strings.Join(a, " "))
		if len(a) == 1 && a[0] == "-j" {
			return []byte(`{"outputs":[{"name":"eDP-1","enabled":true,"connected":true,"priority":1,"brightness":0.75,"currentModeId":"1","modes":[]}]}`), nil
		}
		return nil, nil
	})
	getenv = env(map[string]string{"XDG_CURRENT_DESKTOP": "KDE"})
	calls := recordBacklight(t)
	if pct, err := ReadBrightness(); err != nil || pct != 75 {
		t.Fatalf("ReadBrightness = %d, %v; want 75", pct, err)
	}
	if err := SetBrightness(40); err != nil {
		t.Fatalf("SetBrightness: %v", err)
	}
	if args[len(args)-1] != "output.eDP-1.brightness.40" {
		t.Errorf("last kscreen-doctor call %q", args[len(args)-1])
	}
	if len(*calls) != 0 {
		t.Errorf("wrote the backlight behind KWin: %v", *calls)
	}
}

// An output KWin offers no brightness for says so rather than guessing.
func TestKWinOutputWithoutBrightness(t *testing.T) {
	withStubs(t, foundOnPath, func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"outputs":[{"name":"DP-1","enabled":true,"connected":true,"priority":1,"currentModeId":"1","modes":[]}]}`), nil
	})
	getenv = env(map[string]string{"XDG_CURRENT_DESKTOP": "KDE"})
	if _, err := ReadBrightness(); !errors.Is(err, ErrNoBrightness) {
		t.Errorf("ReadBrightness err = %v, want ErrNoBrightness", err)
	}
}

func TestDisplaySession(t *testing.T) {
	if p, ok := displaySession([]any{"4", dbus.ObjectPath("/org/freedesktop/login1/session/_34")}); !ok || p != "/org/freedesktop/login1/session/_34" {
		t.Errorf("displaySession = %q, %v", p, ok)
	}
	for _, v := range []any{[]any{"", dbus.ObjectPath("/")}, "x", []any{"4"}} {
		if _, ok := displaySession(v); ok {
			t.Errorf("displaySession(%v) found a session", v)
		}
	}
}
