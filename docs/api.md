# API Module

The `api/` module is a standalone Go library for communicating with the z13ctl
daemon from external tools — GUI frontends, Decky plugins, shell integrations,
or anything else that wants to control z13ctl programmatically.

## Import

```go
import "github.com/dahui/z13ctl/api"
```

The module is deliberately stdlib-only (no third-party dependencies) so that
integrations can pull it in without inheriting the CLI's dependency tree.

It is a separate Go module at `./api` with its own `go.mod`. If you are working
on both the main binary and the API library simultaneously, create a
[go.work](https://go.dev/ref/mod#workspaces) file:

```sh
go work init . ./api
```

---

## Connection model

All `Send*` functions open a fresh Unix socket connection to the daemon, send
one JSON request, read one JSON response, and close the connection. This is
intentionally simple and stateless.

If the daemon is not running (connection refused), every `Send*` function returns
`(false, nil)` — the first return value (`handled bool`) signals whether the
daemon was reached. Callers can use this to decide whether to fall back to direct
hardware access.

```go
handled, err := api.SendApply("", "FF0000", "000000", "static", "normal", 3)
if !handled {
    // daemon not running; do your own HID access here
}
```

`Subscribe` follows the same pattern but holds the connection open to receive a
stream of events.

---

## Socket path

```go
path := api.SocketPath()
// $XDG_RUNTIME_DIR/z13ctl/z13ctl.sock  (or /tmp/z13ctl/z13ctl.sock)
```

---

## Examples

**Apply lighting:**

```go
// Static cyan at full brightness on all devices
handled, err := api.SendApply("", "00FFFF", "000000", "static", "normal", 3)

// Breathe between red and blue on the keyboard only
handled, err := api.SendApply("keyboard", "FF0000", "0000FF", "breathe", "slow", 3)
```

**System settings:**

```go
// Battery limit
handled, limit, err := api.SendBatteryLimitGet()
handled, err       := api.SendBatteryLimitSet(80)

// Performance profile
handled, profile, err := api.SendProfileGet()
handled, err          := api.SendProfileSet("performance")

// Boot sound and panel overdrive
handled, err := api.SendBootSoundSet(0)
handled, err := api.SendPanelOverdriveSet(1)
```

**Full state snapshot (for GUI initialization):**

```go
handled, state, err := api.SendGetState()
if handled && err == nil {
    fmt.Println("lighting mode:", state.Lighting.Mode)
    fmt.Println("profile:", state.Profile)
    fmt.Println("battery limit:", state.Battery)
}
```

**Subscribe to Armoury Crate button events:**

```go
ch, cancel, err := api.Subscribe([]string{"gui-toggle"})
if ch == nil {
    // daemon not running
}
defer cancel()
for event := range ch {
    fmt.Println("received:", event)
}
```

---

## Full API reference

See the [API Reference](api-reference.md) page for auto-generated documentation
of all exported types and functions.
