# What the OXP's captures changed in the button drivers

**What the OXP's captures changed in the button drivers** (2026-11-17).
`hidraw-report` matches `match = [[offset, value], …]`, all of which must
hold: the OXP's Home sends a press frame and a release frame that differ only
in a state byte, and the old single `byte`/`value` read every tap as a double
press (`TestOXPHomeReleaseIsNotAPress`). Evdev buttons may name `usb` +
`interface`: the OXP controller registers `HID 1a86:fe00` on interfaces 0
*and* 1, sysfs lists 1 first, and only 0 carries the chords; the watcher
matches the node's USB identity and the udev grant adds
`ENV{ID_VENDOR_ID}`/`ENV{ID_MODEL_ID}`/`ENV{ID_USB_INTERFACE_NUM}` — ENV,
because `ATTRS{idVendor}` lives on a different parent from `ATTRS{name}` and
the rule would match nothing (the lighting lesson again). Checked with
`udevadm test` against a throwaway /run rule: event13 and hidraw4 matched,
event10, the AT keyboard and the other hidraw interfaces did not.
