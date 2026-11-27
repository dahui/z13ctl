# `charge_mode` was the only thing that could tell the Z13's two power inputs apart, and its vocabulary was established by swapping the charger — not by inference

**`charge_mode` was the only thing that could tell the Z13's two power inputs
apart, and its vocabulary was established by swapping the charger — not by
inference.** The machine takes power two ways: the proprietary high-wattage DC
adapter shared with the Zephyrus line, and USB-C PD. **The Mains supply reads
`online=1` for both**, which is correct — mains power is attached either way —
and is exactly why `OnACPower` cannot answer this. `OnAC` says "is it plugged
in"; `charge_mode` says "into what", and the two inputs have very different
ceilings.
Observed, with the cable physically changed (Jeff, 2026-08-14): on the DC
adapter `charge_mode = 1` with both `ucsi-source-psy-*` offline; on USB-C
`charge_mode = 2` with a ucsi port `online=1` and its `usb_type` showing
`[PD]` active. `0` is documented by `possible_values` and is the only
remaining case, but has **not** been observed — a machine with no charger is
on battery, where nobody is watching this attribute. An unrecognised value
reports `ChargerUnknown` rather than a guess.
I first deferred this on the stated grounds that "the Z13 has no barrel jack",
which is **wrong** — the premise, not the reasoning, was the error, and it
inverted the conclusion: `1` on mains is *consistent* with the standard
asus-wmi mapping rather than contradicting it. Worth remembering as the shape
of the mistake: a confident hardware claim, used to reject the obvious reading
of an attribute.
**The negotiated PD wattage is not available.** `current_max`/`voltage_max`
read 0 on both ucsi ports even with PD live; only `current_now` (5 A) is
populated, and without a voltage there are no watts. So a charger-class rule
can distinguish adapter from USB-C but *not* a 65 W brick from a 100 W one.
`profileui.ChargerLabel` is deliberately empty for `none` and for an unknown
kind: `PowerLabel` already says the machine is on battery, and naming a
charger wrongly is worse than saying nothing. It is rendered *beside*
`BatteryStatus` rather than inside it, because that line already drops its own
state word to stay legible.
