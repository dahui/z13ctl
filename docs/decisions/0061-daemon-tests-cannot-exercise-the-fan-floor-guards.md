# Daemon tests cannot exercise the fan-floor guards

> **Superseded 2026-11-26.** Daemon tests now run write paths over
> `fakeDevice` (internal/daemon/fakes_test.go), and `testDev` refuses writes
> (writeguard_test.go); `TestHighTDPWritesTheFloorFirst` drives the floor
> through the handler. Kept for why the old rule existed.

**Daemon tests cannot exercise the fan-floor guards.** Whether they refuse
depends on the real `ppt_*` values and `internal/drivers/asus`'s path vars
are unexported, so a daemon test that passes the guard writes the machine's
actual fan mode. `internal/daemon/server_test.go` stays on parse-rejection
paths; the guards themselves are covered hermetically in
`internal/drivers/asus/tdp_test.go` and `internal/safety/safety_test.go`.
