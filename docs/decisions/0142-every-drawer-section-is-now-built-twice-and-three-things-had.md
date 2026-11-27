# Every drawer section is now built twice, and three things had to stop being Window-level singletons for that

**Every drawer section is now built twice, and three things had to stop being
Window-level singletons for that.** `autoswitchSection` was already an
instance; `profileSection`, `batterySection` and `lightingView` became ones
(`newXSection` returns the instance and its widgets, `buildXSection` keeps the
drawer's registration), and each Window-level sync walks both through a
`xSections()` helper on the pattern `customViews()` set. The three that would
have failed *silently*:
(1) the RGB **swatch CSS ids**. Both current-colour squares are painted by a
provider registered display-wide and keyed on `#color1-swatch`, so two
instances editing different zones would have fought over one selector; each
now carries an id prefix.
(2) `Window.updateSwatches/sendApply/queueApply`. A preset click or an HSL
slider has to reach *its own* section, and those wrappers reached
`w.lighting` — the window's picker would have applied the drawer's zone.
`colorInput` carries its `owner` instead and the wrappers are gone.
(3) **`refreshState` had to grow `syncBattery` and `syncLightingSection`.**
Both were synced only by `syncState`, the *drawer's* own fetch, so the
window's copies would have shown whatever state was current when the page was
built and never moved again. Identical in shape to the settings-page fix a day
earlier, and the same rule: `refreshState` is the funnel, `syncState` is one
surface's.
