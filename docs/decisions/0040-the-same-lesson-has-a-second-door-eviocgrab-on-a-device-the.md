# The same lesson has a second door: `EVIOCGRAB` on a device the drawer only *thinks* is a controller's

**The same lesson has a second door: `EVIOCGRAB` on a device the drawer only
*thinks* is a controller's.** The GUI's gamepad reader grabs a PlayStation
controller's own touchpad so it does not act as a mouse behind the drawer, and
its test for "is this a controller touchpad" was `ABS_MT_POSITION_X` and no
gamepad buttons — which the Z13's own touchpad (`0b05:1a30`) and touchscreen
(`04f3:43c7`) answer just as well. Opening the drawer therefore took both away
from the compositor for as long as it was open, so touch input died
system-wide; the stylus survived only because it reports pressure and tilt
rather than MT slots, and `Esc` was the way out. Same failure as issue #10 —
grabbing a device the rest of the desktop is using — reached from the GUI
instead of the daemon, and the daemon's structural defence (an interface with
no `Grab` method) does not transfer, because this reader legitimately grabs.
A multitouch device now has to be shown to *belong to* a controller already
tracked (`sameController`: `vendor:product`, refined by EVIOCGUNIQ or the
EVIOCGPHYS root), and `INPUT_PROP_DIRECT` rules a touchscreen out ahead of any
matching. `scan()` is two-pass for that reason: `/dev/input/event*` enumerates
in node order and a controller's touchpad routinely appears *before* its
gamepad, so a one-pass check could never see the sibling it needs.
**The load-bearing part is that classification is now pure and tested.** It
previously took an open `*evdev.InputDevice`, so it could not be exercised
without the hardware in hand — which is exactly how an unqualified multitouch
test survived to a release. `classify(deviceInfo, []deviceInfo)` is a pure
function over identity and capabilities, and
`internal/gui/gamepad/gamepad_test.go` drives it from the real capability sets
of the devices involved. The case that matters most has its own entry:
attaching a controller must not make the machine's own devices grabbable
again. Ported from z13gui (its issue #18, fixed in 1.4.1); no voltaire release
ever shipped the fault.
