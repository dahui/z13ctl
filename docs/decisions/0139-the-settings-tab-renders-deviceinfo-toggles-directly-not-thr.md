# The Settings tab renders `DeviceInfo.Toggles` directly, not through `internal/controls`

**The Settings tab renders `DeviceInfo.Toggles` directly, not through
`internal/controls`.** `internal/controls` lists the *drawer's* sections, and
a firmware toggle is not one: the rows are device data, so a registry entry
per toggle would be a second list to keep in step with the document. What
does live in `controls` is `CapToggles`, so "does this machine have any"
has one answer for every surface — and it is the odd capability out, since
every other one is a nil-able document section while toggles are a *list*
whose emptiness is the question. `internal/settingsui` holds the rules
(`Rows`, `EmptyReason`) and `gui/settingsview.go` builds a label, a switch
and a send; nothing in it knows what a boot sound is.
Three rules earned their place. **Absent from `State.Features` renders as
insensitive, never as off** — the daemon omits a toggle it could not read
precisely so a failed read is distinguishable from "off", and reading absence
as zero here would put that claim back one layer up; insensitive also means
the gamepad grid skips it, which is the established rule for a control that
cannot be operated. **An unrecognized `Kind` is skipped** (`api.ToggleKindBool`
is the only one today): drawing an enumerated toggle as a switch would
misrepresent it and writing to it would send a 0 or 1 to something that means
neither. And **an empty page says which kind of nothing it is** — daemon not
running, device has none, or a kind this build cannot show — because the
three call for different responses from the user.
**The drawer's two bespoke switches were then removed** (Jeff, 2026-08-14):
BIOS settings nobody adjusts often, on the surface meant for the controls you
reach for in a hurry. Keeping them was the first instinct — drawer/window
overlap is the established shape for profiles and autoswitch — but those two
are things you change *because* you are already in the drawer, and a POST beep
is not. Nothing is lost under gamescope, where the full window is hosted in
the same surface, so its Settings tab is as reachable as the bar was.
That took `buildToggle`, both `*Switch` fields, `syncOverdrive`/`syncBootSound`
and `sendOverdriveSet`/`sendBootSoundSet` with it — the last hardcoded
per-toggle UI in the tree, which is the point: **the GUI now has exactly one
toggle write path** (`sendFeatureSet`, by id) and no code anywhere that knows
what a boot sound is. `api.SendBootSoundSet`/`SendPanelOverdriveSet` stay for
the CLI and for clients written against them.
It is also the **first deliberate change to the drawer's own focus-dump line**
(`main` 42 → 40, footer three items → one); the other six lines were
byte-identical across the move, which is what said the removal touched
nothing else.
One thing came free and is worth knowing: the switch styling was scoped
`.bottom-bar switch`, so re-scoping it to `.drawer switch` rather than
deleting it fixed the **autoswitch** enable switch, which had been wearing
stock Adwaita colours on every theme since it was written.
