# The Ally's rings are Aura over interface 2, and InputPlumber must leave it visible

**The ROG Xbox Ally X's joystick rings are driven with Aura over the
controller's HID interface 2, and where InputPlumber manages the controller
that interface needs InputPlumber's `passthrough`; until then the lighting is
unavailable, with that reason** (2026-11-26; `internal/aura` `Protocol`,
`internal/drivers/aurahid`, `hid.HiddenAuraNode`, the Ally device file).

**The protocol is device data.** The Ally takes G-Helper's Ally path
(seerge/g-helper `app/USB/Aura.cs`): the generic init without the Z13's
`0xC0 0x03 0x01` (G-Helper gates it on `IsZ13()`), brightness and modes as
**feature** reports, zone byte 0 for every ring, and not the Z13's `0xBD`
power message (G-Helper sends the Ally `5A D1 09 01`, which the rings were
shown not to need). `[lighting]` gained `report`, `dynamic_lighting_init` and
`power_flags`; their defaults are the Z13's, so its packets are unchanged.
Only interface 2 carries report ID `0x5D` (`02 5a 5d 5e`; interface 0 is
LampArray, `01`–`06`), so discovery already picks the right node. Verified on
the Ally through the daemon: static, breathe, cycle, strobe, a brightness-only
change (no effect re-send needed, unlike G-Helper's habit) and off, and the
effect put back 3 s after a 31.6 s suspend.

**Why interface 2 is hidden, and why voltaire does not unhide it.**
InputPlumber's `RogAlly` hidraw driver reads no input from interface 2 (it
advertises no capabilities); its only write there is the extended rumble
report. The node is hidden because InputPlumber hides every source it
manages, with runtime rules in `/run/udev/rules.d/` that set `MODE:="0000"`
(final, so no later rule can win), `chmod 000` and `setfacl -b` on every add
or change event. A chmod from voltaire would last until the next such event,
which is the tug-of-war decision 0032 rules out. InputPlumber 0.85 has no DBus
call to release a source (unlike intercept mode, decision 0177); the only
switch is `passthrough: true` on the source in its device config, which leaves
the node visible while InputPlumber keeps using it. The clean fix is upstream
(a request drafted for Jeff to post); until then setup writes the override,
but only when asked.

**Setup writes the override only on opt-in, and remakes it rather than
keeping it** (Jeff, 2026-11-26; `internal/drivers/inputplumber/override.go`,
`cmd/setup_inputplumber.go`). `voltaire setup --inputplumber-passthrough`
reads InputPlumber's installed config for this machine, inserts
`passthrough: true` on the hidraw source matching a lighting zone's
`usb`/`interface` from the device file, and writes it under the same name in
`/etc/inputplumber/devices.d/`. InputPlumber loads both directories with no
de-duplication, sorted by file name with `/etc` first
(`src/config/path.rs` `get_multidir_sorted_files`), so the same name is what
makes the override win. The rules behind it:
- *A copy kept goes stale*, hiding every later upstream change to that
  config: the old static udev rules' failure. So every setup run remakes it
  from the current upstream file, the boot pass included, and the boot unit
  gains `Before=inputplumber.service` while opted in so the fresh copy is in
  place before InputPlumber reads it. The boot pass never restarts
  InputPlumber (a restart queued behind a unit ordered after this one would
  wait on it); an interactive run does, before the grants.
- *The edit is one inserted line, then checked*: both files are parsed and
  must differ only by that key (`checkOnlyPassthrough`, negative control in
  its test). A config that cannot be edited cleanly gets no override, so the
  worst case is InputPlumber's own file: lighting hidden, controller
  untouched. An upstream config that already passes the source through gets
  none either, and an existing override is removed.
- *The opt-in is recorded by the file itself* (its first line is
  `inputplumber.OverrideMarker`): package upgrades run a plain `voltaire
  setup`, which keeps it; `=false` and `--remove` delete it. A same-name file
  without the marker is the administrator's and is never touched.
- *Only configs whose DMI `matches` fit this machine*, judged like
  InputPlumber (globs; no list matches everywhere); what DMI cannot judge (a
  udev match, CPU fields) counts as a match, since an unused override costs
  nothing. On the Xbox Ally X that is `50-rog_xbox_ally.yaml` alone, not the
  X20's.
- *Only lighting interfaces are ever passed through*; nothing else
  InputPlumber hides is setup's business.
Without the flag, setup prints a note naming it when InputPlumber hides this
machine's lighting. This replaces the earlier rule that voltaire never writes
InputPlumber's config, and only for this file.

**Hidden means unavailable, not broken.** `aurahid.New` returns a
`HiddenError` when a zone's Aura node is at mode 0000; the registry turns it
into `device.UnavailableError`, so the device assembles without lighting and
the reason reaches the daemon's WARN, device-get, every lighting refusal
(daemon `noLighting`, CLI `lightingUnavailable`) and the Dashboard's RGB band.
A node hidden after startup counts as absent (`present`), so the hotplug
watcher waits instead of retrying an open that cannot succeed. The daemon
assembles once, so the override counts after a restart; the note says so.

**The kernel's LED device is not a fallback.** `ally:rgb:joystick_rings`
takes four values clamped to 255 (`multi_max_intensity 255 255 255 255`,
`multi_index rgb rgb rgb rgb`); packed colours are clamped too, and on the
rings it showed only blue. A blue-only "static colour" would be a control that
cannot do what it says, so there is none.

The grant is interface 2 alone (`ENV{ID_USB_INTERFACE_NUM}=="02"`), never the
gamepad's interface 5. It also reaches the MCU's settings (remaps, mode,
vibration), the same order of trust the group has over power and fans; the
docs say so. Tests: `TestAllyProtocolSendsOnlyItsPackets`,
`TestHiddenZoneIsUnavailable`, `TestHiddenZoneIsNotPresent`,
`TestFeatureReportsGoThroughSetFeature`, `TestHiddenAuraNode`,
`TestUnavailableLightingIsRecorded`, the perms goldens and cross-device check;
for the override, `override_test.go` (against InputPlumber 0.85.0's two Ally
configs) and `TestSetupPassthroughOptIn`, `TestSetupRemoveTakesTheOverride`.
