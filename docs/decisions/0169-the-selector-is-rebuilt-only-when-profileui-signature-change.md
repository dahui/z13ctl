# The selector is rebuilt only when `profileui.Signature` changes

**The selector is rebuilt only when `profileui.Signature` changes**;
highlights, sensitivity and tooltips move on the existing widgets otherwise,
so a background state refresh cannot tear buttons out from under the
pointer. Selecting a profile in it only re-targets the editor —
**`Activate` is what applies it** — and every refusal reason a tooltip shows
comes from `profileui` (`ActivateBlock`, `DeleteBlockFor`, `SaveAsBlock`,
`CreateNameProblem`), mirroring the daemon's own text so the tooltip and the
error agree. Delete sits at the bottom of the editor with a two-tap arm
rather than a confirm dialog, for the same no-popup reason.
