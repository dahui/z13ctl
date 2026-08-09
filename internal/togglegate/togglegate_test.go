// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package togglegate

import (
	"testing"
	"time"
)

func TestAcceptAllowsFirstEvent(t *testing.T) {
	acceptedAt, ok := Accept(time.Time{}, time.Unix(100, 0), 250*time.Millisecond)
	if !ok {
		t.Fatal("first daemon toggle should be accepted")
	}
	if acceptedAt.IsZero() {
		t.Fatal("accepted timestamp should be recorded")
	}
}

func TestAcceptSuppressesBurst(t *testing.T) {
	base := time.Unix(100, 0)

	acceptedAt, ok := Accept(time.Time{}, base, 250*time.Millisecond)
	if !ok {
		t.Fatal("first daemon toggle should be accepted")
	}
	if _, ok := Accept(acceptedAt, base.Add(100*time.Millisecond), 250*time.Millisecond); ok {
		t.Fatal("duplicate burst should be suppressed")
	}
	if _, ok := Accept(acceptedAt, base.Add(300*time.Millisecond), 250*time.Millisecond); !ok {
		t.Fatal("toggle after debounce window should be accepted")
	}
}
