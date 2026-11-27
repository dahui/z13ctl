# `hwMu` guards hardware mutation sequences; lock order is `hwMu` then `d.mu`

**`hwMu` guards hardware mutation sequences; lock order is `hwMu` then `d.mu`.**
`applyProfileLocked` states this in its doc comment because it is the one
function both a socket handler and a watcher call: the caller holds `hwMu` and
must NOT hold `d.mu`, and nothing reached from it may call
`d.effectiveProfile()`, which takes `d.mu`. It also decides custom-ness and
snapshots the profile in a single `d.mu` critical section, closing the window
where a concurrent `profile-delete` could remove the entry between the two.
`d.state.Profile` is set *before* the hardware work, so the reconcile watcher
starts defending the fans during the apply and a half-failed apply self-heals
on the next tick.

`d.mu` guards state, and every mutating handler does its hardware I/O outside
it, so nothing otherwise stops the reconcile watcher interleaving its
`asus.setFanCurves` with `handleProfile`'s fan release — the fans would
keep whichever mode landed last. `handleProfile`'s "custom" branch used to hold
`d.mu` across the `cli.*` calls and now snapshots first, so the order holds
everywhere. `*-get` handlers deliberately do not take `hwMu`: blocking a GUI
read behind a fan write sequence would be a regression.
