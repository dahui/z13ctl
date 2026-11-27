# `VOLTAIRE_GUI_DUMP_FOCUS=1` exists because the drawer's focus lists were the one checkable thing no test could reach

**`VOLTAIRE_GUI_DUMP_FOCUS=1` exists because the drawer's focus lists were the
one checkable thing no test could reach.** `internal/gui` needs GTK4 headers,
so `make test` cannot compile it, and every view but the main one is built on
first navigation — so four of the five gamepad grids existed at runtime only
after a person tapped a button, and the parity tests in `internal/focusgrid`
could say the coordinates were right without saying they described the widgets
actually built. The flag builds every view at startup and logs all five grids
as `row:col:section`, giving a five-line fingerprint of the whole drawer that a
refactor can be diffed against. It is what made the M4 window split verifiable
rather than merely careful: each view moved out of `Window` with the dump
byte-identical to the baseline. Any future change that touches widget
construction or navigation should capture it first. It now covers the full
window too, logging its pages as `full:<tab>` — a change that left the
drawer's grids untouched and broke the window's would otherwise pass the
diff. Its sibling `VOLTAIRE_GUI_OPEN_FULL=1` opens the full window at
startup, and exists for the same reason: that window is otherwise reachable
only by double-pressing a key on one laptop, so nothing about it could be
checked while it was being written.
