# The charger kind comes from `power_supply`, not `charge_mode`

**The charger kind comes from `power_supply`, not `charge_mode`**
(2026-10-09). `ReadChargerFromSupplies` needs no firmware call: a
`System`-scope USB supply `online` (ucsi's PD contract) means USB-C,
otherwise mains means the adapter. It is the default (`battery.charger`
omitted), and the Z13 uses it: checked live against `charge_mode` on both
inputs (adapter 1 → adapter; USB-C 2 → usb-c, ucsi port 2 `online=1` with
`[PD]`), so a battery poll no longer makes a live WMI read. The
`charge_mode` mapping below stays behind `charger = "asus-armoury"` for a
device where only the firmware can tell.
