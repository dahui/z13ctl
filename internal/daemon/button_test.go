package daemon

// button_test.go — what a press means. Pure: no evdev device, no clock.

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/dahui/voltaire/api/v2"
)

var t0 = time.Date(2026, 8, 9, 21, 0, 0, 0, time.UTC)

func TestPressTick(t *testing.T) {
	cases := []struct {
		name      string
		prev      pressState
		gap       time.Duration // from prev.last; ignored when prev is zero
		openFull  bool
		keepsLast bool // whether the returned state can still pair with a next press
	}{
		{
			name: "first press since startup", prev: pressState{},
			openFull: false, keepsLast: true,
		},
		{
			// The reason the lower bound exists: some firmware reports one
			// press twice in the same instant. Treating that as a gesture would
			// make the full window open on every single press and leave the
			// quickbar unreachable.
			name: "hardware duplicate", prev: pressState{last: t0}, gap: 2 * time.Millisecond,
			openFull: false, keepsLast: true,
		},
		{
			name: "just inside the floor", prev: pressState{last: t0}, gap: doublePressMin,
			openFull: true,
		},
		{
			name: "just under the floor", prev: pressState{last: t0}, gap: doublePressMin - time.Millisecond,
			openFull: false, keepsLast: true,
		},
		{
			// A comfortable double tap, above the measured 129ms human floor.
			name: "deliberate double tap", prev: pressState{last: t0}, gap: 220 * time.Millisecond,
			openFull: true,
		},
		{
			name: "at the ceiling", prev: pressState{last: t0}, gap: doublePressMax,
			openFull: true,
		},
		{
			// Opened the drawer, looked at it, pressed again to close. One
			// gesture each, not one double.
			name: "two unrelated presses", prev: pressState{last: t0}, gap: 2 * time.Second,
			openFull: false, keepsLast: true,
		},
		{
			// A clock step backwards must not panic or accidentally pair.
			name: "time went backwards", prev: pressState{last: t0}, gap: -time.Second,
			openFull: false, keepsLast: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := t0
			if !tc.prev.last.IsZero() {
				now = tc.prev.last.Add(tc.gap)
			}
			next, act := pressTick(tc.prev, now)

			// Every press toggles, always. That is the latency argument: the
			// quickbar opens on the first press rather than after the window.
			if !act.Toggle {
				t.Error("Toggle = false; every press must toggle")
			}
			if act.OpenFull != tc.openFull {
				t.Errorf("OpenFull = %v, want %v (reason %q)", act.OpenFull, tc.openFull, act.Reason)
			}
			if act.Reason == "" {
				t.Error("action carries no reason")
			}
			if got := !next.last.IsZero(); got != tc.keepsLast {
				t.Errorf("state keeps last = %v, want %v", got, tc.keepsLast)
			}
			if tc.keepsLast && !next.last.Equal(now) {
				t.Errorf("state kept %v, want the press just seen (%v)", next.last, now)
			}
		})
	}
}

func TestTripleTapEscalatesOnce(t *testing.T) {
	// A completed pair clears the state, so a third press starts a new one
	// rather than escalating again. Without that, an enthusiastic triple tap —
	// or a button autorepeating — emits gui-open-full on every press after the
	// first.
	var st pressState
	now := t0
	var opens int
	for range 3 {
		var act pressAction
		st, act = pressTick(st, now)
		if act.OpenFull {
			opens++
		}
		now = now.Add(200 * time.Millisecond)
	}
	if opens != 1 {
		t.Errorf("three presses produced %d escalations, want 1", opens)
	}
}

func TestDuplicateThenRealPressStillPairs(t *testing.T) {
	// The sequence a machine with duplicate-reporting firmware actually
	// produces: press, its instant duplicate, then the user's second press.
	// The duplicate does not escalate, and it must not eat the real pair
	// either — the gesture is still a double tap to the person making it.
	var st pressState
	var act pressAction

	st, act = pressTick(st, t0)
	if act.OpenFull {
		t.Fatal("first press escalated")
	}
	st, act = pressTick(st, t0.Add(2*time.Millisecond)) // duplicate
	if act.OpenFull {
		t.Fatal("hardware duplicate escalated")
	}
	_, act = pressTick(st, t0.Add(210*time.Millisecond)) // the user's second press
	if !act.OpenFull {
		t.Error("the real second press did not escalate")
	}
}

func TestPressWindowBoundsAreSane(t *testing.T) {
	// Both bounds are measured figures, not round numbers. Pin the
	// relationships rather than the values, so tuning one cannot silently
	// invert the rule.
	if doublePressMin >= doublePressMax {
		t.Fatalf("window is empty: %v..%v", doublePressMin, doublePressMax)
	}
	// Below the measured human tapping floor (~129ms), or a deliberate double
	// tap would be classified as a hardware duplicate.
	if doublePressMin >= 129*time.Millisecond {
		t.Errorf("doublePressMin = %v, at or above the measured human floor", doublePressMin)
	}
	// Above it, or a comfortable double tap would be too slow to pair.
	if doublePressMax <= 129*time.Millisecond {
		t.Errorf("doublePressMax = %v, at or below the measured human floor", doublePressMax)
	}
}

// TestPressEmitsTheEventsInOrder covers everything between the classifier and
// the wire: that a single press ships one event, that a double ships the toggle
// *first* and the escalation second, and that both carry ok=true.
//
// The order is the part worth pinning. A client is told to hide whatever the
// toggle opened when the escalation arrives, so an escalation delivered before
// its own toggle would open the full window and then have the quickbar appear
// over it. Nothing but this ordering prevents that.
//
// The one thing not covered here is evdev delivery itself — that needs a finger
// on the Armoury Crate button.
func TestPressEmitsTheEventsInOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  pressAction
		want []string
	}{
		{"single press", pressAction{Toggle: true}, []string{api.EventGUIToggle}},
		{"double press", pressAction{Toggle: true, OpenFull: true},
			[]string{api.EventGUIToggle, api.EventGUIOpenFull}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()

			d := &Daemon{}
			d.addSubscriber(server, api.AllEvents)

			go d.emitPress(tc.act)

			reader := bufio.NewReader(client)
			for i, want := range tc.want {
				_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
				line, err := reader.ReadBytes('\n')
				if err != nil {
					t.Fatalf("event %d (%s) never arrived: %v", i, want, err)
				}
				var got struct {
					OK    bool   `json:"ok"`
					Event string `json:"event"`
				}
				if err := json.Unmarshal(line, &got); err != nil {
					t.Fatalf("event %d is not valid JSON: %v (%q)", i, err, line)
				}
				if got.Event != want {
					t.Errorf("event %d = %q, want %q", i, got.Event, want)
				}
				if !got.OK {
					t.Errorf("event %q shipped ok=false", got.Event)
				}
			}
		})
	}
}

// TestOpenFullIsFilterable is the compatibility guard: a client subscribed only
// to gui-toggle — every pre-2.0 GUI — must not receive the new event, and must
// survive the daemon emitting it. Both halves matter; a subscriber that did not
// want an event has to be left connected rather than pruned.
func TestOpenFullIsFilterable(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	d := &Daemon{}
	d.addSubscriber(server, []string{api.EventGUIToggle})

	go d.emitPress(pressAction{Toggle: true, OpenFull: true})

	if got := readEvent(t, client); got != api.EventGUIToggle {
		t.Fatalf("first event = %q, want the toggle", got)
	}

	d.subMu.Lock()
	n := len(d.subscribers)
	d.subMu.Unlock()
	if n != 1 {
		t.Errorf("subscribers = %d, want 1 — an unwanted event must not drop the subscriber", n)
	}
}
