# The mouse wheel scrolls the drawer even over a slider (`Window.wheelScrollsView`)

**The mouse wheel scrolls the drawer even over a slider
(`Window.wheelScrollsView`).** `GtkRange` consumes scroll events to adjust
itself, and the drawer is a tall scrolling panel that is mostly sliders, so
a wheel flick passing over one silently changed a hardware setting instead
of scrolling — found on hardware, where scrolling past PL3 moved it from
90W to 30W. A **capture-phase** `EventControllerScroll` is what makes this
work: it sees the event before GtkRange's own bubble-phase handler, so
consuming it there is what stops the range acting, and the scroll is then
applied to the enclosing scroller by hand. The step is a fraction of
`PageSize`, not the adjustment's `StepIncrement` (a couple of pixels, which
moved the view almost not at all) — page-relative also keeps the feel
identical under gamescope, where every dimension is scaled. Returning false
when there is no scroller leaves the colour picker's sliders, in the one
view that does not scroll, answering the wheel as before. Every new
`gtk.Scale` must be passed through it.
