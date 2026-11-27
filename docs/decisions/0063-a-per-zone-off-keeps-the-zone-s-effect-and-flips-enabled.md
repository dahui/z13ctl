# A per-zone `off` keeps the zone's effect and flips `Enabled`

**A per-zone `off` keeps the zone's effect and flips `Enabled`** (2026-11-15;
`.claude/plans/lighting-off-state-wipe.md`), as the all-device off always
did. Normalizing on read repaired mode and colour but could not invent the
zone's *own* effect, so `brightness --device` after an off came back on the
all-device effect, and `get-state` served the zeroed entry in-session —
brightness 0, the hardware's off level, which is how the drawer once re-enabled
a zone into darkness. A zone with no entry of its own keeps the all-device
state it was showing. `get-state` needs no normalization of its own: `off` was
the only writer of a partial entry, and old files are repaired at load.
The drawer and window now show a disabled zone's kept level, so picking an
effect after an off resumes it rather than jumping to the default
(`internal/lighting.ResolveBrightness`, 2026-11-15); a stored 0 still falls
back to the default, because z13ctl ≤1.4.2 zeroes the entry and
`brightness 0` stores a disabled 0 on every version. `lighting_off_test.go` drives the handlers over a fake driver; its
negative control (the old line restored) fails all four cases. The global
off's `Devices = nil` is deliberate and stays: keeping per-zone entries
disabled there would leave them off after `brightness <level>` (no
`--device`) turned the hardware back on.
