# On a slider device the undervolt is a device setting, like the TDP

**On a slider device the undervolt is a device setting, like the TDP**
(Jeff, 2026-11-17: "we should support undervolt"; `deviceundervolt.go`,
`State.DeviceUndervolt`, the OXP's `[undervolt]`). Enabling the block alone
would have shipped a feature that undoes itself: the offset lived only in
custom profiles, moving the TDP slider leaves the running profile for the
pin, and every route to a firmware profile clears the offset. So on a
`PowerSlider` device with an undervolter: a bare `undervolt --set N` is the
setting (applied, remembered; `0` and `--reset` clear it) and, like a bare
`tdp` set, leaves a running custom profile; the setting runs on the firmware
profile (`applyStockHW`) and under a custom profile with no offset of its own
(`applyCustomHW`, the handBack-for-TDP analogue); a profile's own offset wins
while it runs. Each change is **one** SMU write of the offset that applies,
never a reset followed by a write, and nothing is written when state says
that offset is already in force. `tdp-reset` keeps it in force,
`tuning-reset` clears it, Save As copies it. Restored at startup and resume,
where the `Active` flag is first marked lost (`deviceUndervoltLost`: CO does
not survive a reboot or a suspend, the state file's flag does), and by the
reconcile watcher's lost-resume arm. **The applied-gate covers it**:
`setUndervoltActive`/`undervoltActive` walk the device offset with the
profiles', so `uvApplied` still decides every reset; a clear keeps `Active`
until the hardware half has run (`CPUCO: 0, Active: true`), or the reset
would be skipped with an offset applied. `applyDeviceUndervolt` joined
`TestOnlyWritersProbeTheSMU`'s list. GUI: an Undervolt row under TDP in the
Dashboard's POWER card and an opt-in `undervolt` drawer control
(`gui/undervoltslider.go`, rules in `profileui/undervoltslider.go`); the
editor's note says the setting runs when the profile has none
(`UndervoltNoteOn`). Tests: `deviceundervolt_test.go` (SMU traffic per case;
two negative controls run: the gate ignoring the device offset, and the
firmware profile clearing instead of applying). **Verified on the OXP**, the
first CO write it has seen: −5 accepted, `tdp --set` left it in force with no
CO write, a daemon restart re-applied it, 20 s of `yes` ×16 ran clean, and
the reset was one logged write. **Without ryzen_smu, undervolt is now
*unavailable* on every device** (`smuMissing("Undervolting")` →
`UnavailableError`, generalized in `Assemble`): listing a section the
module's absence made dead would have brought back a Profiles tab with only
a hidden card on the OXP. On a Z13 with the module nothing changes; without
it, the card that was hidden is not built and the CLI names the module.
`register`'s `smuMissing` is the test seam (its `TestMain` answers "loaded";
`TestSMUMissingMakesUndervoltUnavailable` the other way), and the daemon's
`assembleForTest` supplies the undervolter, since module presence is the
test machine's fact — checked by running the suite with
`amd.smuDriverPath` pointed nowhere.
