# The drawer's edge is `panelgeom.Edge`, and every anchor and margin write goes through one accessor

**The drawer's edge is `panelgeom.Edge`, and every anchor and margin write
goes through one accessor.** `gui.toml`'s `[quickbar] edge` moves the drawer
between the left and right screen edges; all three backends take it, and the
geometry is pure — `Panel` aligns the rect, `HiddenX` says which way "away"
is, `HasNeighbor` answers whether sliding off that edge would bleed onto
another output. That last one is why this needed care rather than a flag:
the layer-shell backend fades in place instead of sliding when a monitor sits
beyond the edge (KWin does not clip a layer surface's overflow), and the check
asked exclusively about the *right* until the drawer could move. An assumption
like that survives a move silently and produces a drawer bleeding onto the
monitor it used to be nowhere near. `shellEdge()` exists so there is no path
that relocates the panel while leaving the animation driving the opposite
side; gamescope has no rectangle at all, so its edge is the append order of
backdrop and panel.
**Top and bottom are refused with a message that says why.** They are not an
anchor change — the drawer is a fixed-width column of stacked sections, so a
horizontal edge means laying every section out along the other axis, which is
a different panel rather than a moved one. `focusgrid.Horizontal` is already
written and tested for the day that lands; until then `ParseEdge` names the
reason instead of answering "unknown edge", and every parse failure still
returns a usable edge so a bad config costs a warning and not a drawer that
will not open.
