# A button InputPlumber manages reaches voltaire through InputPlumber's profile, loaded over DBus, never a file

**A button InputPlumber manages reaches voltaire through InputPlumber's
profile, loaded over DBus, never a file** (Jeff, 2026-11-16/17;
`.claude/plans/inputplumber-buttons.md`, built and verified on the OXP X2
Mini Pro 2026-11-17). InputPlumber EVIOCGRABs the evdev nodes of a device it
manages and sets them and its hidraw nodes to 0000 (udev `MODE:=` + chmod),
so no direct watcher can coexist. A button's device data names its
InputPlumber capability (`inputplumber = "QuickAccess2"`); the daemon's
route watcher (`internal/daemon/inputplumber.go`) finds the composite device
whose `SourceDevicePaths` hold the chosen button's node (`ButtonInfo.Node`,
the direct driver's sysfs lookup), and merges one mapping — capability →
`dbus: voltaire_<id>` — into its running profile (`GetProfileYaml` →
`inputplumber.SetMapping` → `LoadProfileFromYaml`; wheel/inputplumber polkit).
A mapping already on that capability is **replaced, not joined** (both would
fire: the OXP's Keyboard button would also screenshot) and kept in voltaire's
mapping's `name` in YAML flow style, so removal restores it even after a
daemon restart. Re-applied on InputPlumber's NameOwnerChanged and any
PropertiesChanged under its tree (profile loaded, composite added — it has no
ObjectManager), a button-set, and a 30 s backstop; a pass that finds the
mapping in place loads nothing, so it cannot fight. Removed on button-set to
another button or none, and on shutdown (`Run` waits up to 2 s). The first
pass after start also clears a mapping a crashed daemon left on any device.
**One press, one route**: `ButtonEvent.Via` marks InputPlumber presses and
`pressRoute` counts them only while InputPlumber carries the chosen button,
direct presses only while it does not — a hidraw fd opened before
InputPlumber hid the node keeps delivering (hidraw has no grab), and
pressTick's 50 ms floor still toggles. Presses are accepted only from the
current owner of `org.shadowblip.InputPlumber`, so another local process
cannot fake one. Verified: Keyboard single/double through InputPlumber;
moving between buttons, none, and back; another client's `LoadProfilePath`
and an InputPlumber restart both re-mapped; stopping InputPlumber flipped to
the direct route (nodes reappear) and starting it flipped back; stopping the
daemon restored Screenshot. Rejected alternatives, checked: an
`/etc/inputplumber` override (whole files only, no profiles directory);
watching the virtual pad (the default sends QuickAccess as Guide+A, so it is
indistinguishable from a user's chord, and Steam acts on it too).
