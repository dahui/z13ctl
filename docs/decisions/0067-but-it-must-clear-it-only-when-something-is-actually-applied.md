# …but it must clear it only when something is actually applied: `d.uvApplied()`, never availability alone

**…but it must clear it only when something is actually applied:
`d.uvApplied()`, never availability alone.** Availability answers "is
the module loaded and does this fork support CO on this platform" — a property
of the *machine*, true on every Z13 with ryzen_smu installed. Gating a reset on
it meant every route to a stock profile sent a live MP1 mailbox write to clear
an offset that was never set. On 2026-08-14 one of those hard-locked the SoC:
total freeze, power cycle, no kernel output, no pstore record, while state
showed profile `balanced` with no undervolt saved or active anywhere. The
mechanism is the known ryzenadj-class hang — a userspace MP1 message colliding
with the kernel's own PMFW traffic — and the onexplayer work records the
aggravating factor: *"the driver shares one argument buffer across mailboxes,
so concurrent commands corrupt each other's arguments"*. A colliding command is
not merely wasted.
`Undervolt.Active` is the closest thing to a readback that exists, because CO
has none, and it is trustworthy for exactly the reason the entry above gives:
it is set in one place, after a successful write. `undervoltActive()` walks
precisely what `setUndervoltActive` stamps — they are the read and the write of
one fact, and a getter reading a different field would drift silently.
**The trade is real and deliberate**: an offset present in hardware but absent
from state (lost state file, `ryzenadj` by hand) now survives a reset that
should have cleared it. That is recoverable — CO is volatile, so a reboot or
suspend clears it and the daemon re-applies from state on resume — while the
hang is not. `handleUndervoltReset` succeeds rather than erroring when nothing
is applied: "no offset applied" is already the requested end state, and the
stored value, which is the half the user can observe, is still cleared.
`uvApplied` reads `Active` *before* calling `uvProbe`, since the first probe
in a process is itself the reset this gate exists to avoid.
`internal/daemon/undervolt_gate_test.go` is the guard. It is source-shaped, so
it strips comments before matching — every one of these call sites *mentions*
`uvAvailable` in prose — and its negative control was run: reverting any single
site to `uvAvailable()` fails it. `undervolt_reset_test.go` checks the same
rule by behaviour, counting the probes and resets `handleUndervoltReset`
sends across seven cases (nothing applied ⇒ zero, a stored target while
another profile's offset is live ⇒ zero). Both negative controls were run:
gating the reset on `uvAvailable()` and opening the handler with the probe
each fail it.
