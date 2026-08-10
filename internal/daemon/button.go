package daemon

// button.go — the hardware button watcher, and what a press means.
//
// The driver reports that a press happened; deciding which client event that
// becomes is the daemon's job, and this is where it lives. Today that is one
// rule with a timing window in it — a second press soon after the first
// additionally asks for the full window — which is exactly the sort of thing
// worth having as a pure function with a table rather than as three
// comparisons inside a select.

import (
	"context"
	"log/slog"
	"time"

	"github.com/dahui/voltaire/api/v2"
	"github.com/dahui/voltaire/v2/internal/driver"
)

// The double-press window. Both bounds are load-bearing and neither is a round
// number chosen for looks.
//
// The lower bound exists because some firmware revisions report a single
// Armoury Crate press twice in the same evdev instant — the duplicate the GUI's
// own togglegate was built to swallow. Without a floor, every press on that
// hardware would be a double-press, so the drawer would open the full window
// every time and the quickbar would be unreachable. 50 ms is the same figure
// togglegate uses, measured on the same hardware.
//
// The upper bound has to sit above the human floor: deliberate tapping on a Z13
// was measured bottoming out near 129 ms between presses, and a comfortable
// double-tap lands around 200–300 ms. 400 ms covers that with room, while
// staying short enough that two unrelated presses — open the drawer, look at
// it, press again to close — are not mistaken for one gesture.
const (
	doublePressMin = 50 * time.Millisecond
	doublePressMax = 400 * time.Millisecond
)

// pressState is what the classifier remembers between presses.
type pressState struct {
	// last is the previous press that is still eligible to be the first half of
	// a pair. Zero means there is none — either this is the first press since
	// the daemon started, or the previous pair already completed.
	last time.Time
}

// pressAction is what one press should emit.
type pressAction struct {
	Toggle   bool
	OpenFull bool
	Reason   string
}

// pressTick decides what a press at now means and returns the state to carry
// forward.
//
// Toggle is unconditional, and that is the whole latency argument: the first
// press opens the quickbar immediately rather than waiting to see whether a
// second one arrives. A client that wants the escalation subscribes to both
// events and hides the quickbar when the second arrives; the brief flash is the
// accepted trade for never adding the window's length to a single press.
//
// A completed pair clears last, so three presses in a row produce one
// escalation rather than two. Without that, holding the button down through an
// autorepeat — or a genuinely enthusiastic triple tap — would emit gui-open-full
// on every press after the first.
func pressTick(st pressState, now time.Time) (pressState, pressAction) {
	act := pressAction{Toggle: true, Reason: "press"}
	if st.last.IsZero() {
		return pressState{last: now}, act
	}

	gap := now.Sub(st.last)
	switch {
	case gap < doublePressMin:
		// A hardware duplicate, not a gesture. It still toggles, exactly as it
		// did before this rule existed, and the client debounces it.
		act.Reason = "duplicate press"
		return pressState{last: now}, act
	case gap > doublePressMax:
		act.Reason = "press (too slow to pair)"
		return pressState{last: now}, act
	}

	act.OpenFull = true
	act.Reason = "double press"
	return pressState{}, act
}

// watchButtons runs the device's button watcher, forwarding each press to
// buttonCh for the broadcast loop.
//
// The timestamp is taken here rather than where the channel is read, because
// the reader can be held up broadcasting to a slow subscriber (bounded, but not
// zero). Two presses queued behind one of those would be read back to back and
// their gap would read as a hardware duplicate — a real double press classified
// as noise, and only ever on a machine with a subscriber that had stalled.
func (d *Daemon) watchButtons(ctx context.Context) {
	events := make(chan driver.ButtonEvent, 4)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-events:
				select {
				case d.buttonCh <- time.Now():
				default: // non-blocking: discard if nobody consuming
				}
			}
		}
	}()
	if err := d.hw.Buttons.Watch(ctx, events); err != nil {
		slog.Warn("button watcher stopped", "err", err)
	}
}

// emitPress broadcasts the events one press calls for.
//
// OK must be true on both: `ok` has no omitempty, so leaving it zero ships
// {"ok":false,...} on a perfectly good event, and any client that checks ok
// before dispatching — the documented contract — silently drops every press.
func (d *Daemon) emitPress(act pressAction) {
	if act.Toggle {
		d.broadcast(response{OK: true, Event: api.EventGUIToggle})
	}
	if act.OpenFull {
		d.broadcast(response{OK: true, Event: api.EventGUIOpenFull})
	}
	slog.Debug("button", "reason", act.Reason, "openFull", act.OpenFull)
}
