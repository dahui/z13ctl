# Never call EVIOCGRAB on the button device

**Never call EVIOCGRAB on the button device** (issue #10). The "Asus WMI hotkeys"
node carries `SW_TABLET_MODE` as well as KEY_PROG3 (`capabilities/sw = 2` on the
Z13). Grabbing it exclusively — which z13ctl did through v1.2.0 — takes the
tablet-mode transitions away from libinput, so attaching the detachable cover
after login leaves the desktop in tablet mode and the cover keyboard dead until
the session restarts. The watcher reads shared; evdev delivers to every
non-exclusive reader. This is enforced structurally: evdevkey's `runLoop` takes an
`eventDevice` interface that deliberately has no `Grab` method, so reintroducing
the grab is a compile error. Trade-off: the keypress also reaches the desktop,
and a foreign exclusive grab now fails silently rather than logging EBUSY.
