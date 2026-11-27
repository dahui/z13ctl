# The drawer's limits come from `device-get`, with `DefaultLimits` as the fallback — and the two must stay interchangeable

**The drawer's limits come from `device-get`, with `DefaultLimits` as the
fallback — and the two must stay interchangeable.** `internal/limits` was
built for this swap and then sat unused for a release: the daemon shipped the
capability document at M2 while `gui.go` still hardcoded
`limits.DefaultLimits()`, so a release whose whole point was device
abstraction had a drawer that believed every machine was a Z13. The tell was
the package doc still saying "the daemon does not yet serve its limits over
the API" long after it did. `limits.FromDevice` is the one entry point;
`deviceLimits()` in `gui.go` is the only caller, and *any* failure —
no daemon, a pre-M2 daemon answering `unknown command`, a malformed reply —
falls back rather than erroring, because a drawer with slightly wrong bounds
is worth having and the daemon validates every write anyway.
**Fetched at startup and kept current, not fetched once** (2026-10-09). The
first version fetched the document once, before any widget existed, on the
belief that it was static and a later change would need a rebuild path. Both
premises were wrong. The document is not static: on asus-armoury the power
ranges are the kernel's, per power source, and a daemon restart can move them.
A drawer started against asus-nb-wmi kept PL1 at 5–93 W after the armoury
grant moved the daemon to 28–80 W. And no rebuild is needed: a slider's
bounds are a `SetRange` away. `internal/gui/limitsync.go` now refetches the
document on every daemon connection and lays get-state's live `tdp_limits`
over it on every refresh (`limits.WithTDPLimits`), and `applyLimits` moves
each built slider in place under `w.syncing`. Verified on hardware: a GUI
started with the daemon down logged `device limits changed tdpMin=28 tdpMax=80`
one second after the daemon came up, and the focus dump was byte-identical.
What it still cannot follow is a change in *which* controls exist; it logs
that and asks for a restart.
`TestDocumentMatchesTheDrawersFallback` (`internal/daemon`) is the guard that
makes the fallback safe: device TOML → driver envelope → wire → `FromDevice`
must land exactly on `DefaultLimits`, or the drawer behaves differently
depending on whether the daemon happened to answer. It lives in the daemon
package because that is the only place both halves are reachable —
`internal/limits` cannot import the daemon. It caught a real difference on
its first run: the envelope carries five PPT rails and the drawer compares
three, so `FromDevice` narrows the table to `PL1SPL`/`PL2SPPT`/`FPPT`.
Carrying the other two would cost twice — the fallback would stop being
interchangeable with the fetched value, and the guard would fire on a
difference that means nothing while saying nothing about the ones that do.
**Capability *absence* hides controls** (2026-11-15). `Limits` carries
`HasPower`/`HasFans`/`HasUndervolt`/`HasBattery` from the document's
sections; the numbers are still always complete (Sanitized fills them so no
arithmetic divides by zero) but no longer imply presence. `DefaultLimits`,
the answer when the daemon did not answer, has every capability. Without a
second real device the rules are tested against a synthetic one
(`internal/devicefixture`: one power limit, no fans, undervolt, lighting,
button or toggles), whose document `internal/daemon`'s
`TestSyntheticFixtureIsADocumentTheDaemonProduces` proves the daemon really
produces (regenerate with `VOLTAIRE_UPDATE_FIXTURE=1`). See the
device-genericity entry below for what each surface does with it.
(`Curve` becoming a slice and firmware profile names from `ProfileInfo` both
landed with the 2026-10-09 device-values audit.)
