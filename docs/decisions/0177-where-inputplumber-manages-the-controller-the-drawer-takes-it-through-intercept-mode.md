# Where InputPlumber manages the controller, the drawer takes it through intercept mode, owned by the daemon

**Where InputPlumber manages the controller, the drawer takes it through
InputPlumber's intercept mode, and the daemon owns the mode** (2026-11-26,
the ROG Xbox Ally X; `internal/daemon/intercept.go`,
`internal/drivers/inputplumber/intercept.go`, `internal/gui/gamepad/intercept.go`;
`.claude/plans/gui-gamepad-inputplumber.md` has the captures).

On the Ally the drawer did not answer the controller and Steam kept the input,
in KDE and Gaming Mode. InputPlumber hides the real gamepad (mode 0000) and
presents games a `deck-uhid` target: an emulated Steam Deck controller that is
a hidraw node only, with no evdev node. Steam reads that node and writes its own
virtual Xbox pad (`28de:11ff`), which the evdev reader classifies grab-only
because it expects to read the physical controller beside it. There was
nothing to read and nothing to grab, and the hidraw blocker fired without
stopping Steam. The OXP never showed this only because InputPlumber's OXP
configs use an `xbox-elite` target, an evdev pad the reader could read and grab.

Reading each target type ourselves was rejected: a report parser per format,
hidraw grants for the GUI, and still no way to keep a game off a hidraw node,
which has no exclusive grab. Intercept mode covers every target type at once.
In `InterceptAlways` InputPlumber stops feeding the target and sends each input
to its DBus target as a named action. Measured on the Ally with a root reader of
the target's hidraw node while Jeff pressed throughout: in mode 0, 8722 reports
in 15 s with 7 distinct button fields and 1742 stick values; in mode 2, 3751
reports, all one frozen idle state. The D-pad, A, B and the bumpers arrive as
`ui_up/down/left/right`, `ui_accept`, `ui_back`, `ui_l1/r1` (press 1.0, release
0.0), exactly the set the evdev reader maps; the left stick arrives as the same
four directions; the `voltaire_*` button route keeps working throughout.

**The daemon owns the mode because a vanished client must not leave games
without a controller.** `gamepad-intercept` is a streaming command like
`subscribe`: the intercept lasts exactly as long as a connection. The first
client engages it, the last to close restores the mode found (a crash closes
the socket too), and so does daemon shutdown. A daemon killed outright, or one
exiting on a session change, leaves `$XDG_RUNTIME_DIR/voltaire/intercept.json`,
and its successor restores from it before opening the socket.

Rules each piece follows, with the test that holds it:
- Only composite devices carrying a gamepad are intercepted; a keyboard
  composite would swallow typing (`TestInterceptTakesOnlyGamepadsAndGivesThemBack`).
- Restore only what is still ours: same InputPlumber (its unique bus name), the
  mode still `InterceptAlways`, and not already intercepting before we came.
  A mode another client (OpenGamepadUI) set is left alone, and an InputPlumber
  restart re-intercepts while a client holds it
  (`TestInterceptRestoresOnlyWhatIsStillOurs`, `TestRestoreStaleIntercept`).
- The GUI opens the intercept on every grab and gives it back on release under
  the same `grabGen` ordering as the evdev grab; one landing after a hide is
  given straight back (`TestInterceptSupersededGrabGivesItBack`). A refusal
  (InputPlumber absent, polkit) leaves the evdev reader alone.
- Each source repeats its own held direction (`repeater`), so a release on one
  cannot stop a direction held on another.

polkit allows `SetInterceptMode` for `wheel` and `inputplumber` members, the
same requirement as the button route's profile load; voltaire never adds anyone
to `inputplumber` (it also grants keystroke injection). Both this and the button
route depend on receiving InputPlumber's signals, which its DBus policy plans to
deny once an alternative API exists.
