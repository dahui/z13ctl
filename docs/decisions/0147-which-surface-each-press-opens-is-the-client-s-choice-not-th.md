# Which surface each press opens is the *client's* choice, not the daemon's

**Which surface each press opens is the *client's* choice, not the
daemon's** (`internal/buttonpref`; Jeff, 2026-08-14). The daemon reports that
a press happened and that a second one followed; it says nothing about what to
show, so the GUI's Settings tab offers the swap — one press for the quickbar
and two for the full window, or the reverse. **No protocol change was needed
or made.** `gui-open-full` keeps its name even though it now means "the other
surface": it is a published string every subscriber keys on, and renaming a
wire constant to improve a comment is not a trade worth making.
The preference is a *single* value — which surface a single press opens —
because there are two surfaces and two gestures, so one arrangement is the
other reversed. Storing both would admit a state where they name the same
surface, leaving the other unreachable.
`Toggle()` now reads as: put away whatever is in front, otherwise open the
primary. The dismissal half is unconditional and always was; only the last
step consults the preference. It is stored in `config.toml` — the file the
*UI* writes — rather than `gui.toml`, which is hand-edited and has no writer;
that split is now stated on `theme.AppConfig`.
**`theme.UpdateAppConfig` landed with it and is the real lesson.**
`SaveAppConfig` writes the whole file, so a caller that builds a fresh
`AppConfig` drops every field it does not set. `applyCustomAccent` had already
done that once and been fixed by hand; `applyTheme` still constructed
`AppConfig{Theme, Accent}`, complete only while those were the only two fields
— so adding a third would have silently discarded the button preference on
every theme change. A read-modify-write mutator makes it structural, and
`TestUpdateAppConfigPreservesEveryOtherField` fails if anyone goes back.
