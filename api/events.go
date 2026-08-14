package api

// events.go — the event names a client can subscribe to.
//
// Events are deliberately bare notifications: the name says what happened, and
// the client calls SendGetState for the details. Carrying a state payload on the
// event would save a round trip on a local socket — worth nothing — and would
// introduce a staleness race, because a payload describes the moment the event
// was queued while get-state always answers with current truth.

const (
	// EventGUIToggle is emitted when the Armoury Crate button is pressed.
	EventGUIToggle = "gui-toggle"

	// EventGUIOpenFull is emitted in *addition* to EventGUIToggle when the
	// Armoury Crate button is pressed twice in quick succession. The pair is
	// deliberate: the first press toggles with no added latency, and a client
	// that wants the escalation subscribes to both and hides whatever the toggle
	// opened when this arrives. A daemon that waited to see whether a second
	// press was coming would put the window's length onto every single press.
	//
	// A client that does not subscribe to it never sees it, so a pre-2.0 GUI is
	// unaffected — subscription filtering is what makes this additive.
	EventGUIOpenFull = "gui-open-full"

	// EventPowerSource is emitted when the machine moves between mains and
	// battery power. It fires on the transition itself, so it is useful whether
	// or not autoswitch is configured — a client can drive a plug/battery
	// indicator from it alone.
	EventPowerSource = "power-source"

	// EventStateChanged is emitted when the active profile, its settings, the
	// saved profiles, or the autoswitch configuration change — whatever the
	// cause: this client, another client, the CLI, autoswitch, or a resume.
	// A client displaying profile, TDP, fan curve, or undervolt values should
	// re-read them with SendGetState when it arrives.
	//
	// Lighting is deliberately excluded. A brightness slider drag would emit a
	// burst of events describing values the client just set itself.
	EventStateChanged = "state-changed"
)

// A "device-changed" event is deliberately absent, and this note exists so its
// absence reads as a decision rather than an oversight.
//
// The roadmap specifies one: payload-free, telling clients to re-fetch
// device-get. Nothing can emit it today. The daemon assembles its device once
// at startup from the DMI-matched device data and never reassigns it, and the
// document is a pure projection of that value — so the capability set a client
// holds cannot go stale while the daemon lives. Adding the name now would
// publish an event that never fires, which is the same trap as a document
// declaring a capability nothing reads: it looks like a feature and delivers
// nothing, and a client could reasonably write a handler that is dead code.
//
// The real trigger is the external plugin tier, where a plugin registering,
// crashing or being removed genuinely changes what the machine can do. It
// belongs in that change, beside the thing that fires it.

// AllEvents lists every event name the daemon can emit. Subscribing with an
// empty event list is equivalent to subscribing to all of them.
var AllEvents = []string{EventGUIToggle, EventGUIOpenFull, EventPowerSource, EventStateChanged}
