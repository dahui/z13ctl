# z13ctl — Project Context for Claude

## What this project is

`z13ctl` is a Linux CLI for controlling RGB lighting, fan curves, TDP (PPT power
limits), and system settings on the 2025 ASUS ROG Flow Z13 via Linux hidraw,
asus-wmi sysfs, and asus-armoury firmware-attributes interfaces.
It uses the ASUS Aura HID protocol reverse-engineered from g-helper.
Module path: `github.com/dahui/z13ctl`. Binary name: `z13ctl`. License: Apache 2.0.

## Package layout

```
api/                         Public client API submodule (github.com/dahui/z13ctl/api)
  go.mod                     Separate module; stdlib only; importable by z13gui and external tools
  types.go                   State, LightingState, FanCurvePoint, FanCurveState, TDPState, UndervoltState,
                             CustomProfile, AutoswitchState; IsCustomProfile/InCustomProfile/ActiveCustomProfile
  client.go                  SocketPath, Send*, Subscribe (all client functions)
  example_test.go            testable examples for all Send* and Subscribe functions
main.go                      entry point
cmd/                         Cobra subcommands
  root.go                    root command, Version var, dryRunFlag, deviceFlag, noButtonFlag
  apply.go                   apply lighting effect
  brightness.go              set brightness only
  daemon.go                  start the daemon (z13ctl daemon)
  list.go                    list hidraw devices
  off.go                     turn lighting off
  profile.go                 get/set profile; create/save-as/delete/list custom profiles
  autoswitch.go              configure the AC/battery profile pair
  batterylimit.go            get/set battery charge limit (power_supply sysfs)
  bootsound.go               get/set POST boot sound (asus-armoury firmware-attributes)
  paneloverdrive.go          get/set panel refresh overdrive (asus-armoury firmware-attributes)
  fancurve.go                get/set/reset custom fan curves (hwmon sysfs)
  tdp.go                     get/set/reset TDP power limits (asus-nb-wmi PPT sysfs)
  undervolt.go               get/set/reset CPU Curve Optimizer offsets via ryzen_smu
  status.go                  display system status (temperature, fans, profile, TDP, battery)
  setup.go                   install udev rules; applySysfsPerms helper (HID, hwmon, PPT, firmware-attributes, ryzen_smu)
  setup_test.go              drift guard: generated vs packaged contrib/ artifacts
  undervolt_test.go          --set parse rejections (CLI/daemon parity; no SMU access)
  autoswitch_test.go         flag-combination rejections; profile-name parity; --profile guards
internal/
  aura/                      Aura HID protocol implementation
    aura.go                  Writer interface + Init/SetPower/SetBrightness/SetMode/Apply/TurnOff
    modes.go                 Mode and Speed constants + ModeFromString/SpeedFromString
  cli/                       CLI helpers shared by cmd/ and internal/daemon/
    cli.go                   package doc file only
    colors.go                named color table, ResolveColor, PrintColorList
    parse.go                 ParseColor, ParseBrightness
    sysfs.go                 FindProfilePath, SetProfile, FindBatteryThresholdPath, FindBootSoundPath, SetBootSound, FindPanelOverdrivePath, SetPanelOverdrive, FindAPUTemperaturePath, ReadAPUTemperature, FindBatteryCapacityPath
    power.go                 FindACOnlinePath, OnACPower (Mains-only discovery), IsStockProfile, ValidateProfileName
    power_test.go            mains discovery vs the decoy supplies; profile-name rules
    fan.go                   hwmon discovery, fan curve read/write (both fans), RPM read, mode control, ParseFanCurve, SetAllFansFullSpeed
    paths.go                 sysfs roots as vars (injectable by tests); see Testing
    tdp.go                   PPT helpers, safety constants, StockProfilePPT, ReadEffectivePPT, ReadAllPPT,
                             SetTDP, SetTDPState, TDPStateFor, ApplyTDPSafely, CheckFanCurveFloor/CheckCurveAgainstTDP,
                             CheckFanFloorRelease/CheckFanFloorReleaseAt (the *At forms are pure, for inactive profiles)
    smu.go                   SMU sysfs communication: SMUAvailable, SMUProbeUndervolt, SendSMUCommand, response codes
    undervolt.go             Curve Optimizer commands: SetCurveOptimizer, ResetCurveOptimizer, ValidateCOValues, safety limits
    undervolt_test.go        encodeCOValue, ValidateCOValues, smuResponseError tests
    fan_test.go              ParseFanCurve, FanModeName tests
    sysfs_fake_test.go       fake sysfs tree + fakeSMU mailbox + ppdRunner stub
    fan_sysfs_test.go        fan curve/mode/RPM + profile/battery/firmware-attr tests
    smu_test.go              SMU mailbox protocol + Curve Optimizer tests
    tdp_test.go              SetTDPState/SetTDP/StockProfilePPT/ReadEffectivePPT tests +
                             ApplyTDPSafely fan-floor refusal (the thermal-guard regression test)
    dryrun.go                DryRunApply, DryRunOff, DryRunBrightness, DryRunProfile, DryRunProfileCreate/Save/Delete,
                             DryRunAutoswitch, DryRunBatteryLimit, DryRunBootSound, DryRunPanelOverdrive, DryRunFanCurve,
                             DryRunFanCurveReset, DryRunTdp, DryRunTdpReset, DryRunUndervolt, DryRunUndervoltReset
  daemon/                    long-running daemon (socket server, state, button watcher)
    daemon.go                Package doc, Daemon struct, Run(), getListener() (socket activation)
    state.go                 XDG state file persistence (uses api.State/api.LightingState)
    button.go                evdev button watcher (KEY_PROG3 / Armoury Crate button on 2025 Z13);
                             eventDevice seam — no Grab method, see issue #10
    button_test.go           device discovery + read-loop filtering (fake evdev device)
    hotplug.go               detachable-keyboard reattach watcher (polls sysfs, reopens HID + restores lighting)
    reconcile.go             custom fan curve / high-TDP floor watcher; pure reconcileTick seam + reconcileOnce
    profile.go               applyProfileLocked/applyCustomHW/applyStockHW, edit-target resolution,
                             profile create/save/delete/list handlers
    powersource.go           AC/battery autoswitch watcher; pure powerTick seam + powerSourceOnce;
                             UPower nudge + sysfs poll; autoswitchTarget for the Run() startup case
    powersource_test.go      powerTick decision table, settle window, charger flap (no hardware)
    reconcile_test.go        reconcileTick decision table (no hardware) + reconcileOnce race guard
    server.go                JSON request handler; handleConn(), dispatch(), command handlers, restoreStockPPT(), effectiveProfile()
    server_test.go           request validation + dispatch routing (no hardware access)
    state_test.go            state persistence: round-trip, corrupt-file preservation, temp cleanup,
                             legacy migration, reserved-name sanitisation
    clone_test.go            cloneState deep-copy (incl. CustomProfiles), saveState race regression,
                             broadcast wire shape, withLegacyProjection
    resume.go                DBus logind PrepareForSleep watcher; turns off lightbar on sleep, restores lighting + volatile state on resume
    client.go                Redirect comment only — client functions live in api/
  hid/
    doc.go                   package doc file only
    device.go                Device type, Write, SetFeature, Paths, Descriptions, Close
    scan.go                  FindDevice, ListDevices, sysfs discovery, hasAuraReport, descriptorHasAuraReport
    export_test.go           test-only exports: NewTestDevice, NewTestDeviceAnon,
                             UeventToDevPath, DeviceNameFromUevent, HasDeviceGlob,
                             DescriptorHasAuraReport
contrib/
  systemd/user/
    z13ctl.socket            systemd user socket unit (socket activation, %t/z13ctl/z13ctl.sock)
    z13ctl.service           systemd user service unit (Type=notify, Restart=on-failure)
  systemd/system/
    z13ctl-perms.service     system oneshot unit: chgrp/chmod on battery, firmware-attributes,
                             PPT, and ryzen_smu sysfs at boot (keep in sync with buildServiceContent)
```

## Key architectural decisions

- `aura.Writer` interface (not `*hid.Device`) — decouples aura from hid, enables
  mock-based testing without hardware.
- `hid.Device` holds `[]hidrawNode`; writes go to all nodes simultaneously.
- Zone 0 = keyboard (`0b05:1a30`), Zone 1 = lightbar (`0b05:18c6`). Both are always
  addressed; each physical device silently ignores the other zone's packets.
- `profile.go` and `batterylimit.go` use path discovery via `/sys/class/*/` rather
  than hardcoded paths, so udev-chmoded device inodes are used (not the ACPI alias).
- `setup.go` uses a two-part permission strategy: udev rules for boot persistence
  (real ADD events) + direct `chgrp`/`chmod` in Go for immediate effect (systemd 259+
  does not execute `RUN{program}` on synthetic `udevadm trigger` events).
- **Packaged permission artifacts must match the generated ones.** `z13ctl setup`
  generates udev rules + the perms unit at runtime, but package installs
  (.rpm/.deb/Arch) ship the static copies in `contrib/udev/99-z13ctl.rules` and
  `contrib/systemd/system/z13ctl-perms.service`. These drifted and cost .rpm users
  all PPT and fan-curve access on every reboot (issue #12). `cmd/setup_test.go`
  now asserts every grant appears in both. Escaping differs by file: `%%p` in
  `setup.go` is Sprintf escaping (single `%p` in the packaged file), while `$$f`
  is systemd's *and* udev's literal-dollar escape and stays doubled in **both** —
  a bare `$f` expands to empty and the loop silently chmods nothing.
- **`contrib/nfpm/postinstall.sh` must `systemctl restart z13ctl-perms.service`.**
  `enable --now` is a no-op on upgrade: the unit is `Type=oneshot` with
  `RemainAfterExit=yes`, so it is already active and new `ExecStart` lines never
  run. Without the restart, an upgrade does not apply added grants until reboot.
- `--dry-run` is a global persistent flag; each command checks `dryRunFlag` and
  calls the appropriate `cli.DryRun*` function.
- `--no-button` is a global persistent flag; only affects the daemon subcommand.
  When set, the button watcher goroutine is not started and z13ctl does not open
  the Armoury Crate button device at all — for users who would rather the keypress
  reach only their desktop, or who need another tool to manage the device.
- **Daemon socket fallback**: CLI commands try the Unix socket first (1 s timeout);
  fall back to direct HID/sysfs if the daemon is not running. Detection is implicit:
  connection refused → fall back.
- **Daemon systemd integration**: `z13ctl.socket` uses socket activation (`LISTEN_FDS`).
  `z13ctl.service` uses `Type=notify` (sd_notify READY=1 after HID open + state restore),
  `WantedBy=graphical-session.target` (works in both desktop and Steam Gaming Mode).
  Logging goes to journald via stdout/stderr.
- **State persistence**: `$XDG_STATE_HOME/z13ctl/state.json` (atomic write via temp +
  rename). Daemon restores lighting, fan curves, and TDP on start. Fan curves and
  custom TDP are only restored when `profile == "custom"`; a saved *stock* profile
  gets its `StockProfilePPT` row written instead, since the kernel's PPT
  attributes come up holding a stale 5W cache after boot.
- **Always snapshot state with `cloneState()` before releasing `d.mu`.**
  `api.State` holds a map and four pointer fields, so the plain `s := d.state`
  copy still aliases live daemon state. Handlers unlock before calling
  `saveState`, so marshaling an aliased snapshot races with any handler mutating
  the map or dereferencing a pointer under the lock — a concurrent map
  read/write, which Go turns into an unrecoverable crash rather than a catchable
  panic. `internal/daemon/clone_test.go` guards this under `-race`.
- **Button watcher**: Finds "Asus WMI hotkeys" input device by sysfs name
  (`/sys/class/input/*/device/name`), opens it, and listens for KEY_PROG3 (code 202)
  key-down events. KEY_PROG3 is the Armoury Crate button keycode on the 2025 ROG
  Flow Z13 (differs from older ASUS models that use KEY_PROG1). Forwards press
  events to subscribed GUI connections via long-lived socket connections. Disabled
  with `--no-button`.
- **Never call EVIOCGRAB on the button device** (issue #10). The "Asus WMI hotkeys"
  node carries `SW_TABLET_MODE` as well as KEY_PROG3 (`capabilities/sw = 2` on the
  Z13). Grabbing it exclusively — which z13ctl did through v1.2.0 — takes the
  tablet-mode transitions away from libinput, so attaching the detachable cover
  after login leaves the desktop in tablet mode and the cover keyboard dead until
  the session restarts. The watcher reads shared; evdev delivers to every
  non-exclusive reader. This is enforced structurally: `runButtonLoop` takes an
  `eventDevice` interface that deliberately has no `Grab` method, so reintroducing
  the grab is a compile error. Trade-off: the keypress also reaches the desktop,
  and a foreign exclusive grab now fails silently rather than logging EBUSY.
- **Daemon socket protocol**: Newline-delimited JSON over Unix socket. All responses
  are `{"ok":bool,...}`. CLI `--get` commands read sysfs directly (always ground
  truth) — except `profile --get`, which asks the daemon first and falls back to
  sysfs: `platform_profile` is never a custom profile name, so sysfs alone cannot
  say which custom profile is running.
  GUI/Decky callers use the daemon socket for all operations including GET. Protocol:
  | Command | Request | Response field |
  |---|---|---|
  | apply | `{"cmd":"apply","mode":"cycle","color":"FF0000","brightness":3,"device":"lightbar"}` | `ok` |
  | off | `{"cmd":"off","device":""}` | `ok` |
  | brightness | `{"cmd":"brightness","brightness":2,"device":""}` | `ok` |
  | profile set | `{"cmd":"profile","set":"performance"}` | `ok` |
  | profile get | `{"cmd":"profile-get"}` | `ok`, `value` |
  | battery set | `{"cmd":"batterylimit","set":"80"}` | `ok` |
  | battery get | `{"cmd":"batterylimit-get"}` | `ok`, `value` |
  | boot sound set | `{"cmd":"bootsound","set":"1"}` | `ok` |
  | boot sound get | `{"cmd":"bootsound-get"}` | `ok`, `value` |
  | panel overdrive set | `{"cmd":"paneloverdrive","set":"1"}` | `ok` |
  | panel overdrive get | `{"cmd":"paneloverdrive-get"}` | `ok`, `value` |
  | fan curve get | `{"cmd":"fancurve-get"}` | `ok`, `value` (JSON) |
  | fan curve set | `{"cmd":"fancurve","set":"48:2,..."}` | `ok` |
  | fan curve reset | `{"cmd":"fancurve-reset"}` | `ok` |
  | tdp get | `{"cmd":"tdp-get"}` | `ok`, `value` (JSON) |
  | tdp set | `{"cmd":"tdp","set":"60","pl1":"55","pl2":"65","pl3":"70","force":true}` | `ok` |
  | tdp reset | `{"cmd":"tdp-reset"}` | `ok` |
  | undervolt set | `{"cmd":"undervolt","set":"-20"}` | `ok` |
  | undervolt get | `{"cmd":"undervolt-get"}` | `ok`, `value` (JSON: `cpu_co`, `active`, `profile`) |
  | undervolt reset | `{"cmd":"undervolt-reset"}` | `ok` |
  | profile create | `{"cmd":"profile-create","set":"gaming"}` | `ok` |
  | profile save-as | `{"cmd":"profile-save","set":"gaming"}` | `ok` |
  | profile delete | `{"cmd":"profile-delete","set":"gaming"}` | `ok` |
  | profile list | `{"cmd":"profile-list"}` | `ok`, `value` (JSON) |
  | autoswitch set | `{"cmd":"autoswitch","enabled":true,"ac":"balanced","battery":"gaming"}` | `ok` |
  | autoswitch get | `{"cmd":"autoswitch-get"}` | `ok`, `value` (JSON) |
  | full state | `{"cmd":"get-state"}` | `ok`, `state` (cached + sysfs + live temp/RPM + undervolt_available + on_ac) |
  | subscribe | `{"cmd":"subscribe","events":["gui-toggle"]}` | `ok`, then streams `{"ok":true,"event":"gui-toggle"}` |

  `fancurve`, `fancurve-reset`, `tdp`, `tdp-reset`, `undervolt` and
  `undervolt-reset` take an optional `"profile"` field naming the custom profile
  to edit; absent/empty means the active one. `profile --set` now *rejects* a
  name that is neither a firmware profile nor a saved custom profile, where it
  used to forward any string to `platform_profile`.
- **Streamed events must set `OK: true`.** `response.OK` has no `omitempty`, so
  `broadcast(response{Event: ...})` ships `{"ok":false,...}` on a perfectly good
  event. The Go client keys on the `event` field and never noticed, but the
  documented protocol says every response carries `ok`, so a Python/Decky client
  honouring that contract silently dropped every button press (fixed in v1.2.1).
  `internal/daemon/clone_test.go` pins the wire shape.
- **Socket I/O must be deadline-bounded on both ends.** `net.DialTimeout` bounds
  only connecting. `api.sendCommand` sets a full-exchange deadline
  (`commandTimeout`, 10s) or a daemon that accepts and never replies hangs the
  CLI forever; `handleConn` sets `requestReadTimeout` (30s) on the request line
  or a client that connects and stays silent pins a goroutine and fd for the
  daemon's lifetime. Both deadlines are cleared for `subscribe`, which is idle by
  design. Timeouts are vars so tests can shorten them.
- **`api.Subscribe`'s cancel func must release the reader goroutine.** The reader
  parks in a channel send once the 8-slot buffer fills, where closing the
  connection cannot reach it — so cancel closes a `done` channel that the send
  selects on, guarded by `sync.Once` for idempotency. Without it, any caller that
  stops consuming leaks the goroutine and never closes the channel.
- **`applyLightingState` and `d.dev` require `d.mu`.** The hotplug watcher closes
  and replaces `d.dev`; `applyLightingState` also reads the `d.state.Devices`
  map that socket handlers mutate. The resume watcher previously read both
  unlocked, racing the hotplug watcher over a live HID handle.
- **IPC library**: Hand-rolled JSON. gRPC/drpc require protobuf code generation;
  `net/rpc` lacks streaming; Twirp is HTTP-only. Decky plugin Python backend connects
  via `asyncio.open_unix_connection()` + `json` — zero extra deps.
- **Shared sysfs helpers**: `internal/cli/sysfs.go` contains `FindProfilePath()`,
  `FindBatteryThresholdPath()`, `FindBootSoundPath()`, and `FindPanelOverdrivePath()`,
  used by both `cmd/` and `internal/daemon/server.go` to avoid duplication. Daemon
  GET handlers read sysfs directly (not cached state) for accuracy when another
  process has modified the setting.
- **asus-armoury firmware-attributes**: The `asus_armoury` kernel module (mainline
  since Linux 6.19) exposes BIOS attributes via the `fw_attributes_class` interface
  at `/sys/class/firmware-attributes/asus-armoury/attributes/`. On the 2025 Z13,
  `boot_sound` (POST beep toggle, 0/1) and `panel_overdrive` (panel refresh overdrive,
  0/1) are available and writable. These are BIOS firmware settings managed by the
  kernel — no daemon state persistence needed. The `current_value` files are
  `0644 root:root` by default; `setup.go` handles permissions via udev rules
  (`SUBSYSTEM=="firmware-attributes", KERNEL=="asus-armoury"`) and direct `applySysfsPerms()`.
  Other attributes exist (`charge_mode` is read-only charger type detection; PPT
  controls exist but are empty on Z13 due to missing DMI calibration data).
- **Fan curves**: Two hwmon devices under asus-nb-wmi: `asus` (RPM readings +
  `pwm_enable`) and `asus_custom_fan_curve` (8-point curves + `pwm_enable`).
  hwmon numbers are unstable across reboots — discovery by `name` sysfs attribute
  via `FindFanHwmonPath()`. `pwm_enable` values: 0=full-speed, 1=custom,
  2=auto/firmware. Modes 1 and 2 go to the **curve device only**: the base `asus`
  device is `fan_type` SPEC83 on the Z13, whose `pwm1_enable_store` accepts just 0
  and 2 (mode 1 is `-EINVAL`) and clears `custom_fan_curves[*].enabled` for every
  fan before returning — so syncing the mode there, which z13ctl did through
  v1.2.1, would disable the curve it had just enabled on any kernel or SKU that
  accepts the write. Only `SetAllFansFullSpeed` (mode 0) and the RPM reads use the
  base device. Both fans cool the same APU (no discrete GPU), so the same curve is
  always applied to both fans simultaneously.
- **A custom fan curve is dropped by any `platform_profile` write** (issue #15).
  `throttle_thermal_policy_write()` — which every profile write goes through —
  ends by clearing `custom_fan_curves[*].enabled`, and `fan_curve_write()` then
  returns early on `!enabled`. Nothing is reported to the process that set the
  curve. On a GNOME desktop, power-profiles-daemon writes `platform_profile` on
  every AC/battery transition and on any PPD hold, so a curve set by z13ctl stops
  working minutes later for no visible reason; Fn+F5, asusctl and tuned do the
  same. Two halves fix it: `SetBothFanCurves` reads `pwm_enable` back
  (`VerifyFanCurveActive`) so a dropped curve is an error rather than a false
  success — which is also what makes `ApplyTDPSafely` genuinely fail closed — and
  `internal/daemon/reconcile.go` polls the curve device's `pwm_enable` every 2s
  and re-applies. It polls the enable flag rather than `platform_profile` because
  `fan_curve_enable_show()` returns the driver's cached `enabled`, making it
  ground truth and catching every cause rather than the one we predicted. It
  never writes `platform_profile` (that would be a write-fight with PPD over
  every AC transition) and acts only while the active profile is a custom one
  (`obs.Custom`, from `state.ActiveCustomProfile()`), so a deliberate
  firmware-profile switch is left alone by construction. Named profiles are
  defended exactly as `custom` is; a reserved name never is.
- **`hwMu` guards hardware mutation sequences; lock order is `hwMu` then `d.mu`.**
  `applyProfileLocked` states this in its doc comment because it is the one
  function both a socket handler and a watcher call: the caller holds `hwMu` and
  must NOT hold `d.mu`, and nothing reached from it may call
  `d.effectiveProfile()`, which takes `d.mu`. It also decides custom-ness and
  snapshots the profile in a single `d.mu` critical section, closing the window
  where a concurrent `profile-delete` could remove the entry between the two.
  `d.state.Profile` is set *before* the hardware work, so the reconcile watcher
  starts defending the fans during the apply and a half-failed apply self-heals
  on the next tick.
  
  `d.mu` guards state, and every mutating handler does its hardware I/O outside
  it, so nothing otherwise stops the reconcile watcher interleaving its
  `SetBothFanCurves` with `handleProfile`'s `ResetAllFanCurves` — the fans would
  keep whichever mode landed last. `handleProfile`'s "custom" branch used to hold
  `d.mu` across the `cli.*` calls and now snapshots first, so the order holds
  everywhere. `*-get` handlers deliberately do not take `hwMu`: blocking a GUI
  read behind a fan write sequence would be a regression.
- **TDP (PPT power limits)**: Direct platform device attributes at
  `/sys/devices/platform/asus-nb-wmi/ppt_*` (NOT the firmware-attributes interface,
  which has empty calibration data). Five attributes: `ppt_pl1_spl` (Sustained),
  `ppt_pl2_sppt` (Short Boost), `ppt_fppt` (Fast Boost), `ppt_apu_sppt`,
  `ppt_platform_sppt`. Safety limits: 5–75W (safe), up to 93W with `--force`
  (G-Helper absolute max for 2025 Z13 GZ302E). When the **sustained** limit
  (PL1) exceeds 75W, both fans are written `HighTDPFanCurve` — a 50% PWM floor
  with `pwm_enable=1`, ramping to 100% at 80°C — before the PPT writes, and the
  TDP is not applied at all if that fails (see `ApplyTDPSafely`). The floor was
  80% (204) through v1.2.1 and users reported it as loud enough that they simply
  stopped using high TDP, so it is now 127; the *ramp* is what protects the APU,
  since a machine actually sustaining >75W is well past 60°C where the curve is
  far above the floor anyway. Burst limits alone do not trigger it.
  `SetAllFansFullSpeed` (`pwm_enable=0`) is an earlier strategy that nothing
  calls any more; the docs, the dry-run output, and this file all described it
  for far longer than the code did. APU sPPT and Platform sPPT always follow PL2.
- **`cli.ApplyTDPSafely` is the only way to apply a custom TDP.** Above
  `TDPMaxSafe` (75W) the fans must be held to a 50% PWM floor
  (`HighTDPFanCurve`). Five paths apply a TDP — `handleTDP`, the `handleProfile`
  "custom" branch, daemon startup, resume, and `cmd/tdp.go`'s no-daemon path —
  and they previously enforced this four different ways: two warned and applied
  anyway, one raised power *before* raising the fans and discarded the fan
  error with `_ =`, and only one refused. They all now call `ApplyTDPSafely`,
  which fails closed: if the fan write fails the TDP is not written at all.
  Release order is the mirror image — lower power *first*, then release the
  fans (`handleTDPReset`, the stock-profile branch, `cmd/tdp.go` reset), so the
  machine is never at a high limit with no floor. `internal/cli/tdp_test.go`
  guards this against the fake sysfs; the refusal case is the one that matters.
- **Fan-floor checks read hardware for the *live* profile, and the profile's own
  TDP for any other.** `CheckFanCurveFloor`/`CheckFanFloorRelease` go through
  `ReadEffectivePPT`; their pure siblings `CheckCurveAgainstTDP` and
  `CheckFanFloorReleaseAt` take a limit directly, which is what an inactive
  profile needs — hardware says nothing about a profile that is not running. The
  effect is a *stronger* invariant than before: a profile can never be stored in
  a state that would be unsafe the moment it is activated, and `ApplyTDPSafely`
  still fails closed at activation.
- **Fan-floor checks read hardware, not cached state.** `cli.CheckFanCurveFloor`
  and `cli.CheckFanFloorRelease` both take the *effective* profile and go through
  `ReadEffectivePPT`. `handleFanCurve` used to gate on `d.state.TDP`, which
  silently skipped the guard whenever state and hardware disagreed (a TDP set
  while the daemon was down, a reset state file). `fancurve --reset` is refused
  above 75W in both the daemon and the CLI: firmware auto has no floor, so
  dropping to it removes exactly the protection the limit requires. A PPT *read*
  failure is deliberately not a refusal — it must not make fan control
  unavailable.
- **Daemon tests cannot exercise the fan-floor guards.** Whether they refuse
  depends on the real `ppt_*` values and `internal/cli`'s path vars are
  unexported, so a daemon test that passes the guard writes the machine's actual
  fan mode. `internal/daemon/server_test.go` stays on parse-rejection paths; the
  guards themselves are covered hermetically in `internal/cli/tdp_test.go`.
- **Per-device lighting states must be normalized before use.** `handleOff`
  saves a named zone as `{Enabled: false}` with no mode/colour/speed, so
  `handleBrightness` reusing that entry produced an *enabled* state with empty
  fields — and `ModeFromString("")` is an error, so every later restore failed
  (daemon start, resume, hotplug). Worse, `applyLightingState` returned on the
  first zone error, so a broken keyboard entry also left the lightbar dark.
  `normalizeLightingState(ls, fallback)` fills gaps from the all-device state
  then `defaultState()`, and is applied on **both** the write path
  (`handleBrightness`) and the read path (`applyLightingState`) — the read side
  is what repairs the state files users already have. `applyLightingState` now
  continues past a failing zone and returns the first error.
- **`cli.SetProfile` must write the primary path even when the loop misses it.**
  It writes every platform-profile class device but only tracks the error for
  the one `FindProfilePath` picked; when no class device has a `profile` file
  that path is the ACPI alias, which lives outside `sysProfileDir` and the loop
  never visits. It returned nil having written nothing, and still called
  `setPPD`. Guarded by `primaryWritten` + a fallback write.
- **`SMUProbeUndervolt()` is destructive — never call it speculatively from the
  CLI.** The "safe no-op probe" sends CO offset 0, which is byte-for-byte what
  `ResetCurveOptimizer` sends, so probing *clears any active undervolt*. That is
  fine where the caller writes a CO value immediately afterwards
  (`SetCurveOptimizer`/`ResetCurveOptimizer`) or caches the result for the
  process lifetime — the daemon probes once at startup, before restoring saved
  offsets, and the `sync.Once` covers every later call. It is NOT fine in a
  short-lived CLI process, where the `sync.Once` is fresh every invocation: a
  `status` that probed would wipe the user's undervolt every single run. `status`
  therefore asks the daemon (`get-state`'s `undervolt_available`) and falls back
  to `SMUAvailable()` — a plain stat — with wording that claims less.
  `internal/cli/smu_test.go:TestSMUProbeIsDestructive` pins the payload equality;
  if the probe ever becomes genuinely read-only, that test is the signal to relax
  these warnings.
- **Every route to a stock profile must clear the undervolt.** `handleProfile`
  did; `handleTDPReset` and `cmd/tdp.go`'s `runTdpReset` did not, even though
  both land on "balanced". That left CO applied in hardware with
  `Undervolt.Active` still true while the daemon reported a stock profile — the
  same "custom setting leaks into a stock profile" defect as #12. Saved values
  are still preserved for recall; only `Active` and the hardware are reset.
  `setUndervoltActive(state, false)` stamps every saved profile, since CO is
  global hardware and at most one profile's offset can be applied. `Active` is
  set true in exactly one place — `applyCustomHW`, and only when the SMU write
  succeeded — so a profile copied while CO was live never claims to be applied
  before anything was written.
- **A corrupt state file is preserved, not silently replaced.** `loadState`
  renames an unparseable `state.json` to `state.json.corrupt` and logs before
  returning defaults; the next `saveState` would otherwise overwrite it, taking
  every saved setting with it and leaving nothing to diagnose. `statePath` also
  falls back to `os.TempDir()` when neither `XDG_STATE_HOME` nor a home
  directory resolves — the old code yielded the root-relative
  `/.local/state/...`, unwritable for any non-root user.
- **`make test` and `make lint` must run both modules.** `api/` is a separate Go
  module, so a bare `go test ./...` / `golangci-lint run ./...` from the root
  silently skips it — which is how two lint issues sat unnoticed in a *released*
  module. Both targets now `cd api` as a second step.
- **`--color 000000` is not black.** `aura.SetMode` sets the random-colour flag
  (`0xFF`) for an all-zero primary colour, matching g-helper, so the firmware
  picks a colour. `off` / `--brightness off` is how you get no light. Documented
  in `docs/commands.md` and the `--color` flag help.
- **Custom profiles are named records; `state.CustomProfiles` is the only
  in-memory truth.** A custom profile is a `CustomProfile{Name, FanCurve, TDP,
  Undervolt}` — per-subsystem *pointers*, so `nil` means "this profile does not
  control that subsystem" and a new subsystem (GPU PPT, dynamic boost) is an
  additive field that leaves old profiles loadable. None of them writes
  `platform_profile`; the firmware profile underneath stays as-is.
  `api.State.FanCurve/TDP/Undervolt` still exist but are a **projection**, filled
  in only by `withLegacyProjection` at the two serialization boundaries
  (`get-state` and `saveState`) so z13gui and the Decky plugin keep working. The
  source is the active custom profile, or `custom` when a firmware profile is
  active — i.e. whatever a bare `undervolt --set` edits and `profile --set custom`
  recalls. Projecting *nothing* on a firmware profile looks tidier and is a
  regression on both sides: a GUI showing "saved undervolt, not active" loses the
  value it displays, and a downgrade taken while on `balanced` writes those
  settings away entirely. `loadState` clears them after migrating, so nothing inside
  the daemon can read the stale copy instead of the map; internal readers go
  through `state.ActiveCustomProfile()`. Any new pointer/slice/map on `api.State`
  must also be added to `cloneState` — `clone_test.go` under `-race` is the only
  thing that catches a shallow copy of the profile map, and each entry needs a
  copy of its own (three pointers and a slice), not just a new map header.
- **The active profile is the default edit target; `--profile` overrides it.**
  `fancurve|tdp|undervolt --set` with no `--profile` edits the profile you are
  running, creating and activating `custom` when a firmware profile is active —
  exactly the pre-existing behaviour, so no existing invocation changes meaning.
  There is no working slot and no save step: the edit lands in the profile and
  persists immediately. `--profile <name>` edits a profile that is *not* running,
  stores only, and writes no hardware. That is not a convenience — autoswitch is
  unusable without it, since configuring the battery profile would otherwise mean
  applying it first. `resolveEditTargetLocked` + `commitEditLocked`
  (`internal/daemon/profile.go`) are the single place that resolves and commits.
- **Activating a custom profile *clears* the subsystems it does not set.**
  `applyCustomHW` releases the fans when the profile has no curve, resets the
  Curve Optimizer when it has no offset, and hands the PPT limits back to the
  firmware profile underneath when it has no TDP. Without that, switching from a
  profile with a 90W limit and a -25 offset to one that sets neither leaves both
  in force while the daemon reports the second profile — and A→B→A does not give
  the same machine as A. This only became reachable once custom→custom switching
  existed. Ordering is the fail-closed part and is not free to rearrange: the
  profile's own curve goes on *before* its TDP so `ApplyTDPSafely`'s floor is
  written last and wins; clearing the TDP lowers power before the fans are
  touched; and the fans are released only when no high sustained limit is in
  force, so a high-TDP profile with no curve of its own keeps the floor
  `ApplyTDPSafely` just wrote. `Run()` calls the same helper rather than
  hand-rolling the restore, so startup cannot drift from it.
- **There is exactly one apply-a-custom-profile sequence: `applyCustomHW`.** The
  socket command, the autoswitch watcher, `Run()`'s startup restore and
  `resume.go` all call it. Four hand-rolled copies is precisely how the ordering
  and clearing rules drifted apart; `handleTDP`'s post-lower fan step is the last
  place that repeats any of it, and it mirrors the same rule deliberately
  (restore the profile's curve, else release the fans, so lowering a limit and
  selecting the profile converge on the same hardware).
- **A daemon restart does not reset the fan controller or the Curve Optimizer.**
  Both are hardware state that outlives the process, so a custom profile that was
  in force still is. If the startup autoswitch resolution moves off it onto a
  firmware profile, `Run()` must release them exactly as `applyStockHW` would —
  otherwise the machine keeps running the old curve and offset while reporting a
  firmware profile, and the reconcile watcher stays inert because the profile is
  no longer custom.
- **Only a firmware profile name may reach `platform_profile`.** `Run()`'s
  restore is gated on `cli.IsStockProfile`, not on "not custom": a state file
  naming a profile that is neither — deleted by hand, or lost in a downgrade —
  would otherwise be written straight to the attribute. `loadState` also clears
  such a name, so `effectiveProfile` falls back to `platform_profile` instead of
  every later lookup erroring.
- **The profile CRUD handlers take `hwMu` even though they write no hardware.**
  The edit handlers resolve a target under `d.mu`, release it for the hardware
  write, then commit the profile back under `d.mu` again. Mutating the profile map
  inside that window is silently undone by the commit — and for a delete, undone
  by *resurrecting* the profile. `hwMu` is what makes the whole
  resolve-write-commit span exclusive. It also keeps a delete from landing
  part-way through `applyProfileLocked`, which would leave `state.Profile` naming
  a profile that no longer exists and so stop the reconcile watcher defending a
  fan curve that is live in hardware.
  `internal/daemon/profile_test.go:TestProfileMutatorsTakeHwMu` is the guard.
- **The `profile` request field is silently ignored by older daemons, which
  makes an unguarded `--profile` edit apply to the running machine.** It is an
  additive field, so a pre-1.3 daemon unmarshals the request, drops the field,
  applies the setting live, and answers `ok` — the CLI would print "stored in
  profile X (not applied)" over a real TDP change. This happened during
  development. `cmd.ensureProfileTargetSupported` probes `profile-list` (which
  answers `unknown command` on an older daemon) before every `--profile` send.
  Any other client offering profile targeting must do the same.
- **The firmware profile names are reserved at four layers.** `quiet`,
  `balanced` and `performance` can never name a custom profile, so selecting one
  always reaches the firmware profile: (1) `cli.ValidateProfileName` rejects
  them, (2) `applyProfileLocked` tests `cli.IsStockProfile` *before* it consults
  the map, (3) `api.State.IsCustomProfile` returns false for them ahead of the
  lookup, and (4) `loadState` drops a `custom_profiles` entry carrying one. Layer
  4 is not paranoia — `state.json` is a plain file a user can edit, and it parses
  fine, so the other three never see it. The reservation is load-bearing beyond
  aesthetics: `ReadEffectivePPT` disables its stale-5W fallback for any name
  absent from `StockProfilePPT`, which is right for a custom profile and wrong
  for a firmware one, so a custom "balanced" would misreport the power limits.
  Name validation is strict on write (`profile-create`/`profile-save` reject
  `Gaming` rather than folding it to `gaming`, or the user looks for a profile
  under a name that is not there) and lenient on lookup (`--set` and `--profile`
  do fold case).
- **Switching to a firmware profile preserves every custom profile**; it resets
  fan hardware to auto, clears the undervolt, and writes that profile's
  `StockProfilePPT` row. `profile --set <custom>` errors if that profile has no
  settings at all — there would be nothing to apply.
- **AC/battery autoswitch is edge-triggered on `online`, never level-triggered,
  and never reads `platform_profile`** (`internal/daemon/powersource.go`, issue
  #6). That is the whole safety argument: the watcher reacts only to a value
  neither z13ctl nor PPD nor the desktop can write, so the feedback loop that
  would produce a write-fight does not exist. A level-triggered "keep my profile
  applied" watcher would both fight PPD over every transition — the thing
  `reconcile.go` exists to avoid — and make a manual profile change impossible to
  hold. The intended semantics follow directly: a profile chosen by hand sticks
  until the source actually changes, and z13ctl yields in between (GNOME's
  Automatic Power Saver is a *low-battery* trigger, not an unplug trigger, and is
  correctly ignored). One observation function, `cli.OnACPower()`, with two
  triggers: a UPower `OnBattery` nudge for immediacy and a 2s poll as the
  backstop for Gaming Mode / no-UPower setups — so there is one behaviour to
  test, not two. An edge is confirmed on the following observation before
  applying; that settle window lets PPD's own transition write land first (else
  the custom curve is dropped until reconcile notices) and stops a loose USB-C
  connector driving a full PPT+fan+SMU write per bounce. A failed apply latches
  the source anyway and does not retry. **The startup apply lives in `Run()`, not
  the watcher**, which latches its first observation without acting: `Run()`
  holds the same-value guard that keeps a redundant `platform_profile` write —
  and the WMI fan-controller reset that comes with it — out of every daemon
  restart. There is deliberately no hook in `resume.go`: Go timers use
  `CLOCK_MONOTONIC` and do not advance across suspend, so the armed timer fires
  promptly after resume; duplicating it would race the watcher over `hwMu`.
- **`cli.FindACOnlinePath` filters on `type == "Mains"`, never on the presence of
  an `online` file.** On the Z13 the detachable keyboard registers as
  `hid-*-battery-N` (type `Battery`) and the two USB-C ports as
  `ucsi-source-psy-*` (type `USB`), and all of them expose `online` — a `*/online`
  glob reports mains power whenever the cover is attached. `OnACPower` returns an
  *error* when no Mains supply exists (VM, desktop, driver not yet bound); callers
  must treat that as unknown and do nothing, never as "on battery".
- **Stock PPT restore is explicit, not firmware-driven** (issue #12): the firmware
  does *not* re-apply per-profile PPT on a `platform_profile` write, and the
  `ppt_*` attributes have no "reset to firmware default" operation — writing 5W
  (an earlier attempt) just crippled the machine. `cli.StockProfilePPT` is
  therefore authoritative **on write**: `restoreStockPPT()` (present in both
  `internal/daemon/server.go` and `cmd/tdp.go` for the no-daemon path) writes it
  via `cli.SetTDPState` on every stock-profile switch, on `tdp --reset`, and at
  daemon startup. Use `SetTDPState` (exact five values) rather than `SetTDP`
  (mirrors PL2 into APU/Platform) — the measured table has APU/Platform at 70W
  for all three profiles while PL2 varies. Failures warn and continue; the saved
  custom TDP is never cleared on a profile switch.
- **`ReadEffectivePPT` must be passed the *effective* profile**, not
  `platform_profile`. `platform_profile` is never "custom", so passing it makes a
  legitimate 5W custom TDP (5W is a legal value — `TDPMin`) indistinguishable
  from the kernel's stale 5W cache, and the stock table gets reported instead of
  the real values. The daemon passes `d.effectiveProfile()`; `cmd/` uses
  `effectiveProfileForTDP()`, which asks the daemon first and falls back to sysfs.
- **Undervolt (Curve Optimizer)**: CPU voltage reduction via AMD Curve Optimizer,
  using direct SMU communication through the `ryzen_smu` kernel module's sysfs
  interface at `/sys/kernel/ryzen_smu_drv/`. Uses only the MP1 0x4C command for
  CPU CO (iGPU CO was removed — Strix Halo does not support it). Optional
  dependency — gracefully disabled when the module is not installed.
  `SMUProbeUndervolt()` sends a safe no-op probe at daemon startup to detect
  whether the installed `ryzen_smu` fork actually supports CO commands on this
  platform (the amkillam fork is required for Strix Halo; the leogx9r fork does
  not work). CO values are volatile (reset on reboot/sleep); the daemon
  reapplies them on startup and resume when the custom profile is active.
  Safety limit: CPU 0 to -40. `--get` returns saved values from daemon state
  plus the current profile (no sysfs readback exists for CO). `UndervoltState`
  includes an `Active bool` field indicating whether the offset is currently
  applied. Switching to a stock profile resets CO in hardware but preserves
  saved values in state for recall; the CLI displays "(not active)" when a
  stock profile is active. The `get-state` response includes
  `undervolt_available` (using `SMUProbeUndervolt()`, not just `SMUAvailable()`)
  so GUIs can hide controls when ryzen_smu is not installed or the wrong fork
  is present.
- **Sleep/resume hook**: `internal/daemon/resume.go` watches for DBus
  `org.freedesktop.login1.Manager.PrepareForSleep` signals. On sleep
  (`PrepareForSleep(true)`), turns off the lightbar via `aura.TurnOff()` (the
  keyboard turns off automatically in hardware, but the lightbar does not).
  On resume (`PrepareForSleep(false)`), restores lighting (regardless of profile)
  and reapplies volatile state: fan curves, TDP, and undervolt (when custom
  profile is active). Uses `github.com/godbus/dbus/v5`.
- **Keyboard hotplug watcher**: `internal/daemon/hotplug.go` handles the
  detachable keyboard. The keyboard (`0b05:1a30`) is its own HID device that
  loses power when detached; on reattach the firmware does not restore the
  previous RGB effect. The daemon opens the HID device once at startup and holds
  `d.dev`, so after a detach/reattach cycle the keyboard appears as a *new* hidraw
  node that the stale `d.dev` never references. `watchHotplug()` polls
  `hid.HasDevice("keyboard")` (sysfs-only presence check, no device open) every 2s;
  on an absent → present transition it calls `reopenAndRestore()`, which re-runs
  `hid.FindDevice("")` under `d.mu`, swaps in the new device (closing the old one),
  and re-applies saved lighting via the existing `applyLightingState()` (honoring
  per-device overrides). If the reopen fails — e.g. udev has not yet chmod'd the new
  hidraw node — the watcher does not latch the present state and retries on the next
  tick. No action is taken on detach (the keyboard powers off in hardware). Run()'s
  device-close defer closes whatever `d.dev` currently is, since hotplug may have
  replaced it.

## Go conventions established in this project

- Per-file descriptive comments go **below** the `package` line, not before it.
  Before-package comments must be `// Package X ...` format (revive `package-comments`).
- Every package has exactly one file with the package-doc comment (`// Package X ...`).
  For `cmd/`, that comment is in `root.go`. For `aura`, `hid`, and `cli`, there are
  dedicated `aura.go`, `hid.go`, and `cli.go` doc files.
- Octal literals must use `0o644` form (gocritic `octalLiteral`).
- Do not use `filepath.Join` with arguments that contain path separators
  (gocritic `filepathJoin`); use string concatenation instead.
- `t.Parallel()` must NOT be used in tests that redirect `os.Stdout` (race on global).
  This applies to all tests in `internal/cli/dryrun_test.go`.

## Linting

golangci-lint **v2** format. Config at `.golangci.yml`.
- Linter settings nest under `linters.settings:` (not top-level `linters-settings:`).
- `issues.exclude` and `issues.exclude-rules` are not valid in v2.
- Enabled linters: errcheck, govet (with shadow), staticcheck, misspell, revive
  (exported + unused-parameter), gocritic (diagnostic + style tags).
- Verify config with: `golangci-lint config verify`

## Testing

- Hardware not required. `internal/aura` uses `mockWriter`; `internal/hid` uses
  `os.Pipe()` backed devices via `NewTestDevice`; `internal/cli` uses a fake
  sysfs tree (see below).
- Current coverage: ~88% cli, ~78% aura, ~40% hid, ~21% daemon, ~38% api, ~9% cmd.
- aura error branches (write failures) are not covered because mockWriter never errors.

### Fake sysfs (`internal/cli`)

All sysfs roots this package touches live in `paths.go` as package **vars**, not
consts, purely so tests can redirect them. `sysfs_fake_test.go` provides
`newFakeSysfs(t)`, which builds a temp-dir tree (hwmon curve + readings +
k10temp devices, platform-profile devices, PPT, ryzen_smu, battery,
firmware-attributes) and points every var at it, restoring them on cleanup.

Two seams exist specifically to stop tests from touching the developer's machine
— both were added after tests did exactly that:

- `ppdRunner` (`sysfs.go`) wraps the `powerprofilesctl` exec. `newFakeSysfs`
  replaces it with a recorder; without this, exercising `SetProfile` changes the
  live power-profiles-daemon profile.
- `smuReadFile` / `smuWriteFile` (`smu.go`) wrap the ryzen_smu mailbox I/O.
  `fakeSMU` emulates the driver's write-then-read-response protocol, which plain
  files cannot. `resetSMUProbe(t)` clears the `sync.Once` behind
  `SMUProbeUndervolt` so each case re-probes.

**Never let a test reach a real sysfs write.** `internal/daemon` handlers call
`cli` directly and `cli`'s path vars are unexported, so daemon tests must stay on
validation/rejection paths that return before any hardware access —
`server_test.go` documents this at `TestHandleTDPForceBoundaryRejections`. A
daemon test that gets past `handleTDP` validation will change the machine's
actual power limits.

## Build / release

```sh
make build              # go build with version from git tags via ldflags
make test               # go test ./...
make cover              # test + coverage report
make lint               # golangci-lint run ./...
make mod-tidy           # go mod tidy for all modules (main + api/)
sudo make install            # install pre-built binary to /usr/local/bin
make install-service         # install + enable systemd user units (socket + service)
make uninstall-service       # stop, disable, and remove systemd user units
sudo make install-perms-service    # install system oneshot service for sysfs permissions on boot
sudo make uninstall-perms-service  # remove system permissions service
make snapshot           # goreleaser release --snapshot --clean  (no publish)
make release            # goreleaser release --clean             (requires pushed v* tag)
make clean              # remove z13ctl binary, dist/, coverage artifacts
```

Version is injected at link time: `-X github.com/dahui/z13ctl/cmd.Version={{.Version}}`.
Default value in source is `"1.0.0-beta"` (used only in local builds without ldflags).

## goreleaser

Config: `.goreleaser.yml`. GitHub Actions workflow: `.github/workflows/release.yml`.
- Builds for `linux/amd64` only (hidraw is Linux-specific; Z13 is x86_64).
- `before.hooks`: `go mod tidy` only.
- Archives include `LICENSE` and `contrib/systemd/**/*` (user + system unit files).
- `prerelease: auto` — tags with a pre-release suffix (e.g. `v1.0.0-beta`) are marked
  as pre-release on GitHub automatically.
- To release: `git tag v1.0.0 && git push origin v1.0.0`

## Documentation

- `README.md` — user-facing (installation, commands, colors, contributing).
- `docs/` — the mkdocs site (Material theme), published from `mkdocs.yml`.
  Build/serve with `make docs`; verify with `mkdocs build --strict`.
- **`docs/api-reference.md` is generated — do not hand-edit it.** It carries a
  `Code generated by gomarkdoc. DO NOT EDIT` header and embeds source line
  links, so it goes stale whenever `api/` changes. Fix the doc comment or the
  example in `api/*.go` and regenerate:
  `go run github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest ./api/... > docs/api-reference.md`
- `docs/daemon.md` holds the user-facing socket protocol tables; keep them in
  sync with `dispatch()` in `internal/daemon/server.go`.
- `docs/protocol.md` — technical HID protocol reference for developers.

## Current status and next steps

### Phase 1 (daemon) — COMPLETE

The daemon is fully implemented and passing `make build && make test && make lint`.

**Key daemon details:**
- Socket path: `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock`
- State file: `$XDG_STATE_HOME/z13ctl/state.json`
- Protocol: one newline-terminated JSON request → one JSON response; long-lived
  connections for `{"cmd":"subscribe","events":["gui-toggle"]}` (GUI use)
- Button watcher uses `github.com/holoplot/go-evdev`; systemd integration uses
  `github.com/coreos/go-systemd/v22` (activation + daemon packages)
- `ATTR{name}=="asus-wmi"` was removed from the platform-profile udev rule because
  platform-profile class devices do not have a `name` sysfs attribute file. The rule
  now matches `SUBSYSTEM=="platform-profile"` alone.
- `z13ctl-perms.service` (system-level oneshot) runs `chmod g+w` on
  `BAT*/charge_control_end_threshold` and firmware-attributes `current_value` files
  after `sysinit.target`. This is necessary because these attributes may be created
  after observable udev events — so no udev `RUN+=` hook can catch them reliably.

**To activate on this machine (must be done once after each `sudo z13ctl setup`):**
```sh
sudo z13ctl setup                # rewrites rules file; applies sysfs perms immediately
sudo make install-perms-service  # installs battery sysfs permissions service
make install-service             # installs daemon socket + service units
```

### Phase 2 (GUI) — api/ SUBMODULE COMPLETE, z13gui IN PROGRESS

The `api/` submodule (`github.com/dahui/z13ctl/api`) is complete and ready for use
by the separate z13gui binary. Phase 2a changes:
- Module path renamed from `z13ctl` to `github.com/dahui/z13ctl`
- `api/` submodule created with `State`, `LightingState` types and all `Send*`, `Subscribe` client functions
- `internal/daemon/` refactored to use `api.State`/`api.LightingState`
- `cmd/*.go` updated to call `api.Send*` directly
- Multi-module dev setup: `go.work` (gitignored) + `replace` directive in `go.mod` for pre-publication

**z13gui** will be a separate repo (`github.com/dahui/z13gui`) using:
- `github.com/diamondburned/gotk4` + `github.com/diamondburned/gotk4-layer-shell`
- Imports `github.com/dahui/z13ctl/api` for daemon communication
- Right-edge Wayland overlay drawer (~320px wide), touch-first (48px+ tap targets)
- Per-device tabs (Keyboard / Lightbar), mode grid, color swatches + custom picker,
  brightness/speed sliders, profile list, battery slider
- Trigger: Armoury Crate button → daemon → `gui-toggle` subscribe event → show/hide drawer

**Multi-module release workflow:**
1. Tag `api/v1.0.0` → `git tag api/v1.0.0 && git push origin api/v1.0.0`
2. Update `go.mod`: bump `require github.com/dahui/z13ctl/api v1.0.0`, remove `replace` directive
3. Tag main module: `git tag v1.0.0 && git push origin v1.0.0`
