# Controller vibration strength is a daemon setting like CPU boost

**Controller vibration strength is a daemon setting like CPU boost**
(`driver.Rumble`, `internal/drivers/hidoxp`, `[rumble]` in the device file,
`rumble`/`rumble-get`, `State.Rumble`, device-get `rumble {min,max}`).
Checked first, because hid-oxp's RGB writes are ignored by the X2: 5, 1, 5, 1
written between identical FF rumble effects (python EVIOCSFF on InputPlumber's
virtual pad, as root) were felt as strong, weak, strong, weak (Jeff). The
range is the kernel's `rumble_intensity_range`; device-get offers the
section only while the controller answers. The stored value is replayed at
startup and resume, only when set and only when the controller lost it
(`restoreRumble`). Grant: udev on `ACTION=="bind", SUBSYSTEM=="hid",
DRIVER=="hid-oxp"` (the attribute appears at bind, not add), the perms-unit
glob over the driver directory, and setup's direct grant; drift guard
updated. Verified with `udevadm test --action=bind` on the OXP: the rule
matched the controller's vendor interface and resolved to its attribute; a
hid-multitouch device matched nothing. hid-oxp binds three of the
controller's HID devices and only one has the attribute, so the loop is
`|| continue` (`TestRumbleRuleToleratesTheOtherInterfaces`, negative
control run), or udev logs a failed RUN for the other two at every boot.
GUI: a `rumbleSection` built twice, the Settings tab's CONTROLLER card and a
`rumble` drawer control (CONTROLLER heading, `CapRumble`, in the OXP's device
default), rules in `settingsui/rumble.go`. After `sudo voltaire setup` the
daemon set 4 then 5 on the OXP. **The OXP's default drawer is Jeff's own
arrangement** (2026-11-17): tdp, cpu-boost, brightness, volume, rumble,
refresh-rate, with lighting listed for when the rings work; the Saved
profile picker and autoswitch are opt-in there.
