# `gui-open-full`'s consumer is a second GTK surface, and the views it shows are second *instances* rather than a second implementation

**`gui-open-full`'s consumer is a second GTK surface, and the views it shows
are second *instances* rather than a second implementation.** The drawer and
the full window both hosted `dashboardView` and `customView` when this was
written (the drawer's copies were later removed — entries below); what
differed was carried in a `viewHost` — today two fields, whether this page
is on screen and which error bar, since the third (how to leave) lost its
last producer. A view that reads `w.viewStack` — the drawer's — is a view that
cannot live anywhere else, and every such read is now a `host.current()`
call. Making that true needed the constructors to *return* the view instead
of assigning `w.dashboard`, and the focus lists to move onto the view; both
were already how `colorView` and `themeView` worked, so the split was
finishing a pattern rather than inventing one.
Three Window-level things had to widen with it, each because it had been
written against the drawer alone: the gamepad reader's gate and the get-state
poll's (now `anyVisible`, or the window opens with a dead controller and
frozen readouts), and the error bar — there is one per *surface* now, and
`reportError` fans out, because the drawer's bar is hidden whenever the window
is up and a failure reported only to it is invisible. That is issue #14's
shape reintroduced by a second surface.
**Gamescope hosts the full window inside its own surface, and the claim that
it could not was wrong.** What does not composite there is a second
*toplevel*: only one window carries `STEAM_OVERLAY` and
`GetPossibleFocusWindows()` skips `isOverlay`-flagged windows. That is a fact
about second windows, not about screen space — the gamescope backend's window
is *already fullscreen*, so the full window there is a different **layout of
a surface we already own**. HHD is the existence proof and the same shape:
its sidebar and its larger settings view are one Electron surface
re-laying-out its contents, which is also why its menus work where
`GtkDropDown` does not.
`fullSurfaceHost` (`backend.go`) is the seam — `SetFullChild` + `ShowFull`,
implemented by gamescope alone. It is an *optional* interface because
layer-shell and overlay have nothing to implement: they use the real toplevel,
which is the better surface where it works, and two no-op methods would
suggest a choice where there is none. `Window.fullHost()` is the only place
that asks, so `mainWindow.win` being nil is confined to a handful of guards.
Two things are load-bearing. The stack sits **above** the 320px panel: the
drawer's own view stack lives *inside* it, so a page added there would be a
320px "full window" — that part of the old note was right. And `ShowFull(true)`
with no installed page is **ignored**, because switching a `GtkStack` to a
missing child leaves the surface blank with no way back, and a blank
fullscreen overlay over a running game is the worst failure that file can
produce.
The hosted path inverts the drawer handling: `openFull` *shows* the drawer
first (the surface must be up before a page inside it can be), where the
toplevel path hides it (a separate window replaces it).
**The drawer's dashboard view is gone with it.** It existed only as the
gamescope fallback, and with a real full window there it had no caller —
dead code that reads as a fallback is worse than either option. This also
matches the standing rule that the dashboard belongs to the full window
(Jeff, 2026-08-13): the drawer is quick controls, and a chart at 320px is not
one. `VOLTAIRE_GUI_DUMP_FOCUS` diffed to exactly one removed grid
(`view=dashboard`), every other list byte-identical including both full-window
pages. The gamescope path was first run on hardware by Jeff on 2026-11-15;
see Open work item 2 for what it found and what still needs a re-check.
