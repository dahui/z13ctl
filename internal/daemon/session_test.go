// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dahui/voltaire/v2/internal/device"
)

func withSession(t *testing.T, gaming bool) {
	t.Helper()
	origNow, origPoll := gamingModeNow, sessionPollInterval
	gamingModeNow = func() bool { return gaming }
	sessionPollInterval = time.Millisecond
	t.Cleanup(func() { gamingModeNow, sessionPollInterval = origNow, origPoll })
}

// TestWatchSessionRestartsOnAChange: a device that yields, assembled for the
// desktop, restarts once Gaming Mode starts, and the reverse.
func TestWatchSessionRestartsOnAChange(t *testing.T) {
	for _, assembledGaming := range []bool{false, true} {
		withSession(t, !assembledGaming)
		d := &Daemon{hw: &device.Device{YieldsInGamingMode: true, GamingMode: assembledGaming}}
		restart := make(chan error, 1)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		d.watchSession(ctx, restart)
		cancel()
		select {
		case err := <-restart:
			if !errors.Is(err, errSessionChanged) {
				t.Errorf("assembled gaming=%v: got %v", assembledGaming, err)
			}
		default:
			t.Errorf("assembled gaming=%v: no restart when the session changed", assembledGaming)
		}
	}
}

// TestWatchSessionStaysPut: no change, or a device that yields nothing, never
// restarts the daemon.
func TestWatchSessionStaysPut(t *testing.T) {
	for _, hw := range []*device.Device{
		{YieldsInGamingMode: true, GamingMode: true},   // still in Gaming Mode
		{YieldsInGamingMode: false, GamingMode: false}, // the Z13: nothing yields
	} {
		withSession(t, true)
		d := &Daemon{hw: hw}
		restart := make(chan error, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		d.watchSession(ctx, restart)
		cancel()
		select {
		case err := <-restart:
			t.Errorf("%+v: restarted (%v)", hw, err)
		default:
		}
	}
}
