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

// AllEvents lists every event name the daemon can emit. Subscribing with an
// empty event list is equivalent to subscribing to all of them.
var AllEvents = []string{EventGUIToggle, EventPowerSource, EventStateChanged}
