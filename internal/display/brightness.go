// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package display

// brightness.go — the screen's brightness, from whichever owner the session
// has.
//
// Two backends, chosen per session:
//
//   - KWin on KDE (kscreen-doctor's output brightness). KWin owns the
//     backlight there: measured on Plasma 6.8, a write behind it (logind,
//     below) moved the backlight to 50 % while KWin went on reporting 75 %, so
//     Plasma's slider and OSD went stale and the next brightness key jumped
//     back. Going through KWin keeps them one value.
//   - logind everywhere else, gamescope included: Session.SetBrightness on the
//     user's graphical session, which any session user may call with no
//     grant, reading /sys/class/backlight. GNOME's settings daemon follows the
//     backlight itself; Hyprland and gamescope have no owner to fight. (Whether
//     Steam's own slider follows under gamescope is a hardware check.)
//
// Percent on the wire, 1–100: 0 would turn some panels' backlights fully off,
// which on a handheld leaves no way to see the control that undoes it.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// BrightnessMin is the lowest percent the control offers.
const BrightnessMin = 1

// sysBacklightDir is the backlight class; a var so tests point it at a fake.
var sysBacklightDir = "/sys/class/backlight"

// setBacklight is the logind seam: SetBrightness on the user's graphical
// session. Tests replace it.
var setBacklight = logindSetBrightness

type brightnessBackend int

const (
	brightnessNone brightnessBackend = iota
	brightnessKWin
	brightnessLogind
)

// kdeSession reports a Plasma session. XDG_CURRENT_DESKTOP is a colon list.
func kdeSession() bool {
	for _, d := range strings.Split(getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(d, "KDE") {
			return true
		}
	}
	return getenv("KDE_FULL_SESSION") == "true"
}

func pickBrightnessBackend() brightnessBackend {
	if !gamescopeSession() && kdeSession() {
		if _, err := lookPath(kscreenDoctor); err == nil {
			return brightnessKWin
		}
	}
	if _, ok := backlightDevice(); ok {
		return brightnessLogind
	}
	return brightnessNone
}

// BrightnessAvailable reports whether this session can read and set the
// screen's brightness. An environment, PATH and directory check only: it runs
// while the widget tree is built.
func BrightnessAvailable() bool { return pickBrightnessBackend() != brightnessNone }

// backlight is one /sys/class/backlight device.
type backlight struct {
	name string
	kind string // firmware, platform or raw
}

// backlightRank orders the kinds as systemd-backlight and the desktops do: a
// firmware interface first, then a platform driver, then the raw GPU one.
func backlightRank(kind string) int {
	switch kind {
	case "firmware":
		return 0
	case "platform":
		return 1
	case "raw":
		return 2
	}
	return 3
}

// backlightDevice picks the backlight to drive, by kind and then by name.
func backlightDevice() (backlight, bool) {
	entries, err := os.ReadDir(sysBacklightDir)
	if err != nil {
		return backlight{}, false
	}
	var all []backlight
	for _, e := range entries {
		dir := sysBacklightDir + "/" + e.Name()
		if _, err := os.Stat(dir + "/max_brightness"); err != nil {
			continue
		}
		kind, _ := os.ReadFile(dir + "/type")
		all = append(all, backlight{name: e.Name(), kind: strings.TrimSpace(string(kind))})
	}
	if len(all) == 0 {
		return backlight{}, false
	}
	sort.Slice(all, func(i, j int) bool {
		ri, rj := backlightRank(all[i].kind), backlightRank(all[j].kind)
		if ri != rj {
			return ri < rj
		}
		return all[i].name < all[j].name
	})
	return all[0], true
}

func readSysInt(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// rawToPercent and percentToRaw convert a backlight level, rounding, and
// never asking for a raw level of 0.
func rawToPercent(raw, maxRaw int) int {
	if maxRaw <= 0 {
		return 0
	}
	return int(math.Round(float64(raw) * 100 / float64(maxRaw)))
}

func percentToRaw(pct, maxRaw int) uint32 {
	raw := int(math.Round(float64(clampPercent(pct)) * float64(maxRaw) / 100))
	return uint32(max(raw, 1))
}

func clampPercent(pct int) int { return min(max(pct, BrightnessMin), 100) }

// ErrNoBrightness means the screen in front of the user reports no
// brightness control (an external monitor without one, say).
var ErrNoBrightness = errors.New("this screen's brightness cannot be set from here")

// ReadBrightness returns the screen's brightness in percent.
func ReadBrightness() (int, error) {
	switch pickBrightnessBackend() {
	case brightnessKWin:
		outs, err := Query()
		if err != nil {
			return 0, err
		}
		o, ok := Primary(outs)
		if !ok || o.Brightness == nil {
			return 0, ErrNoBrightness
		}
		return int(math.Round(*o.Brightness * 100)), nil
	case brightnessLogind:
		b, _ := backlightDevice()
		dir := sysBacklightDir + "/" + b.name
		raw, err := readSysInt(dir + "/brightness")
		if err != nil {
			return 0, err
		}
		maxRaw, err := readSysInt(dir + "/max_brightness")
		if err != nil {
			return 0, err
		}
		return rawToPercent(raw, maxRaw), nil
	}
	return 0, ErrNoBackend
}

// SetBrightness sets the screen's brightness in percent, clamped to
// BrightnessMin–100.
func SetBrightness(pct int) error {
	pct = clampPercent(pct)
	switch pickBrightnessBackend() {
	case brightnessKWin:
		outs, err := Query()
		if err != nil {
			return err
		}
		o, ok := Primary(outs)
		if !ok || o.Brightness == nil {
			return ErrNoBrightness
		}
		return runKScreen(fmt.Sprintf("output.%s.brightness.%d", o.Name, pct))
	case brightnessLogind:
		b, _ := backlightDevice()
		maxRaw, err := readSysInt(sysBacklightDir + "/" + b.name + "/max_brightness")
		if err != nil {
			return err
		}
		return setBacklight(b.name, percentToRaw(pct, maxRaw))
	}
	return ErrNoBackend
}

// logindSetBrightness asks logind to set the backlight for the user's
// graphical session. The session is looked up rather than taken as "auto": a
// systemd user service (voltaire-gui.service) belongs to no session, and
// logind refuses a session with no seat.
func logindSetBrightness(name string, raw uint32) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("logind: %w", err)
	}
	defer func() { _ = conn.Close() }()

	const dest = "org.freedesktop.login1"
	var userPath dbus.ObjectPath
	if uErr := conn.Object(dest, "/org/freedesktop/login1").
		Call(dest+".Manager.GetUser", 0, uint32(os.Getuid())).Store(&userPath); uErr != nil {
		return fmt.Errorf("logind: finding the user: %w", uErr)
	}
	v, err := conn.Object(dest, userPath).GetProperty(dest + ".User.Display")
	if err != nil {
		return fmt.Errorf("logind: finding the graphical session: %w", err)
	}
	session, ok := displaySession(v.Value())
	if !ok {
		return errors.New("logind: no graphical session to set the brightness for")
	}
	if sErr := conn.Object(dest, session).
		Call(dest+".Session.SetBrightness", 0, "backlight", name, raw).Err; sErr != nil {
		return fmt.Errorf("logind: setting the brightness: %w", sErr)
	}
	return nil
}

// displaySession reads logind's User.Display property, a (session id, object
// path) struct; "/" means the user has no graphical session.
func displaySession(v any) (dbus.ObjectPath, bool) {
	parts, ok := v.([]any)
	if !ok || len(parts) != 2 {
		return "", false
	}
	p, ok := parts[1].(dbus.ObjectPath)
	if !ok || p == "" || p == "/" {
		return "", false
	}
	return p, true
}
