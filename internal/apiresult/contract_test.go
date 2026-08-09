// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/dahui/z13ctl/api"
)

// TestAPIReportsMissingDaemonWithoutAnError pins the api behaviour this whole
// package exists for: when the socket cannot be dialled, api.Send* reports
// handled=false and a *nil* error. Every call site in internal/gui used to test
// err alone, which turned "the daemon is not running" into a success.
//
// Asserting it here rather than trusting the doc comment means an api release
// that changed the convention would fail the build instead of silently making
// daemon.Err redundant — or, worse, wrong.
//
// api.SocketPath derives the path from XDG_RUNTIME_DIR, so pointing that at an
// empty temp dir guarantees no daemon is reachable without going near the real
// one. The calls below therefore never reach hardware.
func TestAPIReportsMissingDaemonWithoutAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	if got := api.SocketPath(); !filepath.IsAbs(got) || filepath.Dir(filepath.Dir(got)) != dir {
		t.Fatalf("socket path %q is not inside the temp dir %q — refusing to run "+
			"against a possibly real daemon", got, dir)
	}

	t.Run("get-state", func(t *testing.T) {
		handled, state, err := api.SendGetState()
		if handled {
			t.Error("handled = true with no daemon listening")
		}
		if err != nil {
			t.Errorf("err = %v, want nil (the api signals absence via handled)", err)
		}
		if state != nil {
			t.Error("state is non-nil with no daemon listening")
		}
		if got := Err(handled, err); !errors.Is(got, ErrNotRunning) {
			t.Errorf("Err(%v, %v) = %v, want ErrNotRunning", handled, err, got)
		}
	})

	// One mutating command, to show the convention is not special to get-state.
	// With no socket to dial this cannot touch the hardware.
	t.Run("tdp-reset", func(t *testing.T) {
		handled, err := api.SendTdpReset()
		if handled {
			t.Error("handled = true with no daemon listening")
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		if got := Err(handled, err); !errors.Is(got, ErrNotRunning) {
			t.Errorf("Err(%v, %v) = %v, want ErrNotRunning", handled, err, got)
		}
	})
}
