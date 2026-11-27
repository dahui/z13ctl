# Promotion starts fresh: a bare edit from a firmware profile commits `custom` containing only that edit

**Promotion starts fresh: a bare edit from a firmware profile commits
`custom` containing only that edit** (`editTarget.freshImplicit()`; Jeff,
2026-08-14). The pre-existing behaviour adopted whatever `custom` already
stored, and both directions of that bit on the same day: a `tdp --set` from
`balanced` dragged a months-old fan curve into hardware through `handleTDP`'s
restore-what-the-profile-describes step, and a `fancurve --set` would have
handed the reconcile watcher a stored 93W TDP to restore within two seconds —
the watcher re-applies the active custom profile's TDP on *any* drift, not
just above the safe max. The discarded settings are the accepted cost:
keeping bundles is what named profiles and Save As are for, and an explicit
`--profile custom` edit still edits the bundle in place, as does a bare edit
while `custom` is already active. The *reset* handlers deliberately do not
call `freshImplicit` — they key on `implicit()` to skip the commit entirely,
because an edit overwriting the profile is the user establishing new
contents, while a reset touching it would be the daemon discarding old ones
(the silent-data-loss guard below). `TestImplicitEditStartsFresh` pins all
three target shapes.
