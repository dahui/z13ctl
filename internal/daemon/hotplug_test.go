package daemon

// hotplug_test.go — Tests for the keyboard reattach watcher's state machine.

import "testing"

func TestHotplugTick(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		present      bool // previous latched presence
		observed     bool // presence reported this tick
		reattachOK   bool // result of onReattach (only relevant on a transition)
		wantNext     bool // expected new latched presence
		wantReattach bool // whether onReattach should have fired
	}{
		{
			name:         "absent to present, restore succeeds",
			present:      false,
			observed:     true,
			reattachOK:   true,
			wantNext:     true,
			wantReattach: true,
		},
		{
			name:         "absent to present, restore fails so do not latch",
			present:      false,
			observed:     true,
			reattachOK:   false,
			wantNext:     false, // retried on the next tick
			wantReattach: true,
		},
		{
			name:         "stable present, no reattach",
			present:      true,
			observed:     true,
			wantNext:     true,
			wantReattach: false,
		},
		{
			name:         "present to absent (detach), no reattach",
			present:      true,
			observed:     false,
			wantNext:     false,
			wantReattach: false,
		},
		{
			name:         "stable absent, no reattach",
			present:      false,
			observed:     false,
			wantNext:     false,
			wantReattach: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reattachCalled := false
			observe := func() bool { return tt.observed }
			onReattach := func() bool {
				reattachCalled = true
				return tt.reattachOK
			}

			got := hotplugTick(tt.present, observe, onReattach)
			if got != tt.wantNext {
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
// every tick and presence stays unlatched until the reopen finally succeeds.
func TestHotplugTick_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	observe := func() bool { return true } // keyboard present the whole time

	attempts := 0
	onReattach := func() bool {
		attempts++
		return attempts >= 3 // udev finishes applying permissions on the 3rd try
	}

	present := false
	for i := 0; i < 2; i++ {
		present = hotplugTick(present, observe, onReattach)
		if present {
			t.Fatalf("tick %d: latched present before reopen succeeded", i)
		}
	}
	// Third tick: reopen succeeds, presence latches.
	present = hotplugTick(present, observe, onReattach)
	if !present {
		t.Error("expected present=true after successful reopen")
	}
	if attempts != 3 {
		t.Errorf("onReattach attempts = %d, want 3", attempts)
	}

	// Once latched, a stable-present tick must not fire onReattach again.
	present = hotplugTick(present, observe, onReattach)
	if attempts != 3 {
		t.Errorf("onReattach fired after latching; attempts = %d, want 3", attempts)
	}
	if !present {
		t.Error("expected present to stay true while keyboard remains attached")
	}
}
