# The GUI's environment variables renamed with a fallback: `startup.GUIEnv(suffix)`

**The GUI's environment variables renamed with a fallback:
`startup.GUIEnv(suffix)`** reads `VOLTAIRE_GUI_<suffix>` and falls back to the
pre-rename `Z13GUI_<suffix>` when the new name is empty — same 2.x contract as
the socket path, removed at 3.0. The helper lives in `internal/startup` (pure,
tested) rather than at the call sites, which are in the untestable cgo island;
a new GUI env var must go through it, not `os.Getenv`. A non-empty new name
wins; both variables treat non-empty as their active state, so set-to-empty
needs no distinguishing.
