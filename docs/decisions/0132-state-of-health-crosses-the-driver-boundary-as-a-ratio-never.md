# State of health crosses the driver boundary as a ratio, never as the pair it is computed from

**State of health crosses the driver boundary as a ratio, never as the pair
it is computed from.** `power_supply` publishes either
`charge_full`/`charge_full_design` in µAh or `energy_full`/`energy_full_design`
in µWh depending on the battery driver — and the Z13's ACPI battery reports
*energy*, where `driver.BatteryStatus`'s first draft assumed charge and named
its fields `ChargeFull`/`ChargeFullDesign`. Only the ratio means the same
thing on every machine, and it is the form a dashboard shows anyway, so
`HealthPercent` is what the interface carries and
`linux.ReadBatteryHealthPercent` tries both pairs. It is deliberately **not
clamped to 100**: a freshly calibrated pack genuinely reads above its design
capacity, and 103% is more useful than a number quietly adjusted to look
plausible. The read is best-effort inside `Status()` for the same reason RPM
is inside `Sample()` — a missing full-charge attribute must not make the
charge level unreadable — and it is gated on the declared capability, so a
device whose data does not claim health reports zero even where the
attributes happen to exist.
