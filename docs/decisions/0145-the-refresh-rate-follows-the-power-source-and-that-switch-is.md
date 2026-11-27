# The refresh rate follows the power source, and that switch is the GUI's for the same reason the manual control is

**The refresh rate follows the power source, and that switch is the GUI's for
the same reason the manual control is** (`internal/display/pref.go`; Jeff,
2026-08-14: "some people like to switch to a lower refresh rate when on
battery"). It reads like a daemon feature — it is autoswitch, on a schedule
the daemon already computes — and the daemon cannot do it: a video mode is the
compositor's, and `voltaire.service` has no guaranteed `WAYLAND_DISPLAY`. So
the split is that the daemon reports **that** the source moved (its watcher is
the only thing on the machine that reads it correctly — Mains-only,
edge-triggered, settled) and a session client decides what that means for the
screen. No protocol change was needed: the GUI already subscribes to
`power-source`, and `State.OnAC`/`SourceKnown` were already on the wire.
It is a **pair** — a rate on AC and a rate on battery — not the single battery
rate the request named. One rate alone is a one-way trip: voltaire would lower
the refresh on unplug and have nothing to put back on plug-in, and
"remembering" what it was is a guess the moment the user changes it by hand or
the GUI restarts in between. Explicit beats remembered.
**The on/off state is a switch, not a "don't change" row in each list**
(Jeff, 2026-08-14: "lets add an autoswitch switch so that it matches the power
state card"). The first cut had no switch and put the off state in the two
dropdowns, which is the same feature spread over two values that can disagree:
off is "don't change on both sides", one side set is a half state, and that
half state needed a caution of its own to explain what it did. `Prefs.Enabled`
is one bool, the lists offer only rates the screen has, and the card is then
the profile autoswitch block one card over rather than a second idea. It is
also why `config.toml` carries three keys rather than two: deriving enabled
from "are both rates set" would mean switching off had to erase them, leaving
nothing to restore.
**Turning the switch on fills both rows** (`DefaultPrefs`/`WithDefaults`):
AC takes the rate already running, so enabling cannot change the screen you
are looking at, and battery takes the lowest the screen offers, because
dropping it is the entire reason the feature exists. Defaulting both to the
running rate was the safer-looking option and is worse — the switch would do
nothing at all until the user found the second dropdown, which is a control
that appears broken. Neither value is silent: both land in the two rows
immediately, ahead of any transition. Switching *off* keeps them, so switching
back on restores the pair instead of re-guessing it.
`Prefs` has two accessors and the difference is load-bearing: `For(onAC)` is
the applier's question and returns nothing while the switch is off, `Rate(onAC)`
is the chooser's and ignores it. They agree whenever the rows are on screen,
so a caller reaching for `For` there is right by accident — and the accident is
what the second method removes.
Four more rules earned their place, and three of them are about *not* acting:
(1) **A preference is hertz, never a `Mode.ID`.** The id is the right thing to
send and the wrong thing to store — kscreen derives it from the mode list, so
it does not survive the list changing and means nothing on a second screen.
`Match` resolves a stored rate back to a mode at the moment it is needed.
(2) **`Match` is exact on the rounded rate, with no nearest-neighbour.** Dock
a screen with no 60 Hz mode and the honest answer is to leave it alone;
substituting whatever is closest retunes hardware the user was not configuring
when they set the preference. `TestMatchRefusesWhatTheScreenDoesNotHave`
pins it, and the rounding has its own test driven from the fixture's
2560x1440 modes (179.94 and 59.961) because the *native* mode list is whole
numbers and would have proved nothing.
(3) **The first observation latches without acting.** The daemon applies its
autoswitch decision at startup because a profile is state it owns and must
restore; a refresh rate is the compositor's and the compositor already
persists it. Applying at startup would override the session's saved mode at
every service restart and make a hand-set rate impossible to keep — the user
sets 180 on battery and gets 60 back at the next login, for reasons nothing
on screen explains.
(4) **Choosing a rate applies nothing**, even when it names the source
currently running. The row says what happens on a *transition*; the live
Refresh rate row directly above it is how the rate is changed now. Same
division as the autoswitch profile targets, which also store and let the next
transition act.
Stored in `config.toml` (`refresh_autoswitch`/`refresh_ac`/`refresh_battery`)
— the file the *UI* writes — through `theme.UpdateAppConfig`, which is the
mutator that exists so a new field cannot be discarded by an unrelated write.
Its guard now compares the whole struct rather than field by field, since the
hand-listed version is exactly what the next field gets left out of.
`setRefreshPrefs` takes the whole value rather than one field, because the
switch changes two of the three at once and a per-field setter would have had
to write the file twice to do it.
