// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package keyrepeat

import "testing"

// Directions, standing in for gamepad.Action.
type dir int

const (
	up dir = iota
	down
	left
	right
)

func TestZeroValueHasNoRepeat(t *testing.T) {
	var tr Tracker[dir]
	if tr.Active() {
		t.Error("zero value reports an active repeat")
	}
	if tr.Stop() {
		t.Error("Stop on a fresh Tracker reported it stopped something")
	}
	if tr.ReArm(0) {
		t.Error("ReArm succeeded with no repeat active")
	}
}

func TestStartThenReArm(t *testing.T) {
	var tr Tracker[dir]
	gen := tr.Start(up)
	if !tr.Active() || tr.Action() != up {
		t.Fatalf("Active=%v Action=%v, want true/up", tr.Active(), tr.Action())
	}
	if !tr.ReArm(gen) {
		t.Error("the current generation may not re-arm")
	}
	// Re-arming does not consume the generation: a repeat ticks many times.
	if !tr.ReArm(gen) {
		t.Error("second ReArm with the same generation failed")
	}
}

func TestStopPreventsReArm(t *testing.T) {
	var tr Tracker[dir]
	gen := tr.Start(up)
	if !tr.Stop() {
		t.Error("Stop reported nothing to stop")
	}
	if tr.Active() {
		t.Error("still active after Stop")
	}
	// The already-fired timer callback must become a no-op rather than keep
	// scheduling itself forever.
	if tr.ReArm(gen) {
		t.Error("a stopped generation was allowed to re-arm — direction would repeat forever")
	}
}

// The first of the two bugs: hold Up, then press Right without releasing Up. The
// Up timer has already fired and is about to re-arm. It must not, and it must not
// displace Right.
func TestSwitchingDirectionRetiresTheOldGeneration(t *testing.T) {
	var tr Tracker[dir]
	upGen := tr.Start(up)

	// ... Up's timer fires here and is now running, holding upGen ...

	rightGen := tr.Start(right)

	if tr.ReArm(upGen) {
		t.Error("the superseded Up callback was allowed to re-arm: Up would keep " +
			"repeating and Right's timer would be dropped")
	}
	if !tr.ReArm(rightGen) {
		t.Error("the current Right callback may not re-arm")
	}
	if tr.Action() != right {
		t.Errorf("Action = %v, want right", tr.Action())
	}
}

// The second bug: two directions held at once, one released. The release names
// only the direction it belongs to, so the other must survive.
func TestStopOnlyAffectsTheOwningAction(t *testing.T) {
	var tr Tracker[dir]
	gen := tr.Start(up)

	if tr.Stop(left) {
		t.Error("releasing Left stopped a repeat owned by Up")
	}
	if !tr.Active() || tr.Action() != up {
		t.Error("Up's repeat did not survive an unrelated release")
	}
	if !tr.ReArm(gen) {
		t.Error("Up may no longer re-arm after an unrelated release")
	}

	if !tr.Stop(up) {
		t.Error("releasing Up did not stop Up's own repeat")
	}
	if tr.Active() {
		t.Error("still active after the owning action was released")
	}
}

// A hat axis returning to centre reports only that axis, so it stops either of
// the two directions on it and leaves the other axis alone.
func TestStopWithAxisPair(t *testing.T) {
	yAxis := []dir{up, down}
	xAxis := []dir{left, right}

	t.Run("centring the other axis leaves the repeat", func(t *testing.T) {
		var tr Tracker[dir]
		tr.Start(down)
		if tr.Stop(xAxis...) {
			t.Error("X axis centring stopped a repeat owned by Down")
		}
		if !tr.Active() {
			t.Error("Down's repeat was lost")
		}
	})

	t.Run("centring the owning axis stops it", func(t *testing.T) {
		var tr Tracker[dir]
		tr.Start(down)
		if !tr.Stop(yAxis...) {
			t.Error("Y axis centring did not stop Down")
		}
		if tr.Active() {
			t.Error("still active after its own axis centred")
		}
	})
}

func TestStopWithNoArgumentsStopsAnything(t *testing.T) {
	for _, d := range []dir{up, down, left, right} {
		var tr Tracker[dir]
		tr.Start(d)
		if !tr.Stop() {
			t.Errorf("bare Stop did not stop a repeat owned by %v", d)
		}
		if tr.Active() {
			t.Errorf("%v still active after bare Stop", d)
		}
	}
}

// Generations must never be reused, or a long-lived stale callback could match a
// later one by coincidence and resurrect a direction nothing is holding.
func TestGenerationsAreNeverReused(t *testing.T) {
	var tr Tracker[dir]
	seen := map[uint64]bool{}
	for i := 0; i < 100; i++ {
		gen := tr.Start(up)
		if seen[gen] {
			t.Fatalf("generation %d reused on iteration %d", gen, i)
		}
		seen[gen] = true
		tr.Stop()
	}
}

// Stop must also burn a generation. Otherwise stop-then-start could hand out the
// same number a callback from before the stop is still holding.
func TestStopAdvancesTheGeneration(t *testing.T) {
	var tr Tracker[dir]
	first := tr.Start(up)
	tr.Stop()
	second := tr.Start(up)
	if first == second {
		t.Fatal("Start after Stop reissued the same generation")
	}
	if tr.ReArm(first) {
		t.Error("the pre-Stop generation may not re-arm")
	}
	if !tr.ReArm(second) {
		t.Error("the current generation may not re-arm")
	}
}
