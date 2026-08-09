// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package hidblocker

import (
	"os"
	"testing"
)

func TestLsmEnabled(t *testing.T) {
	// Just verify it doesn't panic — result depends on kernel config.
	result := lsmEnabled()
	t.Logf("lsmEnabled: %v", result)
}

func TestHidrawMajor(t *testing.T) {
	major, err := hidrawMajor()
	if err != nil {
		t.Skipf("hidraw not available: %v", err)
	}
	if major == 0 {
		t.Fatal("hidraw major should be non-zero")
	}
	t.Logf("hidraw major: %d", major)
}

func TestNew(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root or CAP_BPF")
	}
	if !lsmEnabled() {
		t.Skip("BPF LSM not enabled")
	}

	b, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer b.Close()

	if err := b.Block(99999); err != nil {
		t.Fatalf("Block failed: %v", err)
	}
	if err := b.Unblock(99999); err != nil {
		t.Fatalf("Unblock failed: %v", err)
	}
	b.UnblockAll()
}
