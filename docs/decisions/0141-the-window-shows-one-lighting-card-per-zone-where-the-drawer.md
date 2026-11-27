# The window shows one lighting card per zone where the drawer shows one card and a zone switch

**The window shows one lighting card per zone where the drawer shows one card
and a zone switch** (Jeff, 2026-08-14: "since we have more room in the main
window, we can split out the controls for the lightbar and keyboard rather
than using a radio button… Do NOT change the drawer, where space is at more of
a premium"). A mode switch is a price paid for space, and this surface has the
space, so it does not pay it. `lightingConfig.zone` fixes a block to one zone
and drops its tab row; the dashboard builds two, headed KEYBOARD and LIGHTBAR,
as a full-width row below POWER and DISPLAY — full-width because a six-effect
row starts ellipsizing below about half the window's width, so two of them
need all of it. The two cards genuinely differ in height when one zone is on
an effect that uses no colours; that is the honest rendering, and it is the
thing the radio could never show — a glance now says *keyboard static red,
lightbar cycling*.
This is the fourth thing the "built twice" work bought, and it needed only a
config field because the three silent hazards were already fixed: per-instance
swatch ids, `colorInput.owner`, and `refreshState` walking every instance. Two
new ones came with it — `blocks()` must **drop** an absent zone row rather
than append a nil (a nil `*gtk.Box` in a `gtk.Widgetter` is a non-nil
interface, so GTK receives it and crashes), and focus **sections are
namespaced per zone**, because `focusgrid.Sections` dedupes by name and two
blocks sharing `mode` would collapse into one bumper target, leaving the
second card reachable by D-pad and invisible to the gesture that exists to
skip past a card.
