# Lighting's hidraw grant is one interface; it was every interface of the device until 2026-11-15, the cover's typing interface included

**Lighting's hidraw grant is one interface; it was every interface of the
device until 2026-11-15, the cover's typing interface included.** The rule
matched `idVendor`/`idProduct` alone, so all five hidraw nodes of the cover
(`0b05:1a30`) were group `users` read-write since z13ctl's initial commit:
interface 2 is Generic Desktop / Keyboard (raw keystrokes) and 3 the
touchpad. Only interface 0 carries the Aura report (`0x5d`) on both devices,
and the driver writes only there. Lighting zones now require `interface`,
matched as **`ENV{ID_USB_INTERFACE_NUM}`** (udev's `usb_id` sets it on the
hidraw node), and setup's `revokeStaleHidrawGrants` resets a
lighting device's other interfaces that still carry setup's group to
`root:root 0600`, because udev leaves a node's permissions alone once no rule
sets them — re-running setup otherwise left the hole open until reboot.
**The first version matched `ATTRS{bInterfaceNumber}` and shipped broken for
a night**: every `ATTRS` key in one rule must match on the *same* parent, and
`bInterfaceNumber` is on the USB interface while `idVendor`/`idProduct` are
on the USB device above it, so the rule matched nothing. It looked verified
because the lighting nodes kept the grant an older setup had given them; the
next boot recreated them root-only, and the keyboard came up dark with
"no ASUS Aura devices found". **A permission rule is verified with `udevadm
test --action=add` against the rule as written, or after the node is
recreated — never by `stat` on a node that may still carry an older grant.**
The ENV form was proven by dry run (matches interface 00 of both devices,
nothing else; the ATTRS control matched nothing) and then through the real
udev trigger on a freshly booted machine, where the daemon's hotplug watcher
reopened both zones within two seconds. `TestHidrawGrantsNameAnInterface`
now also refuses `ATTRS{bInterfaceNumber}`; its negative control was run. The
daemon's permission error used to suggest the vendor-only rule this
replaced, and now names `voltaire setup` (`hid.grantHint`).
**The same fix still has to be ported to main as a z13ctl 1.4.4** (Open
work), in the ENV form.
