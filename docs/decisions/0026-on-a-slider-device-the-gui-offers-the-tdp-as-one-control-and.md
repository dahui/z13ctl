# On a slider device the GUI offers the TDP as one control, and the editor stores "custom" instead of sending bare

**On a slider device the GUI offers the TDP as one control, and the
editor stores "custom" instead of sending bare** (2026-11-17; the OXP's
OneXConsole model, `power.slider` + `profiles.pin`). A `tdp` drawer control
(`CapPowerSlider`, `gui/tdpslider.go`) and a TDP row in the Dashboard's
POWER card send a bare `tdp` set, debounced 400 ms (every write is three
SMU commands; the daemon's 150 ms spacing is only the backstop) and held
against mid-drag refreshes (`pending`, generation-counted). Its range is
`limits.TDPSliderMax` (the safe max, which get-state's `tdp_limits` move to
55 W on battery); the note under it is `profileui.TDPSliderNote` (firmware
preset, value above the battery cap, or "set by <profile>, moving the
slider leaves it"). On a pinned device the profile section is one
**Saved profile** dropdown (`profileui.SavedRows`: "No profile" = the pin,
then the saved profiles; the window adds "Profiles…"), with the slider
above it: performance-profile buttons mean nothing there (Jeff). A device
file may set the drawer's default contents (`[quickbar] controls`, served
as `device-get.quickbar`, `controls.DefaultsFor`; a user's saved list
still wins), and a read-only fan source (`telemetry.fans = "oxp_ec"`)
charts RPM without a `[fans]` section. **The slider never edits a profile** (Jeff,
2026-11-17: "A user should actively update it if they want"): a bare
`tdp` set on a slider device, from the GUI or the CLI, leaves a running
custom profile unchanged (`leaveProfileForSlider`: DeviceTDP set first, then
`applyStockHW` onto the pin/default, whose handBack writes the new value
once), and loading a profile only recalls what it saved. A profile's TDP is
changed with `--profile`, which the editor always sends there
(`EditPlan.Named`); the daemon treats a named edit of the running profile
as live. A pinned profile is one
**Default** button (`profileui.FirmwareFrom`). **The editor bug this
prevented:** `PlanEdit` made "custom" live while a firmware profile runs,
and on a slider device a bare send moves the slider, so the Profiles page
would have changed the remembered TDP while showing "custom" saved.
`PlanEditOn(…, slider)` stores it. Save As with no custom profile running
keeps the TDP setting (daemon and `SaveAsBlock`), the "save a slider
position, load it back" half of the model. `BasicSliderMax` reaches the
top when safe == forced. The charge-limit control now needs
`CapChargeLimit` (`battery.charge_limit`), or the OXP drew a dead slider.
Z13 focus dump byte-identical on all six lines; on the OXP the drawer is
profile/tdp/autoswitch and screenshots confirmed preset → 30 W.
