# The drawer has no profile editor; it has a profile *picker

**The drawer has no profile editor; it has a profile *picker*** (Jeff,
2026-08-14: "profile editing can be done via the main window, and the drawer
can be used for quick actions, as intended"). The drawer's `Custom` control
used to open a second instance of the whole editor — a page of sliders and a
Cairo fan-curve chart in a 320px column reached in a hurry — and the full
window now carries that editor at a size worth using. It is a dropdown of the
saved custom profiles instead (`profileui.PickerRows`), and choosing one
activates it.
This is the same removal as the drawer's dashboard, for the same reason and
with the same evidence: the view had exactly one caller, `VOLTAIRE_GUI_DUMP_FOCUS`
diffed to exactly one removed grid (`view=custom`, n=23), and every other line
— `main` included, since the picker's trigger occupies the focus slot the
button did — was byte-identical. It is also what makes quickbar customization
tractable: a drawer whose sections are all quick controls is a list you can
reorder, where one that contains a whole editor is not.
Three rules in the picker. **Empty profiles are shown greyed and
"(empty)"-suffixed, not hidden** — the autoswitch targets' rule, one section
down, and for the same reason. **Selecting the running profile is a no-op**
rather than a send: the daemon refuses it with "already the active profile",
and an error bar for tapping the row already marked as current would be
reporting a failure the user could not have avoided. And **when nothing can be
picked the trigger is dead with a note in its place** — a dropdown that opens
onto one dead row is worse than a sentence — which names the *gesture* that
opens the editor (`buttonpref.OpenGesture`), because the drawer no longer has
one and a drawer-only user has no reason to know a second surface exists. The
gesture depends on the user's own button preference, so it cannot be a GTK
literal: naming it "double press" is right for the default and wrong for
anyone who swapped them.
**The knock-on was that `viewHost.back` lost its producer**, leaving every
`host.back != nil` branch and `customView.hosted()` unreachable. They were
removed in a pass of their own (2026-10-09): the field, the three views'
back headers, the profile editor's drawer layout (flat column, per-domain
Save buttons and their four handlers, TELEMETRY readout, drawer focus
list). The focus dump was byte-identical on all six lines and a Profiles
screenshot pixel-identical before and after.
