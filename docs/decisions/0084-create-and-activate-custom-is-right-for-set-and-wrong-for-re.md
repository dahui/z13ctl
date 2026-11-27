# "Create and activate `custom`" is right for `--set` and wrong for `--reset`; `editTarget.implicit()` is the distinction

**"Create and activate `custom`" is right for `--set` and wrong for `--reset`;
`editTarget.implicit()` is the distinction.** `editTarget` carries both `Live`
("this edit writes hardware") and `Active` ("this profile was already
selected"), and they differ in exactly one case: a bare edit made while a
*firmware* profile is active. A `--set` there should create and activate
`custom` — the user is establishing a custom setting. A `--reset` asks to
*remove* one, and resolving it the same way made all three reset handlers
commit a **cleared** profile: `tdp --reset` on `balanced` deleted the fan curve
and power limits saved under `custom` and switched `state.Profile` to the
profile it had just emptied, `fancurve --reset` deleted its curve, and
`undervolt --reset` its offset. All three are ordinary things to type while on a
firmware profile and none of them said anything about the loss. The hardware
reset is still correct there; there is simply no profile to edit.
The guard is `t.Live && !t.Active`, **not** `!t.Active` — a `--profile <name>`
target that is not running is also inactive, and a reset there must still clear
the stored setting, which is the entire point of `--profile`. Writing it the
short way silently turned `fancurve --reset --profile gaming` into a no-op;
`TestResetProfileTargetStillClearsStoredSettings` is the guard, and it can drive
the real handlers because a non-live target reads the profile's own stored TDP
instead of hardware. `handleUndervoltReset` is driven separately, live paths
included, by `undervolt_reset_test.go`: it reaches no hardware but the
Undervolter, so a `device.Device` holding only a recording fake is hermetic.
