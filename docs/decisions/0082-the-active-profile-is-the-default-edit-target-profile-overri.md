# The active profile is the default edit target; `--profile` overrides it

**The active profile is the default edit target; `--profile` overrides it.**
`fancurve|tdp|undervolt --set` with no `--profile` edits the profile you are
running, creating and activating `custom` when a firmware profile is active.
There is no working slot and no save step: the edit lands in the profile and
persists immediately. `--profile <name>` edits a profile that is *not* running,
stores only, and writes no hardware. That is not a convenience — autoswitch is
unusable without it, since configuring the battery profile would otherwise mean
applying it first. `resolveEditTargetLocked` + `commitEditLocked`
(`internal/daemon/profile.go`) are the single place that resolves and commits.
