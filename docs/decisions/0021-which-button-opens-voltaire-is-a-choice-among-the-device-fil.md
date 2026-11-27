# Which button opens voltaire is a choice among the device file's buttons, never a key learned from a keyboard

**Which button opens voltaire is a choice among the device file's buttons,
never a key learned from a keyboard** (Jeff, 2026-11-15;
`.claude/plans/hardware-button-config.md`). Other devices have other buttons
(the Ally's arrive as a vendor HID report, the OXP's as key chords on its AT
keyboard) and a user may want a different one, so `[button]` became
`[[buttons]]`: each entry has an `id`, a `label`, a method (`evdev-key`,
`evdev-chord`, `hidraw-report`) and at most one is `default`. The daemon
watches all of them (one merged watcher, each press carrying its `ButtonEvent.ID`),
stores the user's choice as `State.DrawerButton` (`button-set`; `none` and
`default` too), and runs only the chosen button's presses through
`pressTick`. get-state reports the choice resolved; device-get lists
`hardware_buttons`. Events stay payload-free and mean what they always did,
so Decky and 1.x clients are unaffected. The GUI's Settings card has an
**Open voltaire with** dropdown, and the CLI `voltaire button`.
**This is the G-Helper model, and the alternatives were measured first.**
G-Helper binds a fixed list of named vendor keys to actions from dropdowns;
its one arbitrary key is a global hotkey set by hand in `config.json` and
registered with the OS (`RegisterHotKey`). HHD uses fixed lists too; on the
OXP it runs as root, EVIOCGRABs the AT keyboard and re-emits only volume.
The XDG GlobalShortcuts portal was spiked and works from a systemd service
(`host.portal.Registry.Register` needs a desktop file named after the app
id, then `CreateSession` succeeds), but binding goes through a KDE dialog
asking the user to assign keys, which Jeff judged too much for casual users.
It remains a possible later option; nothing of it shipped. **List only
buttons captured on the machine (evtest) that do nothing else there**: the
Z13's hotkey node advertises 38 keys (asus-wmi's keymap for every ASUS
model), the kernel acts on some itself (Fn+F5), and since nothing grabs, a
key with a job would do both. The Z13 lists only the Armoury Crate button
until a capture session finds more. A chord requires *every* key held, not
HHD's one distinguishing key, which would fire on that key typed alone.
