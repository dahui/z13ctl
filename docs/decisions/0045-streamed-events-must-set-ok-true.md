# Streamed events must set `OK: true`

**Streamed events must set `OK: true`.** `response.OK` has no `omitempty`, so
`broadcast(response{Event: ...})` ships `{"ok":false,...}` on a perfectly good
event. The Go client keys on the `event` field and never noticed, but the
documented protocol says every response carries `ok`, so a Python/Decky client
honouring that contract silently dropped every button press (fixed in v1.2.1).
`internal/daemon/clone_test.go` pins the wire shape.
