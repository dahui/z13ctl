# The battery chart plots state of charge; the flow feeds the card header's rate and time estimate

**The battery chart plots state of charge; the flow feeds the card header's
rate and time estimate** (Jeff, 2026-08-14: the percentage is what you glance
at a battery chart for). `Sample.BatteryLevelPct`/`Known` →
`TelemetrySample.BatteryLevelPct *int` (zero is a reading — a flat pack — so
the pointer, exactly the flow's reasoning) is the series, on a hard 0–100
frame like Load's; `BatteryPowerW` stays on the wire untouched but no longer
charts, because watts and percent cannot share an axis. The estimate is
client-side and pure (`profileui.batteryEstimate`): get-state gains
`battery_energy_wh`/`battery_energy_full_wh` (via `driver.BatteryStatus` —
watt-hours on every machine, the driver converting a charge-reporting pack
through `voltage_now`; a *pair*, since remaining without the pack size
answers only the discharge half), and the header divides — remaining over
rate to empty while discharging, gap-to-target over rate while charging,
where the target is the charge *limit* when one is set because the pack
genuinely stops there ("to full" would promise what the firmware prevents).
Two suppressions are deliberate: rates under `minEstimateW` (a resting pack
wobbles by tenths of a watt, and dividing by that claims false precision) and
answers beyond a day (noise the floor did not catch) fall back to the plain
state word. The estimate is instantaneous — remaining over the *current*
rate — not smoothed; if that ever wants smoothing, the history ring is where
the average lives, not a second counter in a handler.
