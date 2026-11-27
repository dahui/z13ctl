// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package session answers one question about the user's graphical session:
// is it Steam's Gaming Mode (a gamescope session)? Some handhelds' SteamOS
// configuration has steamos-manager write the same firmware limits voltaire
// does (the ROG Xbox Ally X's TDP and charge limit), and the device data says
// to leave those to Steam there.
//
// The answer comes from logind, not the environment: the daemon is a systemd
// user service, which carries no session's variables and belongs to no
// session itself. logind's User.Display names the user's graphical session,
// and that session's Desktop property is what its session file declared
// ("gamescope" for gamescope-session, "KDE" for Plasma), checked on the Xbox
// Ally X 2026-11-26.
package session

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

// IsGamingDesktop reports whether a logind session Desktop value is a
// gamescope session's.
func IsGamingDesktop(desktop string) bool {
	return strings.Contains(strings.ToLower(desktop), "gamescope")
}

// GamingMode reports whether the user's graphical session is a gamescope
// session. Any failure to ask (no system bus, no graphical session) is "no":
// yielding a control the user could otherwise use needs evidence.
func GamingMode() bool {
	desktop, err := Desktop()
	return err == nil && IsGamingDesktop(desktop)
}

// Desktop returns the Desktop property of the user's graphical session.
func Desktop() (string, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return "", fmt.Errorf("logind: %w", err)
	}
	defer func() { _ = conn.Close() }()

	const dest = "org.freedesktop.login1"
	var userPath dbus.ObjectPath
	if uErr := conn.Object(dest, "/org/freedesktop/login1").
		Call(dest+".Manager.GetUser", 0, uint32(os.Getuid())).Store(&userPath); uErr != nil {
		return "", fmt.Errorf("logind: finding the user: %w", uErr)
	}
	v, err := conn.Object(dest, userPath).GetProperty(dest + ".User.Display")
	if err != nil {
		return "", fmt.Errorf("logind: finding the graphical session: %w", err)
	}
	path, ok := displaySession(v.Value())
	if !ok {
		return "", errors.New("logind: the user has no graphical session")
	}
	d, err := conn.Object(dest, path).GetProperty(dest + ".Session.Desktop")
	if err != nil {
		return "", fmt.Errorf("logind: reading the session's desktop: %w", err)
	}
	s, _ := d.Value().(string)
	return s, nil
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
