package daemon

// hotplug_test.go — Tests for the zone reattach watcher's state machine.

import (
	"slices"
	"testing"
)

func TestHotplugTick(t *testing.T) {
	t.Parallel()

	both := []string{"keyboard", "lightbar"}
	bar := []string{"lightbar"}
	tests := []struct {
		name         string
		present      []string // previous latched zones
		observed     []string // zones present this tick
		reattachOK   bool     // result of onReattach (only relevant when a zone appears)
		wantNext     []string // expected new latched zones
		wantReattach bool     // whether onReattach should have fired
	}{
		{"keyboard reattached, restore succeeds", bar, both, true, both, true},
		{"keyboard reattached, restore fails so do not latch it", bar, both, false, bar, true},
		{"stable, no reattach", both, both, false, both, false},
		{"keyboard detached, no reattach", both, bar, false, bar, false},
		{"nothing present", nil, nil, false, nil, false},
		// A zone the hardware never has (a SKU without it) is never latched and
		// never blocks: the keyboard returning still triggers the reopen.
		{"declared zone that never appears", nil, []string{"keyboard"}, true, []string{"keyboard"}, true},
		// The startup open failed, so nothing was latched: every present zone is
		// new, and the reopen is attempted.
		{"startup open failed", nil, both, true, both, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reattachCalled := false
			observe := func() []string { return tt.observed }
			onReattach := func() bool {
				reattachCalled = true
				return tt.reattachOK
			}

			got := hotplugTick(tt.present, observe, onReattach)
			if !slices.Equal(got, tt.wantNext) {
				t.Errorf("hotplugTick() = %v, want %v", got, tt.wantNext)
			}
			if reattachCalled != tt.wantReattach {
				t.Errorf("onReattach called = %v, want %v", reattachCalled, tt.wantReattach)
			}
		})
	}
}

// TestHotplugTick_RetriesUntilSuccess verifies the retry behavior across ticks:
// while the keyboard is present but the reopen keeps failing, onReattach fires
// every tick and the keyboard stays unlatched until the reopen finally succeeds.
func TestHotplugTick_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	both := []string{"keyboard", "lightbar"}
	observe := func() []string { return both } // keyboard back the whole time

	attempts := 0
	onReattach := func() bool {
		attempts++
		return attempts >= 3 // udev finishes applying permissions on the 3rd try
	}

	present := []string{"lightbar"}
	for i := 0; i < 2; i++ {
		present = hotplugTick(present, observe, onReattach)
		if slices.Contains(present, "keyboard") {
			t.Fatalf("tick %d: latched the keyboard before reopen succeeded", i)
		}
	}
	// Third tick: reopen succeeds, the keyboard latches.
	present = hotplugTick(present, observe, onReattach)
	if !slices.Equal(present, both) {
		t.Errorf("present = %v after the successful reopen, want %v", present, both)
	}
	if attempts != 3 {
		t.Errorf("onReattach attempts = %d, want 3", attempts)
	}

	// Once latched, a stable tick must not fire onReattach again.
	present = hotplugTick(present, observe, onReattach)
	if attempts != 3 {
		t.Errorf("onReattach fired after latching; attempts = %d, want 3", attempts)
	}
	if !slices.Equal(present, both) {
		t.Error("expected both zones to stay latched while attached")
	}
}
