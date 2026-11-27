# Quickbar customization is a Settings card, saved to config.toml, with gui.toml as a hand-written override

**Quickbar customization is a Settings card, saved to config.toml, with
gui.toml as a hand-written override** (Jeff, 2026-10-09; plan in
`.claude/plans/quickbar-customization.md`). The QUICKBAR card lists every
control the machine supports with a show switch and ▲/▼ — plain buttons,
so mouse, touch and pad behave alike and there is no "holding a row" state.
Two controls exist only for it, **off by default**: `cpu-boost` and
`refresh-rate` (the drawer gets the live rate alone; the per-source rates
are configuration and stay in the window). `Control.Default` is what keeps
a default drawer byte-identical — verified, `main` n=40 unchanged. The
rules are pure in `internal/controls/editor.go`: `EditorRows` places a
hidden control after its nearest default-order predecessor (so switching CPU
boost on lands it with the power controls, not under RGB), `MoveRow`, and
`SavedList`, which saves **nil when the result is the defaults** (or a later
build's new default would never reach someone who once opened the editor)
and keeps IDs this machine has no row for. The refresh rate is the one
capability the document cannot answer, so `Resolve` takes **session
capabilities** (`CapRefreshRate`), never satisfied by a nil document.
Storage: config.toml's `quickbar_controls`, a string in
`controls.FormatList`'s encoding (`""` no choice, `"none"` empty) because
`theme.AppConfig` must stay comparable for its preserve-every-field test. A
gui.toml `controls` list wins (`controls.Effective`) and the card goes
read-only with a note naming the file. **The drawer rebuilds in place**
(`rebuildDrawer`): sections cleared, the drawer's section fields nil'd (every
walker was already nil-safe, and the window gates on the document, not on
them), re-resolved, synced from the state in hand, focus list rebuilt —
which also replaced `adoptDevice`'s "restart voltaire-gui" for a changed
capability set. Hints registered while building sections are recorded and
dropped with them, the one exception to "hint entries are never removed".
`focusWidget` keeps the pad on the arrow that moved (`coreglib.BaseObject`
pointers — `gtk.Widget.Native()` is the GtkNative ancestor, shared by every
widget in a surface). Verified on hardware with a throwaway timer build (no
Wayland pointer automation): each change saved, rebuilt and re-synced the
drawer. Known wrinkle: a hidden row's editor position is not stored, so
moving a row past a hidden one changes nothing in the drawer and the hidden
row's slot is recomputed on the next start.
