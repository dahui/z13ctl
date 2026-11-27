# The HSL picker is a popup, not a page, and there is now exactly one of them

**The HSL picker is a popup, not a page, and there is now exactly one of
them** (`internal/gui/colorpopup.go`; Jeff, 2026-08-14). A page is the right
answer in a 320px column — it *is* the drawer's way of handling anything that
would otherwise want a window of its own — and the wrong answer in a window,
where choosing a colour took away the page being configured and blanked the
tab highlight to stay honest about a sub-page that is not a tab. The popup
layer already solves this exact shape for dropdowns, so the picker uses it.
**The unification is the point, not a side effect.** Keeping the page for the
drawer and adding a popup for the window would have been two pickers, which
is the one thing this codebase consistently refuses; one popup body, opened
into whichever layer `activePopup()` names and routed to the right zone by
`colorInput.owner`, deleted `colorview.go`, `mainWindow.{colorPicker,
returnTab, ensureColorView, showColorView, leaveColorView, onColorPage}`,
`Window.colorView`, the `colorPage` stack child, the `ActionBack` sub-page
case and the `hide()` reset — and the four rules that non-tab stack child
needed with them. `VOLTAIRE_GUI_OPEN_FULL=color` still opens it, now over the
dashboard.
Two things had to change underneath, and both are load-bearing:
**(1) `closePopup` detaches the body** (`p.scroll.SetChild(nil)`). A dropdown
builds a fresh list per open and never noticed; a shared body cannot be
parented into the second layer while the first still holds it, so the drawer
and the window would have worked one at a time.
**(2) A popup body may not size itself — it asks the layer**
(`popupLayer.minW`, `openPopupSized`). `popupgeom` clamps the *rectangle* to
the panel while GTK sizes the *child*, and GTK never allocates below a size
request: a `SetSizeRequest(380)` on the body overflowed the drawer's panel and
was clipped at the overlay, taking the readout column and the hex with it —
seen on hardware, and invisible in the window where 380 fits. As a floor
passed to `popupgeom`, the existing clamp does the work and the body fills
what it is given. Any future popup wider than its natural size wants the same
treatment.
**The sliders paint their own troughs** — hue through the spectrum at the
current saturation and lightness, saturation grey→colour, lightness
black→colour→white, all recomputed per change on the provider the preview
swatch already used. That is the difference between a picker and three
anonymous sliders. They carry `border: none` because the *desktop* theme's
trough border survives voltaire's sheet (which sets `background` and nothing
else) and is invisible only at GTK's default trough height; at the height a
ramp needs it drew a 1px frame in Breeze's blue.
