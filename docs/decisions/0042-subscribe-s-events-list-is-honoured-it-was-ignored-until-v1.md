# `subscribe`'s `events` list is honoured; it was ignored until v1.3.0

**`subscribe`'s `events` list is honoured; it was ignored until v1.3.0.**
`addSubscriber` now records the set and `broadcast` filters on it. That was
invisible while `gui-toggle` was the only event, and became a live hazard the
moment a second one existed: a client that subscribed to `gui-toggle` and wrote
the obvious `for range ch { toggle() }` — reasonable when only one event
existed — would toggle its window on every power-source change. A subscriber
that did *not* want an event must survive the broadcast rather than being
pruned. Events are `gui-toggle`, `gui-open-full`, `power-source` and `state-changed`
(`api/events.go`), and carry **no payload**: the name says what happened and
`get-state` answers with current truth, whereas a payload describes the moment
the event was queued. That is also why `api.Subscribe` keeps its
`<-chan string` signature — the payload was the only thing that wanted a
breaking change.
