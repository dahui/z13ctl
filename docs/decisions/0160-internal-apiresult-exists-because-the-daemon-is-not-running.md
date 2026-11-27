# `internal/apiresult` exists because "the daemon is not running" is not an error

**`internal/apiresult` exists because "the daemon is not running" is not an
error.** Every `api.Send*` returns `(handled bool, err error)`, where
`handled == false, err == nil` means the dial failed — a CLI caller falls back
to direct hardware there, but the GUI has no such path, so it collapses the
pair into one error with a `ErrNotRunning` sentinel and a message naming
voltaire. Turning the pair into an error at each call site is how a control
reports success on a write that never happened (issue #14).
