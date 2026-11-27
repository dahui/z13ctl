# The profile CRUD handlers take `hwMu` even though they write no hardware

**The profile CRUD handlers take `hwMu` even though they write no hardware.**
The edit handlers resolve a target under `d.mu`, release it for the hardware
write, then commit the profile back under `d.mu` again. Mutating the profile map
inside that window is silently undone by the commit — and for a delete, undone
by *resurrecting* the profile. `hwMu` is what makes the whole
resolve-write-commit span exclusive. It also keeps a delete from landing
part-way through `applyProfileLocked`, which would leave `state.Profile` naming
a profile that no longer exists and so stop the reconcile watcher defending a
fan curve that is live in hardware.
`internal/daemon/profile_test.go:TestProfileMutatorsTakeHwMu` is the guard.
