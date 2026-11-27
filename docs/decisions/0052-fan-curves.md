# Fan curves

**Fan curves**: Two hwmon devices under asus-nb-wmi: `asus` (RPM readings +
`pwm_enable`) and `asus_custom_fan_curve` (curve points + `pwm_enable`; 2
channels × 8 points on the Z13).
hwmon numbers are unstable across reboots — discovery by `name` sysfs attribute
via `FindFanHwmonPath()`. `pwm_enable` values: 0=full-speed, 1=custom,
2=auto/firmware. Modes 1 and 2 go to the **curve device only**: the base `asus`
device is `fan_type` SPEC83 on the Z13, whose `pwm1_enable_store` accepts just 0
and 2 (mode 1 is `-EINVAL`) and clears `custom_fan_curves[*].enabled` for every
fan before returning — so syncing the mode there, which z13ctl did through
v1.2.1, would disable the curve it had just enabled on any kernel or SKU that
accepts the write. Only `SetAllFansFullSpeed` (mode 0) and the RPM reads use the
base device. One curve goes to every curve channel — the `FanController`
contract, and right on the Z13, where both fans cool the one APU.
