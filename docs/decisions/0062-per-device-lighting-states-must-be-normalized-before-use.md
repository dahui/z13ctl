# Per-device lighting states must be normalized before use

**Per-device lighting states must be normalized before use.** `handleOff`
saved a named zone as `{Enabled: false}` with no mode/colour/speed (until
2.0 — see the next entry), so
`handleBrightness` reusing that entry produced an *enabled* state with empty
fields — and `ModeFromString("")` is an error, so every later restore failed
(daemon start, resume, hotplug). Worse, `applyLightingState` returned on the
first zone error, so a broken keyboard entry also left the lightbar dark.
`normalizeLightingState(ls, fallback)` fills gaps from the all-device state
then `defaultState()`, and is applied on **both** the write path
(`handleBrightness`) and the read path (`applyLightingState`) — the read side
is what repairs the state files users already have. `applyLightingState` now
continues past a failing zone and returns the first error.
