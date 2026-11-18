package audio

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseWpctl(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Volume
		ok   bool
	}{
		{"Volume: 0.40\n", Volume{Percent: 40}, true},
		{"Volume: 0.40 [MUTED]\n", Volume{Percent: 40, Muted: true}, true},
		{"Volume: 1.30\n", Volume{Percent: 100}, true}, // amplified: the slider tops out at 100
		{"Volume: 0.005\n", Volume{Percent: 1}, true},
		{"nope", Volume{}, false},
	} {
		got, err := parseWpctl(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("parseWpctl(%q) = %+v, %v", tc.in, got, err)
		}
	}
}

func TestParsePactl(t *testing.T) {
	v, err := parsePactlVolume("Volume: front-left: 26214 /  40% / -23.87 dB,   front-right: 26214 /  40% / -23.87 dB\n        balance 0.00\n")
	if err != nil || v != 40 {
		t.Errorf("parsePactlVolume = %d, %v", v, err)
	}
	if _, err := parsePactlVolume("garbage"); err == nil {
		t.Error("parsed a volume out of garbage")
	}
	if !parsePactlMute("Mute: yes\n") || parsePactlMute("Mute: no\n") {
		t.Error("parsePactlMute misread")
	}
}

func withTools(t *testing.T, have ...string) *[]string {
	t.Helper()
	var calls []string
	oldPath, oldRun := lookPath, runner
	lookPath = func(name string) (string, error) {
		for _, h := range have {
			if h == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch {
		case name == "wpctl" && args[0] == "get-volume":
			return []byte("Volume: 0.40 [MUTED]\n"), nil
		case name == "pactl" && args[0] == "get-sink-volume":
			return []byte("Volume: front-left: 26214 /  40% / -23.87 dB\n"), nil
		case name == "pactl" && args[0] == "get-sink-mute":
			return []byte("Mute: no\n"), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { lookPath, runner = oldPath, oldRun })
	return &calls
}

// wpctl wins where both exist; setting unmutes.
func TestWpctlBackend(t *testing.T) {
	calls := withTools(t, "wpctl", "pactl")
	if v, err := Read(); err != nil || v != (Volume{Percent: 40, Muted: true}) {
		t.Fatalf("Read = %+v, %v", v, err)
	}
	if err := Set(55); err != nil {
		t.Fatal(err)
	}
	got := strings.Join((*calls)[1:], "; ")
	if got != "wpctl set-volume @DEFAULT_AUDIO_SINK@ 0.55; wpctl set-mute @DEFAULT_AUDIO_SINK@ 0" {
		t.Errorf("Set ran %q", got)
	}
}

func TestPactlFallback(t *testing.T) {
	calls := withTools(t, "pactl")
	if v, err := Read(); err != nil || v != (Volume{Percent: 40}) {
		t.Fatalf("Read = %+v, %v", v, err)
	}
	if err := Set(120); err != nil { // clamped
		t.Fatal(err)
	}
	got := strings.Join((*calls)[2:], "; ")
	if got != "pactl set-sink-volume @DEFAULT_SINK@ 100%; pactl set-sink-mute @DEFAULT_SINK@ 0" {
		t.Errorf("Set ran %q", got)
	}
}

func TestNoBackend(t *testing.T) {
	withTools(t)
	if Available() {
		t.Error("available with neither tool")
	}
	if _, err := Read(); !errors.Is(err, ErrNoBackend) {
		t.Errorf("Read err = %v", err)
	}
	if err := Set(10); !errors.Is(err, ErrNoBackend) {
		t.Errorf("Set err = %v", err)
	}
}
