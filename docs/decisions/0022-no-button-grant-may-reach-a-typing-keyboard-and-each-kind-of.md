# No button grant may reach a typing keyboard, and each kind of node has two guards

**No button grant may reach a typing keyboard, and each kind of node has two
guards** (2026-11-15). Read access to a keyboard is read access to every
keystroke. **evdev:** the udev grant carries `ENV{ID_INPUT_KEYBOARD}!="1"`
(udev's own `input_id` classification; "Asus WMI hotkeys" has only
`ID_INPUT_KEY`) and is read-only `0640` — a writable evdev node accepts
injected events, and every release before this granted `0660`; the watcher
opens read-only and applies the same test to the node's `capabilities/key`
bitmap before opening (`evdevkey.IsTypingKeyboard`, fail-closed on an
unreadable bitmap), and setup's direct grant uses that test too, with
`g=r` so an old write bit is dropped. A device whose button chords arrive on
a keyboard node (the OXP's AT keyboard) opts it in with `keyboard_node =
"<why>"`, and its grant is then written scoped to the file's DMI match
(`ATTR{[dmi/id]sys_vendor}`, `ATTR{[dmi/id]product_name}`), with glob
characters refused in that match. **hidraw:** a `hidraw-report` button names
one `interface`, the watcher refuses a node whose report descriptor has a
top-level Keyboard/Keypad collection (`hidrawbtn.IsKeyboardDescriptor`), and
setup removes the group from any granted interface whose descriptor is a
keyboard (udev cannot read descriptors). Verified on the Z13 with
`udevadm test` against throwaway rules in `/run/udev/rules.d`: the guarded
rule did not match the AT keyboard while an unguarded control did, and the
DMI rule matched this machine and not an OXP pattern.
`TestButtonGrantsNeverReachATypingKeyboard`,
`TestKeyboardNodeGrantIsScopedToItsDevice`, `TestHidrawGrantsNameAnInterface`;
every negative control was run.
