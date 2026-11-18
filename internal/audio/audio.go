// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package audio reads and sets the default output's volume for the GUI's
// volume slider.
//
// Like the screen's brightness and refresh rate it belongs to the session,
// not to hardware the daemon owns: the sound server runs per user, and a GTK
// client is in the session by definition. Unlike those, there is one owner on
// every desktop: PipeWire (wpctl) on any current distribution, gamescope's
// session included, with pactl for a system still on PulseAudio (or
// pipewire-pulse without wpctl). So this works the same under KDE, GNOME,
// Hyprland and Gaming Mode, and Steam's own slider reads the same sink.
//
// The parsing is pure and table-tested; Read and Set go through an exec seam.
package audio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Volume is the default output's level.
type Volume struct {
	// Percent is 0–100. A sink amplified past 100 % (wpctl allows 150) reads
	// 100: the slider does not offer amplification, and showing 100 is true of
	// what the slider can do.
	Percent int
	Muted   bool
}

const commandTimeout = 3 * time.Second

var (
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
	lookPath = exec.LookPath
)

const (
	wpctl = "wpctl"
	pactl = "pactl"
)

// ErrNoBackend means neither wpctl nor pactl is installed.
var ErrNoBackend = errors.New("no supported sound server tool (wpctl or pactl)")

// backend is the tool to drive, wpctl first.
func backend() string {
	if _, err := lookPath(wpctl); err == nil {
		return wpctl
	}
	if _, err := lookPath(pactl); err == nil {
		return pactl
	}
	return ""
}

// Available reports whether the volume can be read and set: a PATH lookup.
func Available() bool { return backend() != "" }

func run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := runner(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

// Read returns the default output's volume.
func Read() (Volume, error) {
	switch backend() {
	case wpctl:
		out, err := run(wpctl, "get-volume", "@DEFAULT_AUDIO_SINK@")
		if err != nil {
			return Volume{}, err
		}
		return parseWpctl(string(out))
	case pactl:
		out, err := run(pactl, "get-sink-volume", "@DEFAULT_SINK@")
		if err != nil {
			return Volume{}, err
		}
		pct, err := parsePactlVolume(string(out))
		if err != nil {
			return Volume{}, err
		}
		mute, err := run(pactl, "get-sink-mute", "@DEFAULT_SINK@")
		if err != nil {
			return Volume{}, err
		}
		return Volume{Percent: pct, Muted: parsePactlMute(string(mute))}, nil
	}
	return Volume{}, ErrNoBackend
}

// Set sets the default output's volume and unmutes it: a user moving a volume
// slider wants to hear the result, and a muted sink would leave the slider
// moving with nothing changing.
func Set(pct int) error {
	pct = min(max(pct, 0), 100)
	switch backend() {
	case wpctl:
		if _, err := run(wpctl, "set-volume", "@DEFAULT_AUDIO_SINK@", strconv.FormatFloat(float64(pct)/100, 'f', 2, 64)); err != nil {
			return err
		}
		_, err := run(wpctl, "set-mute", "@DEFAULT_AUDIO_SINK@", "0")
		return err
	case pactl:
		if _, err := run(pactl, "set-sink-volume", "@DEFAULT_SINK@", strconv.Itoa(pct)+"%"); err != nil {
			return err
		}
		_, err := run(pactl, "set-sink-mute", "@DEFAULT_SINK@", "0")
		return err
	}
	return ErrNoBackend
}

// parseWpctl reads "Volume: 0.40" or "Volume: 0.40 [MUTED]".
func parseWpctl(s string) (Volume, error) {
	f := strings.Fields(s)
	if len(f) < 2 || f[0] != "Volume:" {
		return Volume{}, fmt.Errorf("wpctl: unexpected reply %q", strings.TrimSpace(s))
	}
	v, err := strconv.ParseFloat(f[1], 64)
	if err != nil {
		return Volume{}, fmt.Errorf("wpctl: unexpected volume %q", f[1])
	}
	return Volume{Percent: toPercent(v * 100), Muted: strings.Contains(s, "[MUTED]")}, nil
}

var pactlPercent = regexp.MustCompile(`(\d+)%`)

// parsePactlVolume reads the first channel's percentage from
// "Volume: front-left: 26214 /  40% / -23.87 dB,   front-right: …".
func parsePactlVolume(s string) (int, error) {
	m := pactlPercent.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("pactl: no volume in %q", strings.TrimSpace(s))
	}
	v, _ := strconv.Atoi(m[1])
	return toPercent(float64(v)), nil
}

// parsePactlMute reads "Mute: yes" / "Mute: no".
func parsePactlMute(s string) bool {
	return strings.Contains(strings.ToLower(s), "mute: yes")
}

func toPercent(v float64) int { return min(max(int(math.Round(v)), 0), 100) }
