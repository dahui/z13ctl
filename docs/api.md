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
stream of events. Always call the returned cancel func when you stop consuming
the channel — it releases the reader goroutine, which otherwise parks once the
channel buffer fills. Calling it more than once is safe.

```go
ch, cancel, err := api.Subscribe([]string{"gui-toggle"})
if err != nil || ch == nil {
    // daemon not running, or it rejected the subscription
}
defer cancel()
for range ch {
    // toggle your window
}
```

!!! note "Timeouts"
    Connecting is bounded at 1 second and the whole request/response exchange at
    10 seconds, so a wedged daemon returns an error rather than hanging your
    process. `Subscribe` bounds only its handshake — the stream itself is
    open-ended, since subscriptions are idle by design between events.

---

## Custom profiles

Since api v1.2.0, custom settings live in named profiles. `State.CustomProfiles`
is the source of truth; `State.FanCurve`, `State.TDP`, and `State.Undervolt`
remain as a projection so existing clients keep working unchanged. They carry the
active custom profile's settings, or `custom`'s when a firmware profile is
active — the same values selecting `custom` would recall. `Undervolt.Active` is
what says whether an offset is applied to hardware right now.

Two things to move to:

**`State.Profile == "custom"` is no longer the test for custom settings.** A
named profile makes that comparison false. Use the method instead:

```go
if state.InCustomProfile() {
    // show the fan curve / TDP / undervolt controls
}
if p, ok := state.ActiveCustomProfile(); ok {
    fmt.Println(p.Name, p.TDP)
}
```

**Profile-targeted setters.** `SendFanCurveSet`, `SendTdpSet`, and
`SendUndervoltSet` are unchanged and edit the active profile. The `…For`
variants take a profile name and store the setting without applying it, which is
how a client builds the profile `SendAutoswitchSet` selects on battery:

```go
api.SendTdpSetFor("battery-uv", "35", "", "", "", false)
api.SendAutoswitchSet(true, "balanced", "battery-uv")
```

!!! danger "Probe before offering profile targeting"
    The wire field behind the `…For` variants is additive, so a daemon from
    before custom profiles drops it, **applies the setting to the running
    machine**, and answers `ok`. Call `SendProfileList` first: an older daemon
    answers `unknown command`, and that is the only reliable signal.

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

// Fan curves (applied to both fans simultaneously)
handled, value, err := api.SendFanCurveGet()
handled, err         := api.SendFanCurveSet("48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102")
handled, err         := api.SendFanCurveReset()

// TDP (PPT power limits)
handled, value, err := api.SendTdpGet()
handled, err         := api.SendTdpSet("50", "", "", "", false)  // all PPTs to 50W
handled, err         := api.SendTdpReset()

// Undervolt (Curve Optimizer — requires ryzen_smu kernel module)
handled, value, err := api.SendUndervoltGet()
handled, err         := api.SendUndervoltSet("-20")  // CPU CO -20
handled, err         := api.SendUndervoltReset()
```

**Full state snapshot (for GUI initialization):**

```go
handled, state, err := api.SendGetState()
if handled && err == nil {
    fmt.Println("lighting mode:", state.Lighting.Mode)
    fmt.Println("profile:", state.Profile)
    fmt.Println("battery limit:", state.Battery)
    fmt.Println("fan curve:", state.FanCurve)
    fmt.Println("tdp:", state.TDP)
    fmt.Println("undervolt:", state.Undervolt)
    fmt.Println("undervolt available:", state.UndervoltAvailable)
    fmt.Println("APU temp:", state.Temperature, "°C")
    fmt.Println("fan RPM:", state.FanRPM)
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

See the [API Reference](api-reference.md) page for full documentation
of all exported types and functions.
