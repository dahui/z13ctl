# The gamepad focus grid skips *insensitive* widgets, not just hidden ones

**The gamepad focus grid skips *insensitive* widgets, not just hidden ones.**
`gtk_widget_activate()` does not consult sensitivity — it emits the activate
signal, which GtkButton turns straight into `clicked` — so the controller
path could fire controls a pointer physically cannot. Every desensitized
control in the drawer is desensitized because the daemon would refuse it, so
this presented as an error bar for controller users where mouse users get a
greyed button and a tooltip. Caught on hardware: a gamepad reached Delete on
the *active* profile and got `profile-delete: gaming is the active profile`.
The check lives in `focusItem.visible()`, whose result feeds
`focusgrid.Item.Visible`, so the pure navigation logic needs no change and
treats these exactly as it already treats hidden items; `activateOrEdit`
re-checks, because a state refresh can desensitize the focused widget after
focus landed on it. Skipping is deliberately not "focus but refuse": a
controller cannot read a tooltip, so a focusable-but-dead item says nothing.
