# `BuildThemeCSS` defines every token twice — `@z13-*` and `@voltaire-*` — which is what let the token rename be *staged*

**`BuildThemeCSS` defines every token twice — `@z13-*` and `@voltaire-*` —
which is what let the token rename be *staged*.** The generated
`@define-color` block is prepended to the bundled `theme-default.css`.
Emitting only the new names while that sheet still referenced the old ones
would have dropped every rule using them and left the drawer unstyled, so
defines and references could not flip in the same commit: both names first,
bundled sheet second. **The sheet is migrated** — it defines and references
`@voltaire-*` throughout — so the `@z13-*` aliases now have no in-tree
consumer at all and 3.0's removal is a pure deletion.
`TestLegacyAliasesAreStillEmitted` is what makes that deletion deliberate
rather than accidental, since nothing else would notice them going.
Note the asymmetry that makes the whole thing safe: a user's `theme.css` is
loaded **verbatim** (it supplies its own defines and never sees ours), while
a `theme.toml` is substituted into the bundled template — so the alias only
ever affected the template path, and a hand-written sheet on the old names
keeps working because it never depended on ours.
