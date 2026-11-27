# Always snapshot state with `cloneState()` before releasing `d.mu`

**Always snapshot state with `cloneState()` before releasing `d.mu`.**
`api.State` holds a map and four pointer fields, so the plain `s := d.state`
copy still aliases live daemon state. Handlers unlock before calling
`saveState`, so marshaling an aliased snapshot races with any handler mutating
the map or dereferencing a pointer under the lock — a concurrent map
read/write, which Go turns into an unrecoverable crash rather than a catchable
panic. `internal/daemon/clone_test.go` guards this under `-race`.
