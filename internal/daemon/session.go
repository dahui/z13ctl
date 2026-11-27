// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

// session.go — re-assembly when Gaming Mode starts or ends.
//
// A device whose data yields capabilities to Steam in Gaming Mode
// (device.YieldToSteam: the Xbox Ally X's power and charge limits) is
// assembled for the session kind running when the daemon starts. The
// capability set is fixed after that (device-get is static), so when the
// kind changes the daemon stops with errSessionChanged and systemd's
// Restart=on-failure starts it again, assembled for the new session. On a
// device that yields nothing the watcher never starts.

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/v2/internal/device"
)

// sessionPollInterval is how often the session kind is checked: one logind
// property read, and a few seconds of either kind is harmless because the
// switch itself takes longer than that.
var sessionPollInterval = 10 * time.Second

// errSessionChanged ends Run when the session kind no longer matches the one
// the device was assembled for.
var errSessionChanged = errors.New("session changed between Gaming Mode and the desktop; restarting to re-assemble")

// gamingModeNow is the session check, a var so tests answer without logind.
var gamingModeNow = func() bool { return device.GamingMode() }

// watchSession sends errSessionChanged on restart once the session kind
// differs from d.hw.GamingMode, and returns.
func (d *Daemon) watchSession(ctx context.Context, restart chan<- error) {
	if d.hw == nil || !d.hw.YieldsInGamingMode {
		return
	}
	t := time.NewTicker(sessionPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if now := gamingModeNow(); now != d.hw.GamingMode {
				slog.Info("session kind changed; restarting so Steam's and voltaire's controls follow it",
					"gaming_mode", now)
				restart <- errSessionChanged
				return
			}
		}
	}
}
