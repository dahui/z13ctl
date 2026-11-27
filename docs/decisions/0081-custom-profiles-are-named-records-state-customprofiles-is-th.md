# Custom profiles are named records; `state.CustomProfiles` is the only in-memory truth

**Custom profiles are named records; `state.CustomProfiles` is the only
in-memory truth.** A custom profile is a `CustomProfile{Name, FanCurve, TDP,
Undervolt}` — per-subsystem *pointers*, so `nil` means "this profile does not
control that subsystem" and a new subsystem (GPU PPT, dynamic boost) is an
additive field that leaves old profiles loadable. None of them writes
`platform_profile`; the firmware profile underneath stays as-is.
`api.State.FanCurve/TDP/Undervolt` still exist but are a **projection**, filled
in only by `withLegacyProjection` at the two serialization boundaries
(`get-state` and `saveState`) so z13gui and the Decky plugin keep working. The
source is the active custom profile, or `custom` when a firmware profile is
active — i.e. whatever a bare `undervolt --set` edits and `profile --set custom`
recalls. Projecting *nothing* on a firmware profile looks tidier and is a
regression on both sides: a GUI showing "saved undervolt, not active" loses the
value it displays, and a downgrade taken while on `balanced` writes those
settings away entirely. `loadState` clears them after migrating, so nothing inside
the daemon can read the stale copy instead of the map; internal readers go
through `state.ActiveCustomProfile()`. Any new pointer/slice/map on `api.State`
must also be added to `cloneState` — `clone_test.go` under `-race` is the only
thing that catches a shallow copy of the profile map, and each entry needs a
copy of its own (three pointers and a slice), not just a new map header.
