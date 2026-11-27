# Brightness and volume are the GUI's, and DE-agnostic; HDR was dropped

**Brightness and volume are the GUI's, and DE-agnostic; HDR was dropped**
(Jeff, 2026-11-17: "make sure the controls work in gamescope and gnome as
well"; `.claude/plans/handheld-controls.md`). Both are session-owned like
the refresh rate, so no daemon and no grant. **Brightness** has two
backends (`internal/display/brightness.go`): KWin (kscreen-doctor's output
brightness) on a KDE session, logind's `Session.SetBrightness` everywhere
else, gamescope included. KWin is not optional on KDE: measured on Plasma
6.8, a logind write moved the backlight to 50 % while KWin kept reporting
75 %, so Plasma's slider/OSD go stale. The logind session is resolved
through `Manager.GetUser` → `User.Display`, because voltaire-gui.service is
in no session and logind refuses a seatless one (seen from ssh). Range
1–100 %, never 0 (a black handheld screen hides the control that undoes
it); the backlight is chosen firmware > platform > raw. **Volume**:
`internal/audio`, wpctl then pactl, identical on every desktop; a set
unmutes. Both are session capabilities (`CapBrightness`, `CapVolume`, a PATH,
environment and sysfs check at startup), off by default and in the OXP's
device quickbar (DISPLAY / new SOUND group), plus Dashboard rows (brightness
heads DISPLAY, volume a SOUND card). One `levelSlider` type
(`gui/levels.go`), read on show and after each set, never on the poll.
**HDR was dropped** because only the compositor can switch it: one backend
per compositor (KDE, GNOME's DisplayConfig, Hyprland, gamescope), with no
runtime switch at all on others. Verified on the OXP: both brightness paths
(KWin 75→55→75 with the backlight following; logind the same), wpctl, the
drawer and Dashboard; Z13 focus dump `main` identical, `full:dashboard`
75→77, `full:settings` 25→31 (the editor's two rows).
