# A battery flow figure is meaningless without the pack's state beside it, and shipping the number alone was reported as a broken sensor

**A battery flow figure is meaningless without the pack's state beside it,
and shipping the number alone was reported as a broken sensor.** On a machine
with a charge limit the commonest reading is the confusing one: a pack resting
*above* its end threshold on mains is neither charging (it is over the limit)
nor discharging (mains is attached), so `power_now` is exactly 0 — correct,
and indistinguishable from a dead reading. Measured on this machine: limit 74,
level 81, `status` "Not charging", and UPower independently agreeing at
`energy-rate: 0 W`, `state: pending-charge`. `get-state` was already reading
`BatteryStatus.Capacity` on every request and **throwing it away**, so no
client could show the level either. `battery_level` and `battery_state` close
it; `battery_limit` remains the *setting* and is a different number from
`battery_level`, the reading.
`not-charging` is deliberately **not** folded into `full` even though both
mean no flow — the pack is not full, it is being held back, and that is the
entire explanation for the zero. `mapBatteryState` is the one place that knows
power_supply's `status` vocabulary, so the wire state and `signedByStatus`'s
sign convention cannot drift.
**`batteryStateIn` also returns whether `status` could be *read*, which is not
the same as reading it and finding "Unknown".** Both are `BatteryStateUnknown`
to a client, but an unreadable file leaves `signedByStatus` guessing
"discharging" (the direction that matters, on a machine running off the pack)
while an explicit answer means no flow. Merging the two during this refactor
silently changed what a flow reading meant, and `TestReadBatteryPowerW` caught
it.
