# The editor addresses its target through `profileui.PlanEdit`, and every stored-target send is preceded by `probeStoredTarget`

**The editor addresses its target through `profileui.PlanEdit`, and every
stored-target send is preceded by `probeStoredTarget`.** A plan is live —
bare sends, hardware applied — in exactly two cases: the target is the
active profile, or it is "custom" while a firmware profile is active (the
drawer's historical create-and-activate flow, kept byte-identical for old
daemons). Everything else goes through the `...For` variants and stores
only; the probe (SendProfileList, exactly `cmd.ensureProfileTargetSupported`'s
trick) is what keeps a pre-1.3 daemon from silently applying the edit to
the running machine behind an `ok`. The plan is resolved per operation, not
stored — the active profile can move underneath an open editor. The fan
floor is evaluated against `editorFloorPL1` (live PL1 for a live target,
the profile's *own* saved TDP for a stored one — hardware says nothing
about a profile that is not running), and `ForEditor`/`CurveToShow` decide
what the widgets display, including that a stored curve is adopted on
presence while a live one requires `FanCurveIsCustom`.
