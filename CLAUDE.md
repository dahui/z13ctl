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
  types.go                   State, LightingState, FanCurvePoint, FanCurveState, TDPState, UndervoltState (exported protocol types)
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
  profile.go                 get/set performance profile (platform_profile sysfs); "custom" virtual profile
  batterylimit.go            get/set battery charge limit (power_supply sysfs)
  bootsound.go               get/set POST boot sound (asus-armoury firmware-attributes)
  paneloverdrive.go          get/set panel refresh overdrive (asus-armoury firmware-attributes)
  fancurve.go                get/set/reset custom fan curves (hwmon sysfs)
  tdp.go                     get/set/reset TDP power limits (asus-nb-wmi PPT sysfs)
  undervolt.go               get/set/reset CPU Curve Optimizer offsets via ryzen_smu
  status.go                  display system status (temperature, fans, profile, TDP, battery)
  setup.go                   install udev rules; applySysfsPerms helper (HID, hwmon, PPT, firmware-attributes, ryzen_smu)
internal/
  aura/                      Aura HID protocol implementation
    aura.go                  Writer interface + Init/SetPower/SetBrightness/SetMode/Apply/TurnOff
    modes.go                 Mode and Speed constants + ModeFromString/SpeedFromString
  cli/                       CLI helpers shared by cmd/ and internal/daemon/
    cli.go                   package doc file only
    colors.go                named color table, ResolveColor, PrintColorList
    parse.go                 ParseColor, ParseBrightness
    sysfs.go                 FindProfilePath, SetProfile, FindBatteryThresholdPath, FindBootSoundPath, SetBootSound, FindPanelOverdrivePath, SetPanelOverdrive, FindAPUTemperaturePath, ReadAPUTemperature, FindBatteryCapacityPath
    fan.go                   hwmon discovery, fan curve read/write (both fans), RPM read, mode control, ParseFanCurve, SetAllFansFullSpeed
    tdp.go                   PPT sysfs helpers, safety constants (TDPMin/TDPMaxSafe/TDPMaxForced), ReadAllPPT, SetTDP, ResetTDP
    smu.go                   SMU sysfs communication: SMUAvailable, SMUProbeUndervolt, SendSMUCommand, response codes
    undervolt.go             Curve Optimizer commands: SetCurveOptimizer, ResetCurveOptimizer, ValidateCOValues, safety limits
    undervolt_test.go        encodeCOValue, ValidateCOValues, smuResponseError tests
    fan_test.go              ParseFanCurve, FanModeName tests
    dryrun.go                DryRunApply, DryRunOff, DryRunBrightness, DryRunProfile, DryRunBatteryLimit, DryRunBootSound, DryRunPanelOverdrive, DryRunFanCurve, DryRunFanCurveReset, DryRunTdp, DryRunTdpReset, DryRunUndervolt, DryRunUndervoltReset
  daemon/                    long-running daemon (socket server, state, button watcher)
    daemon.go                Package doc, Daemon struct, Run(), getListener() (socket activation)
    state.go                 XDG state file persistence (uses api.State/api.LightingState)
    button.go                evdev button watcher (KEY_PROG3 / Armoury Crate button on 2025 Z13)
    server.go                JSON request handler; handleConn(), dispatch(), command handlers
    resume.go                DBus logind PrepareForSleep watcher; turns off lightbar on sleep, restores lighting + volatile state on resume
    client.go                Redirect comment only — client functions live in api/
  hid/
    hid.go                   package doc file only
    device.go                Device type, Write, SetFeature, Paths, Descriptions, Close
    scan.go                  FindDevice, ListDevices, sysfs discovery, hasAuraReport
    export_test.go           test-only exports: NewTestDevice, NewTestDeviceAnon,
                             UeventToDevPath, DeviceNameFromUevent
contrib/
  systemd/user/
    z13ctl.socket            systemd user socket unit (socket activation, %t/z13ctl/z13ctl.sock)
    z13ctl.service           systemd user service unit (Type=notify, Restart=on-failure)
  systemd/system/
    z13ctl-perms.service     system oneshot unit: chgrp/chmod on BAT*/charge_control_end_threshold at boot
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
- `--dry-run` is a global persistent flag; each command checks `dryRunFlag` and
  calls the appropriate `cli.DryRun*` function.
- `--no-button` is a global persistent flag; only affects the daemon subcommand.
  When set, the button watcher goroutine is not started and EVIOCGRAB is not issued,
  allowing other tools to exclusively grab the Armoury Crate button device.
- **Daemon socket fallback**: CLI commands try the Unix socket first (1 s timeout);
  fall back to direct HID/sysfs if the daemon is not running. Detection is implicit:
  connection refused → fall back.
- **Daemon systemd integration**: `z13ctl.socket` uses socket activation (`LISTEN_FDS`).
  `z13ctl.service` uses `Type=notify` (sd_notify READY=1 after HID open + state restore),
  `WantedBy=graphical-session.target` (works in both desktop and Steam Gaming Mode).
  Logging goes to journald via stdout/stderr.
- **State persistence**: `$XDG_STATE_HOME/z13ctl/state.json` (atomic write via temp +
  rename). Daemon restores lighting, fan curves, and TDP on start. Fan curves and
  TDP are only restored when `profile == "custom"` (stock profiles let firmware manage).
- **Button watcher**: Finds "Asus WMI hotkeys" input device by sysfs name
  (`/sys/class/input/*/device/name`), opens it, grabs it exclusively with EVIOCGRAB,
  and listens for KEY_PROG3 (code 202) key-down events. KEY_PROG3 is the Armoury Crate
  button keycode on the 2025 ROG Flow Z13 (differs from older ASUS models that use
  KEY_PROG1). Forwards press events to subscribed GUI connections via long-lived
  socket connections. Disabled with `--no-button` to allow other tools exclusive
  access to the button device.
- **Daemon socket protocol**: Newline-delimited JSON over Unix socket. All responses
  are `{"ok":bool,...}`. CLI `--get` commands read sysfs directly (always ground truth).
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
  | full state | `{"cmd":"get-state"}` | `ok`, `state` (cached + sysfs + live temp/RPM + undervolt_available) |
  | subscribe | `{"cmd":"subscribe","events":["gui-toggle"]}` | `ok`, then streams events |
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
  via `FindFanHwmonPath()`. Both hwmon devices expose `pwm_enable` and both must
  be written for the kernel to honor the mode change. `pwm_enable` values:
  0=full-speed, 1=custom, 2=auto/firmware. Both fans cool the same APU (no
  discrete GPU), so the same curve is always applied to both fans simultaneously.
- **TDP (PPT power limits)**: Direct platform device attributes at
  `/sys/devices/platform/asus-nb-wmi/ppt_*` (NOT the firmware-attributes interface,
  which has empty calibration data). Five attributes: `ppt_pl1_spl` (Sustained),
  `ppt_pl2_sppt` (Short Boost), `ppt_fppt` (Fast Boost), `ppt_apu_sppt`,
  `ppt_platform_sppt`. Safety limits: 5–75W (safe), up to 93W with `--force`
  (G-Helper absolute max for 2025 Z13 GZ302E). When any PPT value exceeds 75W,
  all fans are forced to full speed before PPT writes. If fan write fails, TDP
  write is aborted entirely. APU sPPT and Platform sPPT always follow PL2.
- **Custom profile**: `profile --set custom` is a virtual profile that re-applies
  saved fan curves and TDP from daemon state. It does NOT write to `platform_profile`
  — the underlying hardware profile stays as-is. Setting any custom curve or TDP
  implicitly sets profile to "custom". Switching to a stock profile (quiet/balanced/
  performance) resets fan hardware to auto and TDP to firmware defaults, but preserves
  custom settings in state for later recall. `profile --set custom` returns an error
  if no custom fan curves or TDP have been saved.
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
  `os.Pipe()` backed devices via `NewTestDevice`.
- Current coverage: ~79% aura, ~97% cli, ~27% hid (hardware-bound scan functions
  are untestable without a device).
- aura error branches (write failures) are not covered because mockWriter never errors.

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
- `PROTOCOL.md` — technical HID protocol reference for developers.
- Per-command docs are not generated separately; `README.md` and `--help` are the
  authoritative references.

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
  `BAT*/charge_control_end_threshold` after `sysinit.target`. This is necessary because
  the attribute is created deep inside `asus_nb_wmi` probe() — after all observable
  udev child-device events — so no udev `RUN+=` hook can catch it. The service is a
  self-contained shell one-liner with no binary dependency.

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
