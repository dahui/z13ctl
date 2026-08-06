package daemon

// events_test.go — subscriber filtering and the write deadline that keeps a
// wedged subscriber from stalling the daemon.

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/dahui/z13ctl/api"
)

// readEvent reads one event line, or returns "" if none arrives quickly.
func readEvent(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		return ""
	}
	var got struct {
		OK    bool   `json:"ok"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("event is not valid JSON: %v (%q)", err, line)
	}
	if !got.OK {
		t.Errorf("event %q shipped ok=false; a client honouring the documented contract would drop it", got.Event)
	}
	return got.Event
}

// TestBroadcastRespectsTheSubscriptionFilter is the guard for a compatibility
// trap. Before the filter existed the daemon wrote every event to every
// subscriber, which was invisible while "gui-toggle" was the only event. A
// client that subscribed to it and wrote the obvious loop —
// `for range ch { toggleWindow() }`, reasonable when only one event exists —
// would start toggling its window on every power-source change.
func TestBroadcastRespectsTheSubscriptionFilter(t *testing.T) {
	tests := []struct {
		name      string
		subscribe []string
		emit      string
		want      string
	}{
		{"subscribed to the emitted event", []string{api.EventGUIToggle}, api.EventGUIToggle, api.EventGUIToggle},
		{"not subscribed to the emitted event", []string{api.EventGUIToggle}, api.EventPowerSource, ""},
		{"subscribed to several", []string{api.EventGUIToggle, api.EventStateChanged}, api.EventStateChanged, api.EventStateChanged},
		{"one of several not requested", []string{api.EventGUIToggle, api.EventStateChanged}, api.EventPowerSource, ""},
		// An empty list keeps the pre-filter behaviour: everything.
		{"empty list receives all", nil, api.EventPowerSource, api.EventPowerSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()

			d := &Daemon{}
			d.addSubscriber(server, tt.subscribe)
			go d.broadcast(response{OK: true, Event: tt.emit})

			if got := readEvent(t, client); got != tt.want {
				t.Errorf("subscribed to %v, emitted %q, received %q, want %q",
					tt.subscribe, tt.emit, got, tt.want)
			}
		})
	}
}

// TestBroadcastKeepsFilteredSubscribers covers a subtlety in the prune: a
// subscriber that did not want this event must survive it. Dropping it would
// silently unsubscribe every client the first time an event it did not ask for
// was emitted.
func TestBroadcastKeepsFilteredSubscribers(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	d := &Daemon{}
	d.addSubscriber(server, []string{api.EventGUIToggle})

	go d.broadcast(response{OK: true, Event: api.EventPowerSource})
	if got := readEvent(t, client); got != "" {
		t.Fatalf("received %q, want nothing", got)
	}

	go d.broadcast(response{OK: true, Event: api.EventGUIToggle})
	if got := readEvent(t, client); got != api.EventGUIToggle {
		t.Errorf("received %q after a filtered event, want the subscriber to still be registered", got)
	}
}

// TestBroadcastDropsAWedgedSubscriber covers the write deadline. Broadcasts are
// emitted from handlers holding hwMu, so an unbounded write to a client that has
// stopped reading would block every hardware operation in the daemon behind a
// socket buffer.
func TestBroadcastDropsAWedgedSubscriber(t *testing.T) {
	prev := broadcastWriteTimeout
	broadcastWriteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { broadcastWriteTimeout = prev })

	// net.Pipe is unbuffered and synchronous, so a client that never reads makes
	// the write block — exactly the wedged-subscriber case.
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	d := &Daemon{}
	d.addSubscriber(server, nil)

	done := make(chan struct{})
	go func() { defer close(done); d.broadcast(response{OK: true, Event: api.EventStateChanged}) }()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("broadcast blocked on a subscriber that never reads; this would stall every hardware operation")
	}

	d.subMu.Lock()
	n := len(d.subscribers)
	d.subMu.Unlock()
	if n != 0 {
		t.Errorf("subscribers after a timed-out write = %d, want 0 (the wedged one should be dropped)", n)
	}
}

// TestEventNamesAreStable pins the wire strings. They are part of the protocol,
// so a rename is a breaking change for every client.
func TestEventNamesAreStable(t *testing.T) {
	want := map[string]string{
		"gui-toggle":    api.EventGUIToggle,
		"power-source":  api.EventPowerSource,
		"state-changed": api.EventStateChanged,
	}
	for literal, constant := range want {
		if literal != constant {
			t.Errorf("event constant = %q, want the documented wire name %q", constant, literal)
		}
	}
	if len(api.AllEvents) != len(want) {
		t.Errorf("api.AllEvents has %d entries, want %d — a new event was not added to it", len(api.AllEvents), len(want))
	}
}
