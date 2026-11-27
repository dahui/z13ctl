package daemon

// hub.go — the event subscribers: long-lived connections, each with the events
// it asked for. Its lock guards only the list and is never held across a call
// out of the daemon, so it takes no part in the hwMu → d.mu order.

import (
	"net"
	"sync"
	"time"
)

// broadcastWriteTimeout bounds a single write to one subscriber.
//
// Broadcasts are emitted from handlers that hold hwMu, so an unbounded write to
// a subscriber that has stopped reading — a hung GUI, a suspended process —
// would block every hardware operation in the daemon behind a socket buffer.
// A subscriber that cannot take a notification in this long is dropped. Declared
// as a var so tests can shorten it.
var broadcastWriteTimeout = 2 * time.Second

// subscriber is one long-lived event connection and the events it asked for.
type subscriber struct {
	conn net.Conn
	// events is the set the client subscribed to. Empty means every event: that
	// is what a client sending no list gets, and it is what the daemon did for
	// every subscriber before the filter existed.
	events map[string]bool
}

// wants reports whether this subscriber asked for the named event.
func (s subscriber) wants(event string) bool {
	return len(s.events) == 0 || s.events[event]
}

// hub is the subscriber list.
type hub struct {
	mu   sync.Mutex
	subs []subscriber
}

// add registers a connection for the named events.
func (h *hub) add(conn net.Conn, events []string) {
	set := make(map[string]bool, len(events))
	for _, e := range events {
		if e != "" {
			set[e] = true
		}
	}
	h.mu.Lock()
	h.subs = append(h.subs, subscriber{conn: conn, events: set})
	h.mu.Unlock()
}

// broadcast writes data to every subscriber that asked for event, and drops
// (closes) any whose write fails or times out.
//
// The filter matters as soon as there is more than one event name: a client
// that subscribed to "gui-toggle" and reasonably wrote `for range ch { toggle() }`
// — the obvious loop when only one event existed — would otherwise toggle its
// window on every power-source change.
func (h *hub) broadcast(event string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	alive := h.subs[:0:0]
	for _, s := range h.subs {
		if !s.wants(event) {
			alive = append(alive, s)
			continue
		}
		_ = s.conn.SetWriteDeadline(time.Now().Add(broadcastWriteTimeout))
		_, err := s.conn.Write(data)
		_ = s.conn.SetWriteDeadline(time.Time{})
		if err == nil {
			alive = append(alive, s)
		} else {
			_ = s.conn.Close()
		}
	}
	h.subs = alive
}

// closeAll closes and forgets every subscriber (the daemon is stopping).
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs {
		_ = s.conn.Close()
	}
	h.subs = nil
}

// count is how many subscribers there are.
func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
