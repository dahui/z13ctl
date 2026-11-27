# A kernel interface that exists but does not work is declared off, and turned on by kernel version

**A kernel interface that exists but does not work is declared off, and
turned on by kernel version** (the OXP's charge limit, 2026-11-17/18;
`battery.charge_limit = false` in its device file, whose comment carries the
evidence). oxpec offers `charge_control_end_threshold` and
`charge_behaviour` on the X2 Mini Pro and both read back whatever is
written, but the driver writes the OneXPlayer X1's EC registers
(0xA3/0xA4) while this EC reads the APEX's (0xE5/0xE6): the pack charged
to 96 % under a 62 % limit. **A read-back is not evidence a setting
holds**: the earlier steamos-manager check in Jeff's OXP repo only read the
value back, which is how the fault went unnoticed. The capability
is therefore *off*, not unavailable: nothing is missing that a user could
install, and a control that writes a value the hardware ignores is worse
than none. It comes on gated by kernel version once a release carries the
fix (sent to platform-driver-x86 2026-11-17), because sysfs reads the same
under either driver and a functional probe would have to stop the user's
charging to learn anything. That gate does not exist in the device schema
yet; it lands with the fix. **How to test a charge limit on an EC like
this one:** the pack does not start charging again until it is below 90 %
(firmware hysteresis), and `status` reads "Charging" whether current flows
or not, so drain below 90 % first and watch `power_now` (≈42 W charging,
0 when held). UPower reads the threshold once at device add, so after a
module swap KDE shows a stale limit until `upower` restarts. Same result on
BIOS 0.22 and 0.25.
