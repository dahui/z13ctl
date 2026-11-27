# Generic toggle rows needed two api additions; both landed, and the renderer is now the full window's Settings tab

**Generic toggle rows needed two api additions; both landed, and the renderer
is now the full window's Settings tab.** The roadmap had `internal/controls`
rendering the firmware toggles (and later plugin features) from the device
document instead of the bottom bar's two bespoke switches.
(1) **`api.ToggleInfo.Description`** carries the prose those switches held as
GTK literals. It is *device data*, in the TOML beside the label, because
whether panel overdrive ghosts is a fact about the panel — a client rendering
rows generically cannot derive it, and the alternatives were to drop the
warnings or restate them per-id in every UI, the duplication
`api.ValidateProfileName` exists to prevent. It crosses four hops (TOML →
registry → `driver.ToggleSpec` → wire) and a drop at any one is silent, so
`TestToggleDescriptionsReachTheWire` guards the path and
`TestPanelOverdriveKeepsItsGhostingWarning` pins the consequence that
motivated the field. The negative control was run: removing the registry hop
fails both.
(2) **`api.State.Features`** is every declared toggle's current value, keyed
by the id `device-get` and the `feature` commands use, so a row learns its
state from the `get-state` a client already makes rather than one round trip
per toggle per sync. `BootSound`/`PanelOverdrive` remain forever as the fixed
vocabulary pre-2.0 clients know, and are now filled *from the same reads*
rather than by two hardcoded `Get` calls — a device with different toggles was
previously undescribable. A toggle whose value cannot be read is **omitted
rather than zero**: zero is "off", a claim about hardware, and a switch has to
be able to show "I do not know" — the same rule as a failed telemetry sample
being a gap. `readFeatures` returns nil (not an empty map) so `omitempty`
keeps the key off the wire for a device with no toggles.
A `Kind` field on `controls.Control` is still deliberately absent — see the
next entry for where the renderer actually landed.
