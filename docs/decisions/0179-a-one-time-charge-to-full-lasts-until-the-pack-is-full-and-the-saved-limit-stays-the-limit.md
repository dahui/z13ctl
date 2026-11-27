# A one-time charge to full lasts until the pack is full, and the saved limit stays the limit

**`batterylimit-full-once` lifts the charge limit to the device's maximum until
the battery is full, then writes the saved limit back; `state.Battery` stays
the user's limit throughout and `state.ChargeOnce` says the hardware holds the
maximum instead** (2026-11-26; `internal/daemon/chargeonce.go`,
`batterylimit --full-once`, the switch under the window's charge-limit slider;
in the drawer it is the opt-in `charge-once` control, off by default because it
is not used often enough to earn drawer space, Jeff).

**Until full, not until unplugged.** G-Helper's `charge_full`
(`app/Battery/BatteryControl.cs`) re-applies 100 on every power event while
the flag is set and clears it only when capacity passes 99 %, so a charge cut
short by moving the machine carries on at the next plug-in. That is the
feature's point (a full battery for a day out), and ending it on unplug would
make it fail exactly when the user tops up in two goes. Setting a limit ends
it, as G-Helper's slider does: the user has just said what the limit is.

**`state.Battery` is never 100 because of it.** Keeping the user's limit in
`battery_limit` means every client keeps showing the limit the charge returns
to (the slider does not jump to 100 and back), and the startup restore and the
end of the charge are one rule: the maximum while `ChargeOnce` is set,
`state.Battery` otherwise. Started with no limit saved, the daemon adopts the
hardware's (another tool's) as the one to return to; with none at all it
refuses, since the battery charges to full already.

**Full** is capacity 100, or a `full` status, or 99 % and `not-charging` on
mains (firmware that tops off a point short). The watcher checks once a minute
(a charge takes an hour) and stands down while suspending and while the EC is
wedged, like every path into the EC (0113, 0114). A failed restore keeps the
flag, so the next tick retries rather than leaving the limit lifted silently.

**The startup restore had to learn the Gaming Mode yield.** The yield turns
`BatteryCaps.ChargeLimit` off but keeps the driver, whose `SetChargeLimit`
does not check it, so the restore (which tested only for a driver) would have
written a desktop limit over Steam's at every Gaming Mode start. It and every
charge-limit path now go through `Daemon.chargeLimitControl`, nil without the
capability. Tests: `chargeonce_test.go` (negative controls run for the yield
check and for a new limit cancelling the charge).
