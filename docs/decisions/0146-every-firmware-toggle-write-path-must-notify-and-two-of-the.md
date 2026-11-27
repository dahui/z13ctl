# Every firmware-toggle write path must notify, and two of the three did not

**Every firmware-toggle write path must notify, and two of the three did
not.** `handlePanelOverdrive` had updated state and called `saveAndNotify`
since it was written; `handleBootSound` and the generic `handleFeature` did
neither, so a toggle changed by any other client left every open UI showing
the old position. That was invisible while the only renderer was the drawer's
bottom bar, which resyncs whenever the drawer opens, and became visible the
moment a settings page rendered rows from `State.Features`: the same switch
updated live or did not, depending on which command wrote it. All three now
go through `notifyToggleChanged`, whose *notify* is the load-bearing half —
values reach clients through `get-state`'s live reads, so a client that
re-reads sees truth; what it had no way to learn was that there was anything
to re-read. The state write beside it is only the pre-2.0 named-field
projection, kept so the saved file does not disagree with the machine.
`TestEveryToggleWritePathNotifies` is a source check because the alternative
is a hardware write, and **its first version passed with the call deleted**:
each of those functions *mentions* `notifyToggleChanged` in a comment, so the
check was reading prose and reporting it as code. `funcBody` strips comments
now; the negative control is the only thing that caught it, and is the reason
any source-shaped guard needs one.
