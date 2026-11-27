# Battery flow is the one telemetry quantity whose zero is a reading

**Battery flow is the one telemetry quantity whose zero is a reading**, which
is why `api.TelemetrySample.BatteryPowerW` is a `*float64` while every other
field is a plain value with `omitempty`. A full pack on mains genuinely moves
no energy — the commonest state a laptop is in — so testing the value would
drop the chart from every plugged-in machine, and testing nothing at all
would draw one flat at zero on a desktop with no pack. That is the same
`*bool` reasoning `BatteryInfo.ChargeLimit` already carries. Inside the
daemon it is a value plus `Sample.BatteryPowerKnown` rather than a pointer,
because `driver.Sample` is stored in the history ring, which deep-copies in
both directions precisely so a driver cannot alias what it handed over.
The sign convention is positive = discharging, and `power_supply` does not
carry it: the magnitude is in `power_now` (or `current_now` × `voltage_now`
on a charge-reporting pack — the same both-forms split as battery health, and
the Z13 has only the energy form) while the direction is in `status`.
