# The Telemetry tab became the Dashboard: charts *and* the live controls

**The Telemetry tab became the Dashboard: charts *and* the live controls**
(Jeff, 2026-08-14 — "our telemetry page is actually supposed to be a general
use dashboard, not just for monitoring"). Profile, autoswitch, charge limit,
refresh rate and RGB now sit under the tiles, and the tab's title changed to
match. Its **ID did not** and must not: `dashboard` is the GTK stack child
name, the name the focus dump logs the page under, and what will land in the
user's config once the window remembers its last page.
The dividing line against the Profiles page is **what a control changes**: the
dashboard changes what the machine is doing now, the editor changes what a
saved profile *says*. That is why **autoswitch moved here** — it was put on
the Profiles page hours earlier on the reading that it selects profiles, and
selecting a profile is a live act, not an edit to one. Power limits, the fan
curve and the undervolt stay on the editor by the same rule.
**The layout was corrected twice, and both corrections are the same lesson.**
The first cut put the controls in a 320px rail down the left, reasoning that
320 is the drawer's own width so every block would be used at the size it was
designed for. Jeff: "you stacked the controls vertically again... we don't
need it to be in the same format as the drawer. Adjust the layout so that it
takes advantage of the larger window, and keep the telemetry tiles on the
top." The second: with the tiles back on top and the controls across the
width, they were still *drawer units* — a title over a full-width widget, a
slider whose value floats over its handle, a 3×2 keypad of effect buttons.
Jeff: "right now it consists mostly of units that match the drawer. Let's work
to find the best possible UX for a window view like this." They are desktop
form rows now (`internal/gui/formrow.go`), one setting per line with its name
in a fixed label column — the boxed-list idiom, and the one the window's own
Settings tab already used. Reusing a *view* across surfaces is the established
win; reusing its *arrangement*, or its control shapes, is not — and all three
are easy to conflate.
The per-control decisions (why Custom takes an ellipsis, why a slider's value
moves out of the trough, why brightness is named rather than numbered, why the
effects are one row of six) are in `internal/gui/CLAUDE.md`, each with the
thing it was getting wrong.
