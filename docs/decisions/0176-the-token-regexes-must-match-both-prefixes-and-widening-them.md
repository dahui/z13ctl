# The token regexes must match *both* prefixes, and widening them was the load-bearing half of the migration

**The token regexes must match *both* prefixes, and widening them was the
load-bearing half of the migration.** `definePattern` and `referencePattern`
were `z13-`-only, which was correct while that was the only name and would
have silently ended the self-containedness guard the moment the sheet moved:
a `@voltaire-*` token referenced but not defined would have matched nothing,
and `UndefinedColorTokens` would have reported a clean stylesheet. That is
precisely the bug the guard exists for — the template shipped for months
referencing `@z13-error` without defining it — reintroduced by making its own
regex stop seeing the names in use. Widening them found the real gap
immediately (the migrated sheet defined none of what it now referenced).
**There is no runtime signal to fall back on.** GTK4 drops a rule with an
unresolvable colour without logging anything — checked by breaking a token
deliberately and watching a full `--debug` run stay silent — so the symptom
is a wrong-coloured border, not an error. `TestTheComposedSheetHasNoUndefinedTokens`
therefore checks the sheet the app actually loads (defines plus the
stripped template), because the two halves each passing their own test is not
the same as the composition being sound.
