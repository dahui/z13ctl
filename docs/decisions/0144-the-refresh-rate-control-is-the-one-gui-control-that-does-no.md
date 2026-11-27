# The refresh-rate control is the one GUI control that does not go through the daemon, and `internal/display` says why in its package doc

**The refresh-rate control is the one GUI control that does not go through the
daemon, and `internal/display` says why in its package doc.** A video mode
belongs to the compositor, not to hardware voltaire owns; it is per-session;
the compositor already persists it; and the daemon is a systemd user service
with no guaranteed `WAYLAND_DISPLAY`, while a GTK client is by definition in
the session. That is a deliberate, narrow exception to "the GUI is an ordinary
socket client" — the rule exists to stop the *daemon* growing GUI-shaped
shortcuts, not to stop the GUI talking to its own session.
Capability is by absence, as everywhere else: one backend exists
(`kscreen-doctor`), `Available()` is a PATH lookup rather than a query so the
widget tree is not built behind a DBus round trip, and a machine without it
gets **no card at all** rather than a control that could only ever fail.
**It did not cover gamescope for free**, as this entry used to claim: on a
KDE install `kscreen-doctor` is on PATH in Gaming Mode too, and with no KWin
it falls back to gamescope's Xwayland, which reports no enabled screen — so
the card came up dead reading "No screen is currently enabled" (Jeff, on
hardware, 2026-11-15). Gamescope has a refresh rate of its own
(`GAMESCOPE_DYNAMIC_REFRESH` on the root window), but Steam's Quick Access
Menu writes it and would overwrite anything voltaire set, so the decision was
to hide the control there rather than add a backend that races Steam (Jeff,
2026-11-15). `display.Available()` answers false whenever
`GAMESCOPE_WAYLAND_DISPLAY` is set — the variable the GUI picks its gamescope
backend by — which takes out the card, the quickbar row (via the session
capability), `Query`/`Apply` and the power-source refresh switching in one
place. `TestGamescopeHasNoBackend` pins it; its negative control was run.
Two rules in the package earned their tests. **Resolution is held fixed** —
the rates offered are those at the current one, because a list of every mode
is a resolution picker wearing a refresh rate's label, and changing resolution
under a running session is the compositor's own settings dialog's job (it has
a confirmation timer; this control has no business reimplementing one). And
**the label rounds while the value never does**: panels report 59.868 for what
every other piece of software calls 60, so two modes a hundredth of a hertz
apart collapse to one entry — with the mode that is *running* winning the
collapse, or the control could not display the state it is in.
