# The profile name rules live in `api` (`ValidateProfileName`, `MaxProfileNameLen`); `api.ValidateProfileName` is a delegating wrapper

**The profile name rules live in `api` (`ValidateProfileName`,
`MaxProfileNameLen`); `api.ValidateProfileName` is a delegating wrapper.**
They moved for the drawer's inline name entry: the GUI may not import
`internal/cli` (the binaries share only `api` and `internal/version`), and a
second copy of the rules would drift from the one the daemon refuses with.
Any client-side pre-check must call the api function, never re-state a rule.
