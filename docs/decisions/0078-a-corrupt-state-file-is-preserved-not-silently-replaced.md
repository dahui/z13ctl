# A corrupt state file is preserved, not silently replaced

**A corrupt state file is preserved, not silently replaced.** `loadState`
renames an unparseable `state.json` to `state.json.corrupt` and logs before
returning defaults; the next `saveState` would otherwise overwrite it, taking
every saved setting with it and leaving nothing to diagnose. `statePath` also
falls back to `os.TempDir()` when neither `XDG_STATE_HOME` nor a home
directory resolves — the old code yielded the root-relative
`/.local/state/...`, unwritable for any non-root user.
