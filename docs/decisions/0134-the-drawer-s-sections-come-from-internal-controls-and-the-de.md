# The drawer's sections come from `internal/controls`, and the default list is a transcription of what shipped — not a fresh design

**The drawer's sections come from `internal/controls`, and the default list is
a transcription of what shipped — not a fresh design.** `buildContent` used to
answer "which sections exist", "in what order" and "what does each look like"
with one literal run of `Append` calls. The quickbar M4 wants needs the first
two as *data*, before any widget exists, so they moved to a pure package and
`internal/gui` kept only the third. The acceptance test is that a user with no
`gui.toml` sees the drawer they had before, which is why
`TestDefaultOrderIsTheShippedLayout` reads as a transcription and should only
ever change alongside a deliberate decision to move something.
**`Layout` is part of that, not a convenience.** The heading/separator rule —
a heading wherever the group changes, a separator before every heading but the
first — is what the user actually sees, so it belongs where `make test` can
reach it rather than in the build loop. Group is a field on the *control* so a
reorder cannot strand a heading above the wrong section; interleaving two
groups repeats the heading, which is the honest rendering of what was asked
for.
**The two halves of a control live in one struct (`controlBuilder`).** The
build run and the focus-list run were two literal sequences nothing forced to
agree, and a control present in one but not the other is either invisible or
unreachable by controller — the second being the failure nobody notices with a
mouse in their hand. Both now come from one map, walked in the resolved order.
The footer is deliberately *not* a registry control: it is fixed chrome outside
the scroll area, so "the things you can reorder" and "the things that scroll"
stay the same set.
`Requires` is a slice because autoswitch genuinely needs two capabilities —
it selects profiles, and it fires on a power-source change the daemon can only
observe through the battery capability (`acPower` returns unknown without it).
A nil document means the daemon did not answer, not that the machine has no
capabilities, so everything is kept — the same posture `limits.FromDevice`
takes, and for the same reason.
