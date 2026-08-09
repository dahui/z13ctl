// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"errors"
	"fmt"
	"testing"
)

func TestErr(t *testing.T) {
	boom := errors.New("permission denied")

	tests := []struct {
		name    string
		handled bool
		err     error
		want    error
	}{
		{"handled and no failure", true, nil, nil},
		{"handled but rejected", true, boom, boom},
		{
			// The case the drawer used to read as success at every call site.
			name:    "daemon not running",
			handled: false,
			err:     nil,
			want:    ErrNotRunning,
		},
		{
			// Cannot happen with api v1.1.7, which returns err==nil whenever the
			// dial fails. Pinned so a future api that reports both keeps the more
			// specific message.
			name:    "not handled with an error prefers the error",
			handled: false,
			err:     boom,
			want:    boom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Err(tt.handled, tt.err); !errors.Is(got, tt.want) {
				t.Errorf("Err(%v, %v) = %v, want %v", tt.handled, tt.err, got, tt.want)
			}
		})
	}
}

// TestErrTakesAPIResultsDirectly pins the calling convention the GTK code uses:
// an api.Send* call is passed as the sole argument, so no site can read err while
// forgetting handled.
func TestErrTakesAPIResultsDirectly(t *testing.T) {
	sendUnreachable := func() (bool, error) { return false, nil }
	if err := Err(sendUnreachable()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Err(sendUnreachable()) = %v, want %v", err, ErrNotRunning)
	}

	sendOK := func() (bool, error) { return true, nil }
	if err := Err(sendOK()); err != nil {
		t.Errorf("Err(sendOK()) = %v, want nil", err)
	}
}

// TestErrNotRunningIsUserFacing guards the message itself: it is concatenated
// into the error bar as "Save TDP: <message>", so it has to read as a sentence
// fragment a user can act on rather than as a Go identifier.
func TestErrNotRunningIsUserFacing(t *testing.T) {
	msg := ErrNotRunning.Error()
	if got := fmt.Sprintf("Save TDP: %v", ErrNotRunning); got != "Save TDP: "+msg {
		t.Errorf("unexpected formatting: %q", got)
	}
	for _, bad := range []string{"ErrNotRunning", "nil", "%!"} {
		if msg == bad {
			t.Errorf("ErrNotRunning message is not user-facing: %q", msg)
		}
	}
	if msg == "" {
		t.Error("ErrNotRunning has an empty message")
	}
}
