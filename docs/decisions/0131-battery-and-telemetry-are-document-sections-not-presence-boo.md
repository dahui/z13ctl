# `battery` and `telemetry` are document *sections*, not presence bools, because their contents are independently absent

**`battery` and `telemetry` are document *sections*, not presence bools,
because their contents are independently absent.** A machine can report state
of health while exposing no charge-limit attribute, and the reverse — a
single `battery: true` cannot say either, so a client rendering from it
guesses. `BatteryConfig.ChargeLimit` is a `*bool` for the same reason absent
and false must differ: every battery driver written so far exists to control
the threshold, so a bare block means the usual thing and a device that only
reads a level has to say so. A block declaring neither is a validation error
("drop the block instead"), matching the empty-`toggles` rule.
