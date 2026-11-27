# Resets land on `[profiles] default`, never a literal `"balanced"`

**Resets land on `[profiles] default`, never a literal `"balanced"`.**
`Config.Validate` requires the default to be listed, with a stock row whose
PL1 is within `tdp_max_safe`: `resetToStock`'s lower-before-release order is
fail-closed only if that holds. With no default the driver falls back to
`balanced` if the kernel offers it, else `""`. `resetToStock` and
`runTdpResetDirect` resolve it *before* touching anything and refuse on
`""` — the old order cleared the undervolt and then failed the switch.
