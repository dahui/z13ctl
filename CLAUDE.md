# voltaire — Project Context for Claude

## What this project is

`voltaire` (formerly `z13ctl` and `z13gui`) controls RGB lighting, fan curves, TDP (PPT power
limits), and system settings on the 2025 ASUS ROG Flow Z13 via Linux hidraw,
asus-wmi sysfs, and asus-armoury firmware-attributes interfaces.
It uses the ASUS Aura HID protocol reverse-engineered from g-helper.
Module path: `github.com/dahui/voltaire/v2`. License: Apache 2.0.

**Two binaries from one module**: `voltaire` (the CLI + daemon, `CGO_ENABLED=0`)va
and `voltaire-gui` (the GTK4 overlay drawer, `CGO_ENABLED=1`), the latter merged
in from the former z13gui repo at 2.0 with its history intact. They share
`internal/version.Version` (one `-X` ldflag serves both) and talk to each other
only through `api/` — the GUI is a socket client like any other.

## Package layout

```
api/                         Public client API submodule (github.com/dahui/voltaire/api/v2)
  go.mod                     Separate module; stdlib only; importable by voltaire-gui and external tools
  types.go                   State, LightingState, FanCurvePoint, FanCurveState, TDPState, UndervoltState,
                             CustomProfile, AutoswitchState; IsCustomProfile/InCustomProfile/ActiveCustomProfile
  device.go                  DeviceInfo + section types — the device-get capability/limits document
  client.go                  SocketPath, Send*, Subscribe (all client functions)
  example_test.go            testable examples for all Send* and Subscribe functions
main.go                      entry point (voltaire CLI/daemon)
voltaire-gui/                entry point for the GUI binary (built to voltaire-gui/voltaire-gui
  main.go                    locally — a root dir and a root binary cannot share the name);
                             runs theme.MigrateFromZ13gui() before anything reads config
cmd/                         Cobra subcommands
  root.go                    root command, Version var, dryRunFlag, deviceFlag, noButtonFlag, noSleepReleaseFlag
  apply.go                   apply lighting effect
  brightness.go              set brightness only
  daemon.go                  start the daemon (voltaire daemon)
  list.go                    list hidraw devices
  off.go                     turn lighting off
  profile.go                 get/set profile; create/save-as/delete/list custom profiles
  autoswitch.go              configure the AC/battery profile pair
  batterylimit.go            get/set battery charge limit (power_supply sysfs)
  bootsound.go               get/set POST boot sound (asus-armoury firmware-attributes)
  paneloverdrive.go          get/set panel refresh overdrive (asus-armoury firmware-attributes)
  feature.go                 generic firmware-toggle access by id (--list/--get/--set id=value)
  fancurve.go                get/set/reset custom fan curves (hwmon sysfs)
  cpuboost.go                get/set cpufreq boost clocks
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
  cli/                       pure presentation remainder — NOT hardware access.
                             M1 moved everything that touches sysfs out of here;
                             see "Where the hardware code lives" below.
    doc.go                   package doc file only
    colors.go                named color table, ResolveColor, PrintColorList
    parse.go                 ParseColor, ParseBrightness, ParseFanCurve
    dryrun.go                DryRunApply, DryRunOff, DryRunBrightness, DryRunProfile, DryRunProfileCreate/Save/Delete,
                             DryRunAutoswitch, DryRunBatteryLimit, DryRunBootSound, DryRunPanelOverdrive, DryRunFanCurve,
                             DryRunFanCurveReset, DryRunTdp, DryRunTdpReset, DryRunUndervolt, DryRunUndervoltReset
  daemon/                    long-running daemon (socket server, state, button watcher)
    daemon.go                Package doc, Daemon struct, Options, Run(), getListener() (socket activation)
    state.go                 XDG state file persistence (uses api.State/api.LightingState)
    button.go                button watcher + pure pressTick seam: every press emits gui-toggle,
                             a second within [50ms, 400ms] also emits gui-open-full
    button_test.go           pressTick decision table (no evdev device, no clock)
    button_test.go           device discovery + read-loop filtering (fake evdev device)
    hotplug.go               detachable-keyboard reattach watcher (polls sysfs, reopens HID + restores lighting)
    reconcile.go             custom fan curve / high-TDP floor watcher; pure reconcileTick seam + reconcileOnce;
                             stands down while d.suspending, with a tick-counted staleness ceiling
    profile.go               applyProfileLocked/applyCustomHW/applyStockHW, edit-target resolution,
                             profile create/save/delete/list handlers
    powersource.go           AC/battery autoswitch watcher; pure powerTick seam + powerSourceOnce;
                             UPower nudge + sysfs poll; autoswitchTarget for the Run() startup case
    powersource_test.go      powerTick decision table, settle window, charger flap (no hardware)
    reconcile_test.go        reconcileTick decision table (no hardware) + reconcileOnce race guard +
                             the suspend gate and its ceiling
    server.go                JSON request handler; handleConn(), dispatch(), command handlers, restoreStockPPT(), effectiveProfile()
    deviceinfo.go            device-get capability document (pure deviceInfoFor) + feature/feature-get handlers
    telemetry.go             1 Hz sampler → telemetryring; pure telemetryTick seam (stands down while
                             suspending, for a different reason than the other watchers — see below);
                             telemetry-history handler; history() substitutes an empty ring
    telemetry_test.go        telemetryTick decision table + the stand-down through sampleOnce +
                             the history handler's window and defaults
    server_test.go           request validation + dispatch routing (no hardware access)
    state_test.go            state persistence: round-trip, corrupt-file preservation, temp cleanup,
                             legacy migration, reserved-name sanitisation
    clone_test.go            cloneState deep-copy (incl. CustomProfiles), saveState race regression,
                             broadcast wire shape, withLegacyProjection
    resume.go                DBus logind PrepareForSleep watcher; logind delay inhibitor; pure sleepTick seam;
                             on sleep turns off lightbar + releases fans to firmware auto (releaseVolatileState),
                             on resume restores lighting + volatile state (restoreVolatileState)
    resume_test.go           sleepTick decision table + the sleep/resume symmetry invariant (no hardware)
    client.go                Redirect comment only — client functions live in api/
  hid/
    doc.go                   package doc file only
    device.go                Device type, Write, SetFeature, Paths, Descriptions, Close
    scan.go                  FindDevice, ListDevices, sysfs discovery, hasAuraReport, descriptorHasAuraReport
    export_test.go           test-only exports: NewTestDevice, NewTestDeviceAnon,
                             UeventToDevPath, DeviceNameFromUevent, HasDeviceGlob,
                             DescriptorHasAuraReport
  version/                   Version var — one -X ldflag serves both binaries
                             (— device abstraction, M0–M2 —)
  driver/                    the per-hardware-class interfaces the daemon and CLI program
                             against (Fans, Power, Profile, Lighting, Buttons, …) +
                             driver.PowerEnvelope; device support is implementations
                             selected by data, not calls into one machine's sysfs layout
  device/                    assembles a Device (per-class driver fields, nil where the
                             machine has no such capability) from DMI identity matched
                             against the embedded devices/*.toml
    devices/                 device data — asus-rog-flow-z13-2025.toml
  drivers/
    asusz13/                 the 2025 ROG Flow Z13 (GZ302) driver: hwmon fan pair, PPT,
                             platform_profile, battery threshold, asus-armoury toggles,
                             ryzen_smu CO. This is where internal/cli's hardware code
                             moved at M1 — same files, same tests, same assertions.
      drivers.go             the driver.* implementations the registry constructs
      fan.go                 hwmon discovery, curve read/write (both fans), RPM, mode control,
                             SetAllFansFullSpeed, VerifyFanCurveActive
      sysfs.go               FindProfilePath, SetProfile, battery threshold, boot sound,
                             panel overdrive, APU temperature, battery capacity
      power.go               FindACOnlinePath, OnACPower (Mains-only discovery)
      rapl.go                powercap package-energy counter (read-only grant) + battery
                             flow, state and the Wh energy pair from power_supply
                             (power_now, or current x voltage; energy_now, or charge x voltage)
      gpu.go                 amdgpu: edge temp, busy %, sclk, VRAM carveout, and the pure
                             gpu_metrics v3.0 parser (GFX power at 124, UCLK at 186)
      cpu.go                 procfs/cpufreq: jiffie counters, average core clock, memory,
                             and the global boost switch (read/write/verify)
      npu.go                 amdxdna NPU power/util/clock over DRM ioctls; queried only
                             while runtime_status reads active (opening it resumes the NPU)
      net.go                 /proc/net/dev byte counters, summed over physical interfaces
                             only (a /sys/class/net/*/device link) so tunnels never double-count
      tdp.go                 PPT read/write: SetTDP, SetTDPState, ReadAllPPT
      smu.go                 SMU sysfs mailbox: SMUAvailable, SMUProbeUndervolt, SendSMUCommand
      undervolt.go           Curve Optimizer: SetCurveOptimizer, ResetCurveOptimizer, ValidateCOValues
      paths.go               sysfs roots as vars (injectable by tests); see Testing
      sysfs_fake_test.go     fake sysfs tree + fakeSMU mailbox + ppdRunner stub
      fan_sysfs_test.go / smu_test.go / tdp_test.go / power_test.go / undervolt_test.go
      rapl_test.go / battery_test.go / gpu_test.go / cpu_test.go / net_test.go
      register/             blank-import side-effect package wiring it into the registry
    aurahid/                 driver.Lighting over the Aura HID protocol ("aura-hid")
    evdevkey/                driver.Buttons over one key on an evdev device ("evdev-key")
  safety/                    the thermal-safety rules between handlers and drivers, pure and
                             parameterized by driver.PowerEnvelope
    safety.go                pure rules: FanCurveForTDP, FloorPWMAt, FloorAdjustsCurve,
                             CheckCurveAgainstTDP, CheckFanFloorReleaseAt
    engine.go                Engine{Fans, Power} — the only path to a PPT write:
                             ApplyTDPSafely, ReleaseTDP, Read, ReadEffective, RestoreStock,
                             CheckFanFloorRelease, Envelope. Device carries the Engine, not
                             the raw driver.PowerLimiter, so the floor cannot be bypassed.
  controls/                  M4: which drawer sections exist, what each needs from the device,
                             and their order — Resolve(gui.toml, device) + Layout (headings and
                             separators). Pure; internal/gui holds only ID→builder.
  mainwin/                   M4: the full window's page list (Telemetry, Profiles, Settings)
                             and opening geometry — Resolve(device)
                             over the same capabilities controls uses (SupportsAll, so the two
                             cannot disagree) + Fit(screen), which clamps each axis independently.
                             Pure; internal/gui holds only tab ID→view.
  telemetryring/             M4: the daemon's bounded sample history — fixed-capacity ring,
                             copy-in/copy-out, Since(now, d) window. Pure; the one package
                             that carries its own lock, and says why.
                             (— voltaire-gui, merged from z13gui at 2.0 —)
  gui/                       GTK4 overlay drawer: Window, state sync, widgets, theming.
                             The cgo island — excluded from make test/race/cover, except
                             gamepad/ and its hidblocker/, which are cgo-free (see Testing)
    layershell/              Wayland layer-shell backend (KDE, Hyprland, Sway)
    overlay/                 fullscreen click-through backend for compositors without
                             layer-shell — GNOME/Mutter above all
    gamescope/               X11 overlay backend for Steam Gaming Mode
    gamepad/                 evdev gamepad reader → normalized Actions (visible-only dispatch)
      hidblocker/            BPF LSM blocker keeping games from seeing the pad while open
    fonts/                   embedded Inter + fontconfig registration (see NOTICE)
  buttonpref/                which surface the Armoury Crate button opens: one value
                             (what a *single* press raises), its parse and its labels
  theme/                     theme definitions, config persistence, CSS generation — pure Go
    migrate.go               ~/.config/z13gui → ~/.config/voltaire first-run copy shim
    css.go                   BuildThemeCSS — emits every token twice (@z13-* and
                             @voltaire-*) through 2.x
  apiresult/                 turns api's (handled, err) pair into one error; ErrNotRunning
  limits/                    TDP + fan-curve rules the drawer needs so it never offers a
                             state the daemon would refuse (the testable half of the
                             custom view — gui/customview.go and gui/fancurve.go)
  lighting/                  the drawer's RGB rules: mode from state, controls per mode
  display/                   the screen's refresh rate: kscreen-doctor JSON → outputs,
                             Rates (the rates at the *current* resolution, deduped by
                             rounded label, highest first), Primary, Apply. The one GUI
                             control that does not go through the daemon — see the entry
                             below and the package doc. Pure but for Query/Apply, which
                             go through an exec seam tests replace
    pref.go                  the rate to select on each power source: Prefs{Enabled,AC,
                             Battery} with For (applier) vs Rate (chooser), ParseEnabled/
                             ParsePref/Format* (hertz, never a mode id), DefaultPrefs +
                             WithDefaults (what the switch fills in), PrefOptions, Match
                             (exact on the rounded rate; no nearest-neighbour) and
                             PrefNote — the two cautions the controls cannot show
  settingsui/                the full window's Settings page: which firmware-toggle rows a
                             device offers (from the document, never a written list), each
                             row's value from State.Features — absent means *unknown*, not
                             off — and which kind of nothing an empty page is
  profileui/                 the drawer's profile rules: list rows + affordances, live-vs-stored
                             edit planning (PlanEdit/ForEditor), name pre-checks, autoswitch
                             target options, power-source label
    commit.go                the window's one-commit labels: CommitLabel, UnsavedSummary
    battery.go               the battery card's headline: level, rate, and the
                             time-to-limit/full/empty estimate off the Wh pair
  colorconv/                 RRGGBB ⇄ HSL for the colour picker (separate so it is testable)
  focusgrid/                 D-pad focus navigation over rows/columns/sections
  keyrepeat/                 which held direction owns the gamepad auto-repeat
  panelgeom/                 panel rectangle, edge, slide animation, multi-monitor neighbour test
  popupgeom/                 in-surface popup placement: below/above/shortened, always in bounds
  telemetryplot/             M4: the dashboard's series shaping — Build(samples, now, window,
                             maxGap) → time-placed points, gap-broken segments, per-kind shared
                             axis, Groups/Shape. Pure; internal/gui only strokes the result
    header.go                per-card heading and live readout (HeaderTitle/HeaderValue/FormatValue)
    placeholder.go           the framed, trace-less loading cards + which kinds a device's
                             declarations justify framing at all
  uiscale/                   UI scale factor for gamescope, where GTK cannot be asked
  togglegate/                debounce window for the toggle signal
  startup/                   pre-GTK process startup: argument scan + log filtering
contrib/
  systemd/user/
    voltaire.socket          systemd user socket unit (socket activation; TWO ListenStream
                             lines — %t/voltaire/voltaire.sock + the pre-rename
                             %t/z13ctl/z13ctl.sock, served through all of 2.x)
    voltaire.service         systemd user service unit (Type=notify, Restart=on-failure)
    voltaire-gui.service     user service for the drawer (frozen-pid watchdog marker)
  systemd/system/
    voltaire-perms.service   system oneshot unit: chgrp/chmod on battery, firmware-attributes,
                             PPT, and ryzen_smu sysfs at boot (keep in sync with buildServiceContent)
  udev/                      packaged copies: 99-voltaire.rules (sysfs grants),
                             99-voltaire-gamepad.rules (gamepad read access)
  nfpm/                      package scripts: postinstall/preremove/postremove for voltaire,
                             gui-* for voltaire-gui (each migrates the pre-rename unit)
  aur/                       single voltaire-bin PKGBUILD (both binaries) + its
                             .install; release.yml patches the placeholders and pushes
                             on tag. provides/conflicts cover all four old names;
                             replaces=() is inert on AUR but declared anyway.
                             The retirement runbook for the old AUR packages is
                             ~/.claude/plans/voltaire-aur-playbook.md (Jeff executes).
  voltaire-gui.desktop       desktop entry
examples/themes/             shipped theme TOMLs (catppuccin, gruvbox, nord, rog-*, …)
website/                     the docs site (Astro Starlight; see Documentation)
  astro.config.mjs           site config: base /voltaire, sidebar nav, links validator
  src/content/docs/          page sources (.md/.mdx); reference/api-go.md is generated
```

## Where the hardware code lives

M1 moved every function that touches sysfs out of `internal/cli` and into
`internal/drivers/asusz13`, and lifted the thermal-safety rules into
`internal/safety`. **Many decision entries below still name the pre-M1
`cli.*` symbols.** Their reasoning is unchanged and still load-bearing — only
the package moved — so read them through this table rather than trusting the
prefix:

| Written as | Now |
|---|---|
| `cli.ApplyTDPSafely` | `safety.Engine.ApplyTDPSafely` |
| `cli.ReadEffectivePPT` | `safety.Engine.ReadEffective` |
| `cli.CheckFanCurveFloor` | `safety.Engine.CheckFanFloorRelease` (live) / `safety.CheckCurveAgainstTDP` (pure) |
| `cli.CheckFanFloorRelease` / `…At` | `safety.Engine.CheckFanFloorRelease` / `safety.CheckFanFloorReleaseAt` |
| `cli.FanCurveForTDP`, `cli.FloorPWMAt`, `cli.FloorAdjustsCurve` | `safety.*` (same names) |
| `cli.HighTDPFanCurve`, `cli.TDPMaxSafe`, `cli.HighTDPMinPWM` | `driver.PowerEnvelope` fields, from the device TOML |
| `cli.StockProfilePPT` | `driver.PowerEnvelope.StockProfilePPT`, loaded by `internal/device/config.go` |
| `cli.SetProfile`, `cli.IsStockProfile`, `cli.OnACPower`, `cli.FindACOnlinePath` | `internal/drivers/asusz13` |
| `cli.SetBothFanCurves`, `cli.ResetAllFanCurves`, `cli.LiveFanCurve`, `cli.SetAllFansFullSpeed` | `internal/drivers/asusz13` |
| `cli.SMUAvailable`, `cli.SMUProbeUndervolt`, `cli.SetCurveOptimizer`, `cli.ResetCurveOptimizer` | `internal/drivers/asusz13` |
| `cli.ValidateProfileName` | still `cli` — a wrapper delegating to `api.ValidateProfileName` |
| `cli.ParseFanCurve`, `cli.ParseColor`, `cli.ResolveColor`, `cli.DryRun*` | still `cli` — presentation, no hardware |

The structural rule the move bought: `Device` carries a `safety.Engine`, never
the raw `driver.PowerLimiter`, so no handler or plugin can reach a PPT write
without the fan floor. Drivers are passive — no locking, no goroutines, no
policy; serialization stays in the daemon (`hwMu`/`d.mu`) and safety stays in
`internal/safety`.

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
- **Packaged permission artifacts must match the generated ones.** `voltaire setup`
  generates udev rules + the perms unit at runtime, but package installs
  (.rpm/.deb/Arch) ship the static copies in `contrib/udev/99-voltaire.rules` and
  `contrib/systemd/system/voltaire-perms.service`. These drifted and cost .rpm users
  all PPT and fan-curve access on every reboot (issue #12). `cmd/setup_test.go`
  now asserts every grant appears in both. Escaping differs by file: `%%p` in
  `setup.go` is Sprintf escaping (single `%p` in the packaged file), while `$$f`
  is systemd's *and* udev's literal-dollar escape and stays doubled in **both** —
  a bare `$f` expands to empty and the loop silently chmods nothing.
- **`contrib/nfpm/postinstall.sh` must `systemctl restart voltaire-perms.service`.**
  `enable --now` is a no-op on upgrade: the unit is `Type=oneshot` with
  `RemainAfterExit=yes`, so it is already active and new `ExecStart` lines never
  run. Without the restart, an upgrade does not apply added grants until reboot.
- **The AUR package is setup-driven, not file-shipping — keep it one or the
  other.** `voltaire-bin.install` runs `voltaire setup` at install/upgrade, so
  the rules and perms unit are always the generated ones (no drift possible,
  and setup's pre-rename cleanup runs for free); the nfpm packages ship static
  files instead and rely on the drift guard. Do not "fix" the AUR package to
  also install `99-voltaire.rules` — two rules files (its `/usr/lib` copy plus
  setup's `/etc` copy) both active is the kind of half-state issue #12 was.
  The perms unit is the deliberate exception: the PKGBUILD ships it under
  `/usr/lib` while setup writes `/etc`; identical content, `/etc` wins, and
  removing the package leaves the setup-written copy governing — same shape as
  the pre-rename package.
- **`make install-service` installs all three user units, and a source install
  is the only path where forgetting one is invisible.** `voltaire-gui.service`
  has been packaged in `contrib/systemd/user/` and enabled by every
  distribution package since 2.0, but no make target installed it — so on a
  source install `systemctl --user restart voltaire-gui` answered "unit not
  found" while the drawer *was* running, started by the pre-2.0
  `z13gui.service` the target also never disabled. The compatibility symlinks
  are what make that confusing rather than merely broken: `/usr/local/bin`
  precedes `/usr/bin`, so `ExecStart=z13gui` in the old unit resolves through
  `z13gui` → `voltaire-gui` and runs the *new* binary under the *old* unit
  name. A unit name stops being evidence of which binary is running the moment
  those symlinks exist. `LEGACY_USER_UNITS` is therefore one list covering both
  the daemon and the GUI, and `install-service` and `uninstall-service` are
  each written against the full set.
- **`systemctl --user disable` cannot undo a `--global` enable, so
  `install-service` masks what it cannot disable.** The pre-2.0 packages enable
  their units with `systemctl --global`, which writes root-owned symlinks under
  `/etc/systemd/user/*.target.wants`. A `--user disable --now` stops the unit
  for the session — so it looks like it worked — but `is-enabled` still reports
  `enabled` and the unit returns at the next login. Printing the
  `--global disable` line was the first attempt and is not enough: it leaves a
  broken login one un-run command away, and the consequence is not subtle (see
  the next entry). Masking is the one lever a target running as the user has,
  it is user-scope only, `uninstall-service` unmasks, and the note names
  `systemctl --user unmask`. A make target run as the user still has no
  business writing `/etc` itself.
- **Two enabled GUI units make the drawer open itself at login, and the
  compatibility symlinks are why.** `voltaire-gui` is a GApplication holding
  `io.github.dahui.Voltaire`, so a second launch does not start a second
  drawer: it forwards `activate` to the running instance and exits, and
  `activate` on a running instance is deliberately a *toggle* (that is what
  makes the desktop entry work as a show gesture). Leave `z13gui.service`
  enabled beside `voltaire-gui.service` and login starts the same binary twice
  — `ExecStart=z13gui` resolves through `/usr/local/bin/z13gui` →
  `voltaire-gui` — 0.6 ms apart, and the loser toggles the winner open. The
  user meets a drawer they never asked for, and nothing in the drawer's own
  logic is wrong.
  Both halves of the fix are needed and neither substitutes for the other.
  `Conflicts=` + `After=` in the shipped units guarantees only *one* process:
  measured over ten login transactions it removed the spurious toggle every
  time but let **either** unit win, because when both are queued together
  systemd resolves the conflict by processing order and the second one
  processed stops the first. Making `voltaire-gui` the survivor is the job of
  removing the old unit — `--global disable` in the package scripts, the mask
  in `install-service`. With the mask in place the same ten-run sweep is
  unanimous. Any future unit pair aliased by a compatibility symlink needs the
  same treatment.
- **`make install` must not pair a fresh daemon with a stale drawer.** `make
  build` builds only the CLI, so `make build && sudo make install` installed
  whatever `voltaire-gui/voltaire-gui` happened to be lying in the tree — the
  CLI/GUI version skew that shipping the two as one package is meant to make
  structurally impossible, reintroduced by the one install path that predates
  the merge. `make build-all` is the pair; `install` compares the ldflags
  version string embedded in the GUI binary against `$(VERSION)` and warns.
  It reads the string with `grep -aqF` rather than running the binary, because
  the recipe runs as root and `voltaire-gui`'s first statement is
  `theme.MigrateFromZ13gui()`. The old line was
  `[ -f x ] && install ... || true`, which reported nothing when the binary was
  absent and swallowed a real `install` failure with it.
- `--dry-run` is a global persistent flag; each command checks `dryRunFlag` and
  calls the appropriate `cli.DryRun*` function.
- `--no-button` is a global persistent flag; only affects the daemon subcommand.
  When set, the button watcher goroutine is not started and voltaire does not open
  the Armoury Crate button device at all — for users who would rather the keypress
  reach only their desktop, or who need another tool to manage the device.
- `--no-sleep-release` is likewise daemon-only: the pre-sleep fan release is
  skipped and a custom curve stays in force through suspend. Both flags reach the
  daemon through `daemon.Options` rather than positional bools — `Run(ctx, true,
  false)` said nothing about which flag was which, and a third would be worse.
- **Daemon socket fallback**: CLI commands try the Unix socket first (1 s timeout);
  fall back to direct HID/sysfs if the daemon is not running. Detection is implicit:
  connection refused → fall back.
- **Daemon systemd integration**: `voltaire.socket` uses socket activation (`LISTEN_FDS`).
  It carries **two** `ListenStream=` lines — the canonical voltaire path plus the
  pre-rename z13ctl path — and the daemon accept-loops over every fd
  `activation.Listeners()` hands it; the non-systemd path creates both sockets
  itself (legacy-path failure is a warning, canonical failure is fatal). The
  client (`api.SocketPaths`) dials canonical-then-legacy, which also covers the
  upgrade window where a pre-2.0 daemon still runs on the old path only.
  `voltaire.service` uses `Type=notify` (sd_notify READY=1 after HID open + state restore),
  `WantedBy=graphical-session.target` (works in both desktop and Steam Gaming Mode).
  Logging goes to journald via stdout/stderr.
- **State persistence**: `$XDG_STATE_HOME/voltaire/state.json` (atomic write via temp +
  rename). On first run, a pre-2.0 `$XDG_STATE_HOME/z13ctl/state.json` is
  **copied, never moved** to the new path (`migrateOldStateFile`): the old file
  staying put is what keeps a 1.x downgrade safe through all of 2.x, and the
  copy is byte-for-byte so a corrupt old file still hits `loadState`'s own
  corrupt-file preservation instead of being judged during migration.
  Daemon restores lighting, fan curves, and TDP on start. Fan curves and
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
- **The same lesson has a second door: `EVIOCGRAB` on a device the drawer only
  *thinks* is a controller's.** The GUI's gamepad reader grabs a PlayStation
  controller's own touchpad so it does not act as a mouse behind the drawer, and
  its test for "is this a controller touchpad" was `ABS_MT_POSITION_X` and no
  gamepad buttons — which the Z13's own touchpad (`0b05:1a30`) and touchscreen
  (`04f3:43c7`) answer just as well. Opening the drawer therefore took both away
  from the compositor for as long as it was open, so touch input died
  system-wide; the stylus survived only because it reports pressure and tilt
  rather than MT slots, and `Esc` was the way out. Same failure as issue #10 —
  grabbing a device the rest of the desktop is using — reached from the GUI
  instead of the daemon, and the daemon's structural defence (an interface with
  no `Grab` method) does not transfer, because this reader legitimately grabs.
  A multitouch device now has to be shown to *belong to* a controller already
  tracked (`sameController`: `vendor:product`, refined by EVIOCGUNIQ or the
  EVIOCGPHYS root), and `INPUT_PROP_DIRECT` rules a touchscreen out ahead of any
  matching. `scan()` is two-pass for that reason: `/dev/input/event*` enumerates
  in node order and a controller's touchpad routinely appears *before* its
  gamepad, so a one-pass check could never see the sibling it needs.
  **The load-bearing part is that classification is now pure and tested.** It
  previously took an open `*evdev.InputDevice`, so it could not be exercised
  without the hardware in hand — which is exactly how an unqualified multitouch
  test survived to a release. `classify(deviceInfo, []deviceInfo)` is a pure
  function over identity and capabilities, and
  `internal/gui/gamepad/gamepad_test.go` drives it from the real capability sets
  of the devices involved. The case that matters most has its own entry:
  attaching a controller must not make the machine's own devices grabbable
  again. Ported from z13gui (its issue #18, fixed in 1.4.1); no voltaire release
  ever shipped the fault.
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
  | device document | `{"cmd":"device-get"}` | `ok`, `device` (capabilities/limits; static, cacheable) |
  | telemetry history | `{"cmd":"telemetry-history","seconds":60}` | `ok`, `history` (typed array; absent `seconds` = the whole window) |
  | feature set | `{"cmd":"feature","id":"boot_sound","set":"1"}` | `ok` |
  | feature get | `{"cmd":"feature-get","id":"boot_sound"}` | `ok`, `value` |
  | cpu boost set | `{"cmd":"cpuboost","set":"0"}` | `ok` |
  | cpu boost get | `{"cmd":"cpuboost-get"}` | `ok`, `value` (`0`/`1`) |
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
  | full state | `{"cmd":"get-state"}` | `ok`, `state` (cached + sysfs + live telemetry + undervolt_available + on_ac/source_known + battery_health) |
  | subscribe | `{"cmd":"subscribe","events":["gui-toggle"]}` | `ok`, then streams `{"ok":true,"event":"gui-toggle"}` |
  (events: `gui-toggle`, `gui-open-full`, `power-source`, `state-changed`)

  `fancurve`, `fancurve-reset`, `tdp`, `tdp-reset`, `undervolt` and
  `undervolt-reset` take an optional `"profile"` field naming the custom profile
  to edit; absent/empty means the active one. `profile --set` now *rejects* a
  name that is neither a firmware profile nor a saved custom profile, where it
  used to forward any string to `platform_profile`.
- **`subscribe`'s `events` list is honoured; it was ignored until v1.3.0.**
  `addSubscriber` now records the set and `broadcast` filters on it. That was
  invisible while `gui-toggle` was the only event, and became a live hazard the
  moment a second one existed: a client that subscribed to `gui-toggle` and wrote
  the obvious `for range ch { toggle() }` — reasonable when only one event
  existed — would toggle its window on every power-source change. A subscriber
  that did *not* want an event must survive the broadcast rather than being
  pruned. Events are `gui-toggle`, `power-source` and `state-changed`
  (`api/events.go`), and carry **no payload**: the name says what happened and
  `get-state` answers with current truth, whereas a payload describes the moment
  the event was queued. That is also why `api.Subscribe` keeps its
  `<-chan string` signature — the payload was the only thing that wanted a
  breaking change.
- **Broadcast writes are deadline-bounded (`broadcastWriteTimeout`).** Events are
  emitted from handlers holding `hwMu`, so an unbounded write to a subscriber
  that stopped reading would block every hardware operation in the daemon behind
  a socket buffer. A subscriber that cannot take a notification in time is
  dropped.
- **Every handler that mutates profile or thermal state calls `saveAndNotify`,
  not `saveState`.** That is the funnel which keeps a new handler from silently
  leaving clients showing stale values. Lighting handlers deliberately still call
  `saveState` directly — a brightness slider drag would otherwise emit a burst of
  events describing values the client just set.
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
  every AC/battery transition and on any PPD hold, so a curve set by voltaire stops
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
  (PL1) exceeds 75W, both fans must be on a curve that holds a 50% PWM floor,
  written with `pwm_enable=1` before the PPT writes, and the TDP is not applied at
  all if that fails (see `ApplyTDPSafely`). The floor is applied per point against
  `HighTDPFanCurve` — the 50% bottom ramping to 100% at 80°C: a curve's points are
  raised to that curve's value wherever they fall below it and left as drawn
  wherever they do not, and it is written whole only when the profile has no curve
  of its own (see `FanCurveForTDP`). The bottom of that curve was
  80% (204) through v1.2.1 and users reported it as loud enough that they simply
  stopped using high TDP, so it is now 127; the *ramp* is what protects the APU,
  since a machine actually sustaining >75W is well past 60°C where the curve is
  far above the floor anyway. Burst limits alone do not trigger it.
  `SetAllFansFullSpeed` (`pwm_enable=0`) is an earlier strategy that nothing
  calls any more; the docs, the dry-run output, and this file all described it
  for far longer than the code did. APU sPPT and Platform sPPT always follow PL2.
- **The high-TDP fan floor is a *per-point minimum against the whole
  `HighTDPFanCurve`*, not against its `HighTDPMinPWM` bottom, and not a replacement
  curve.** `cli.FanCurveForTDP` is the single place that rule lives: above
  `TDPMaxSafe` it returns the caller's own curve with each point raised to
  `HighTDPFanCurve`'s value at the matching index where it falls below it, and
  **every point above it left exactly as drawn**. Nothing is ever lowered.
  `HighTDPFanCurve()` is returned whole only when there is no curve at all —
  nothing to raise, so it is all that is left to write. `ApplyTDPSafely` and
  `reconcileTick` both call it.
  **The comparison is at each point's *temperature*, via `FloorPWMAt`, not at the
  matching slice index.** Index matching was the first attempt: a curve of
  `70:130,75:135,80:140,…` clears every index-matched comparison and still runs 55%
  fans at 80°C, *weaker* than the wholesale replacement it replaced. Since the
  justification is stated in temperature terms, and the commands reference
  publishes the floor as a temperature table, the comparison has to be too.
  **Clamping to the scalar minimum alone was wrong and shipped briefly during
  development.** It honoured the first half of the rationale for lowering the
  minimum from 204 to 127 and threw away the second: "the *ramp* is what protects
  the APU, since a machine actually sustaining >75W is well past 60°C". A curve flat
  at 127 satisfied a scalar 127 everywhere, so 93W sustained at 90°C ran the fans at
  50% where every pre-1.3.1 path would have reached 100%. Temperatures are always
  the user's — only PWM values move — and the floor is read at each point's
  *temperature*, interpolated piecewise-linearly between floor points
  (`safety.FloorPWMAt`), clamped to the first/last PWM outside the range. An
  earlier index-matched reading is what the temperature rule replaced: a point
  at 48°C is measured against ~137, the ramp's value there, not against
  whichever floor point shares its slice position. The function stays pure and
  total either way.
  `FloorAdjustsCurve` is derived from `FanCurveForTDP` rather than reimplementing
  the comparison, so the two cannot disagree about what counts as an adjustment;
  the scalar version answered "no problem" for curves the ramp does raise.
  **`CheckCurveAgainstTDP` must measure against the same floor.** It is the
  *edit-time* refusal, and while it used the scalar minimum `fancurve --set` was an
  open door around the whole rule: a curve flat at 127 cleared 127 everywhere, so
  `handleFanCurve` accepted it and wrote it verbatim, and the reconcile watcher then
  read `pwm_enable=1` as "the curve is live" and never corrected it — 93W at 90°C on
  50% fans, through the one write path that did not consult `FanCurveForTDP`. Any
  new path that writes a curve must go through one of the two.
  They used to carry separate copies that disagreed — the watcher honoured a curve
  above the floor, the apply path replaced *every* curve above `TDPMaxSafe` — and
  the apply path was the one users saw. A saved curve of 204→255 came back as the
  127→255 ramp, and a curve at 100% everywhere was downgraded to one that idles at
  50%; the watcher could not correct it either, because the fans were left in mode
  1, which reads as "the curve is live". It presented as "my fan curve resets to
  stock after sleep" because `applyCustomHW` runs on every resume, but every
  `tdp --set` above 75W had always done it.
  Two properties of the clamp are load-bearing: it **copies** — `want` aliases
  `p.FanCurve.Points` in daemon state, and clamping in place would rewrite the
  user's saved curve so there was nothing to restore when the limit came down —
  and it preserves the non-decreasing PWM order `ParseFanCurve` enforces, since
  `max(x, floor)` over a non-decreasing `x` is still non-decreasing.
  `cli.FloorAdjustsCurve` is the companion predicate every "the floor changed your
  curve" message must gate on. A *nil* curve counts as adjusted, while
  `CheckCurveAgainstTDP` alone answers "no problem" for it, since a curve with no
  points has none below the floor.
  **It must be evaluated against the curve as it was *before* the write.** On the
  daemon path `cmd/tdp.go` asked it *after* `SendTdpSetFor`, by which point the
  daemon had already written the clamped curve — so the live curve satisfied the
  floor by construction, the notice never printed, and the one case that did print
  it was a failed read returning nil. `preCurve` is now sampled before the send and
  reused by both the daemon and no-daemon branches. Any new "we altered what you
  asked for" message has the same ordering requirement.
  `reconcileTick` has the mirror-image trap: its curve branch must set `act.Reason`
  only when a curve action actually results, or a PPT-only reconcile on a curveless
  profile logs a fan-floor reason for a machine at 52 W — the TDP arm below only
  fills in a reason when one is not already set, so the wrong text wins.
  The edit-time refusals (`CheckFanCurveFloor` in `handleFanCurve`,
  `CheckCurveAgainstTDP` for a non-live `--profile` target) deliberately stay
  refusals rather than clamping: storing a curve silently different from the one
  the user drew is worse than saying no up front. Clamping is for the curve already
  in state when the *limit* rises afterwards, which no edit-time check can catch.
- **`cli.ApplyTDPSafely` is the only way to apply a custom TDP, and it takes the
  curve the caller intends to run.** Five paths apply a TDP — `handleTDP`, the
  `handleProfile` "custom" branch, daemon startup, resume, and `cmd/tdp.go`'s
  no-daemon path — and they previously enforced the floor four different ways:
  two warned and applied anyway, one raised power *before* raising the fans and
  discarded the fan error with `_ =`, and only one refused. They all now call
  `ApplyTDPSafely`, which fails closed: if the fan write fails the TDP is not
  written at all. Passing `nil` for the curve asks for `HighTDPFanCurve` wholesale
  and is right only when there genuinely is no curve; `cmd/tdp.go` has no profile
  state and so passes `cli.LiveFanCurve()`, or a `tdp --set` would discard a
  curve the user set moments earlier.
  Release order is the mirror image — lower power *first*, then release the
  fans (`handleTDPReset`, the stock-profile branch, `cmd/tdp.go` reset), so the
  machine is never at a high limit with no floor. `internal/cli/tdp_test.go`
  guards this against the fake sysfs; the refusal case and the
  "keeps a curve that already meets the floor" case are the two that matter.
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
  depends on the real `ppt_*` values and `internal/drivers/asusz13`'s path vars
  are unexported, so a daemon test that passes the guard writes the machine's
  actual fan mode. `internal/daemon/server_test.go` stays on parse-rejection
  paths; the guards themselves are covered hermetically in
  `internal/drivers/asusz13/tdp_test.go` and `internal/safety/safety_test.go`.
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
  in the commands reference and the `--color` flag help.
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
  running, creating and activating `custom` when a firmware profile is active.
  There is no working slot and no save step: the edit lands in the profile and
  persists immediately. `--profile <name>` edits a profile that is *not* running,
  stores only, and writes no hardware. That is not a convenience — autoswitch is
  unusable without it, since configuring the battery profile would otherwise mean
  applying it first. `resolveEditTargetLocked` + `commitEditLocked`
  (`internal/daemon/profile.go`) are the single place that resolves and commits.
- **Promotion starts fresh: a bare edit from a firmware profile commits
  `custom` containing only that edit** (`editTarget.freshImplicit()`; Jeff,
  2026-08-14). The pre-existing behaviour adopted whatever `custom` already
  stored, and both directions of that bit on the same day: a `tdp --set` from
  `balanced` dragged a months-old fan curve into hardware through `handleTDP`'s
  restore-what-the-profile-describes step, and a `fancurve --set` would have
  handed the reconcile watcher a stored 93W TDP to restore within two seconds —
  the watcher re-applies the active custom profile's TDP on *any* drift, not
  just above the safe max. The discarded settings are the accepted cost:
  keeping bundles is what named profiles and Save As are for, and an explicit
  `--profile custom` edit still edits the bundle in place, as does a bare edit
  while `custom` is already active. The *reset* handlers deliberately do not
  call `freshImplicit` — they key on `implicit()` to skip the commit entirely,
  because an edit overwriting the profile is the user establishing new
  contents, while a reset touching it would be the daemon discarding old ones
  (the silent-data-loss guard below). `TestImplicitEditStartsFresh` pins all
  three target shapes.
- **"Create and activate `custom`" is right for `--set` and wrong for `--reset`;
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
  instead of hardware. `handleUndervoltReset` cannot be tested that way — it opens
  with `SMUProbeUndervolt()`, which is destructive.
- **`reconcileCurveFor` may return nil, and the caller must honour that rather
  than substituting the tick's own `act.Curve`.** `reconcileOnce` writes the TDP
  first and then recomputes the curve against the limit that write established;
  `act.Curve` was computed against the *drifted* limit, so falling back to it
  reintroduced the stale answer. A curveless custom profile whose PPT had wandered
  above `TDPMaxSafe` fired both arms, the TDP arm restored the profile's own 52 W,
  and the fallback then wrote `HighTDPFanCurve` anyway — pinning both fans to a
  50% minimum on a profile that controls no fan curve at all. It stuck, because
  the next tick read `pwm_enable=1` as "the curve is live" and the TDP now
  matched. nil means "the limit imposes nothing and the profile has no curve;
  leave the fans where they are". The helper is pure so the table can cover it
  without writing the developer's fan controller.
- **The reconcile watcher's undervolt arm is signal-driven, not
  observation-driven, because CO has no readback.** `SetCurveOptimizer` and
  `ResetCurveOptimizer` are write-only — the ryzen_smu interface offers nothing to
  read back — so the watcher cannot tell that hardware lost the offset. CO is
  volatile across suspend and `restoreVolatileState` normally replaces it; when
  that signal never arrives the offset sits at stock while state reports it
  applied, and no observation can notice. `reconcileTick` therefore sets
  `act.Undervolt` on exactly one path: the stand-down budget expiring, which is
  the only evidence available that a `PrepareForSleep(false)` went missing. It is
  deliberately **not** a periodic re-apply — that would write the CPU voltage
  curve every 2 s on no evidence — and it is idempotent in the case the signal
  cannot distinguish (an abandoned suspend never lost the offset).
  `cli.SMUProbeUndervolt()` is called in the *apply* step rather than the observe
  step so the common path never touches it; the daemon's startup probe means the
  `sync.Once` makes it a cached bool read rather than the destructive write it
  would otherwise be.
- **`cli.DryRunTdp` takes the curve as a parameter; it must not read hwmon.**
  Mirroring `ApplyTDPSafely`'s own `want`, the CLI passes `cli.LiveFanCurve()`.
  Reading the fan mode inside the function made `internal/cli/dryrun_test.go`
  depend on the developer's machine — and that test package is `cli_test`, so
  `newFakeSysfs` is out of reach. It was not theoretical: only the `len(live) == 0`
  branch prints `HighTDPMinPWM`, and `TestDryRunTdp_HighSustained` asserts on it,
  so the test passed only while the machine happened to be on firmware auto and
  failed outright with a curve live. Any future `DryRun*` that would consult
  hardware state should take it as an argument for the same reason.
- **`Run()` persists a startup autoswitch decision that lands on a firmware
  profile.** A custom target is saved by `applyCustomHW`; a firmware target had no
  such path, so the daemon acted on a decision it never recorded and the state file
  kept naming the previous session's profile. That is self-correcting only while
  autoswitch stays enabled and configured identically.
- **`handleTDPReset` writes `platform_profile` even when already on `balanced`,
  and that is deliberate.** The redundant write costs a WMI call whose
  fan-controller reset is immediately superseded by the `ResetAllFanCurves` that
  follows, so it is invisible — whereas guarding it would also skip `setPPD`,
  since `cli.SetProfile` only syncs power-profiles-daemon after a successful
  primary write. `Run()`'s same-value guard exists because nothing else there is
  touching the fans; that reasoning does not transfer to a command whose whole
  purpose is to land on `balanced`.
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
  That last condition is checked against **hardware** — `cli.CheckFanFloorRelease`
  — and not only against `p.TDP`. `highTDP` is false whenever `ApplyTDPSafely`
  failed, *including* the case where it succeeded at writing `HighTDPFanCurve` and
  then failed at `SetTDPState`, so trusting the flag alone released the floor it
  had just written one line earlier. The same guard also covers a limit left high
  out-of-band, which cached state says nothing about.
- **`ResetAllFanCurves` verifies the release, as `SetBothFanCurves` verifies the
  curve.** It was a bare `setAllFanModes(2)`, so a release the driver silently
  ignored was indistinguishable from success. That became load-bearing with the
  pre-sleep release: firmware auto is what lets the EC stop the fans through
  s2idle, so an unnoticed failure is the difference between a quiet suspend and a
  machine that runs its fans all night. `verifyFanModeReleased` accepts mode 0
  (forced full speed is not a curve, so there is nothing left to release) and an
  unreadable channel, exactly as `VerifyFanCurveActive` does.
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
  neither voltaire nor PPD nor the desktop can write, so the feedback loop that
  would produce a write-fight does not exist. A level-triggered "keep my profile
  applied" watcher would both fight PPD over every transition — the thing
  `reconcile.go` exists to avoid — and make a manual profile change impossible to
  hold. The intended semantics follow directly: a profile chosen by hand sticks
  until the source actually changes, and voltaire yields in between (GNOME's
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
  keyboard turns off automatically in hardware, but the lightbar does not) and
  calls `releaseVolatileState`. On resume (`PrepareForSleep(false)`),
  `restoreVolatileState` restores lighting (regardless of profile) and reapplies
  volatile state through `applyCustomHW` (when a custom profile is active). Uses
  `github.com/godbus/dbus/v5`.
- **On some machines a custom fan curve must be released before sleep, or the
  fans never stop.** The Z13 has no `deep` in `/sys/power/mem_sleep` — only
  `s2idle` — so the EC keeps running its fan loop for the whole suspend. Firmware
  auto (`pwm_enable=2`) is what makes it stop the fans; where `pwm_enable=1`
  survives into the suspend, the EC goes on driving them from the curve and never
  learns the machine is asleep. Any curve with non-zero low-temperature points then
  runs the fans all night, and `HighTDPFanCurve` guarantees it — every point is at
  or above `HighTDPMinPWM`. Reported as "fans not turning off when I close the lid"
  on the issue #15 thread (Fedora), and the docs asserted the *opposite* ("custom
  PWM curves reset to firmware defaults on sleep") for as long as the bug existed.
  **"On some machines" is load-bearing, and was established the hard way.** On the
  CachyOS development machine the curve is dropped across the suspend regardless,
  so the fans spin down with the daemon stopped and the release changes nothing
  observable. The release is therefore written to be *harmless where unnecessary*
  rather than conditional on detecting which case applies — there is no reliable
  way to ask "will this kernel keep my curve through s2idle" ahead of time.
  `--no-sleep-release` is the escape hatch.
- **Do not blame the sleep hook for a machine that will not stay asleep.** That
  was diagnosed twice, wrongly, during development: first as an SD card whose
  `mmc_bus_suspend` returns -84 (real, but present in only one of eight failures
  and harmless on its own), then as our `ppt_*`/`pwm_enable` writes provoking a
  delayed EC notification. The control run killed both — suspend aborted
  identically with the daemon **stopped**. The inference that led there was
  "suspends were minutes-long in earlier boots and seconds-long today, and today
  is when the hook landed"; it was unsound because there were no suspends that day
  *before* the change, so the comparison varied in more than the code. A suspend
  that aborts before `Freezing user space processes` had a wakeup event already
  pending; `/sys/power/pm_wakeup_irq` and `/sys/kernel/debug/wakeup_sources`
  (the `wakeup_count` column) name the source, and on the Z13 the touchscreen
  (`i2c-ELAN9008:00`) and the detachable cover are both wakeup-enabled.
- **`sleepTick` is gated on ownership, and that is what makes sleep and resume
  symmetric.** `sleepObs.Owned` is `ok && !active.Empty()` from
  `ActiveCustomProfile()` — exactly the condition `restoreVolatileState` restores
  under — so the invariant holds both ways: **the sleep hook releases only what
  `applyCustomHW` will put back.** A hardware-only gate would be a one-way door:
  a curve set by asusctl while voltaire sits on a firmware profile also reads
  `pwm_enable=1`, so releasing it (let alone lowering its PPT) leaves nothing on
  the resume side to restore either. `reconcileTick`'s `!obs.Custom` gate exists
  for the same reason. Mode 0 and an unreadable mode are left alone, mirroring
  `reconcileTick` branch for branch. `internal/daemon/resume_test.go` pins both
  the table and the symmetry.
- **The pre-sleep release lowers power before it touches the fans, and fails
  closed.** Above `TDPMaxSafe` it writes the underlying firmware profile's
  `StockProfilePPT` row via `restoreStockPPTErr` and *returns without releasing
  the fans* if that fails — the mirror image of `ApplyTDPSafely`, and the same
  release order as `applyStockHW`/`handleTDPReset`. `restoreStockPPTErr` treats a
  profile absent from `StockProfilePPT` as an error rather than the silent no-op
  `restoreStockPPT` can afford: "there was nothing to write" is not "the limit is
  now low enough to release the fans".
- **`d.suspending` stands the reconcile watcher down between the two signals,
  with a staleness ceiling.** The watcher polls every 2 s and would otherwise
  re-enable the curve in the window before userspace freezes. `reconcileObs`
  carries the flag so `reconcileTick` stays pure. The ceiling
  (`reconcileSuspendMaxTicks`) is counted in *ticks*, not elapsed time, and that
  is load-bearing: Go timers use `CLOCK_MONOTONIC`, which does not advance across
  suspend, so the count is of *awake* ticks, reachable only by a
  `PrepareForSleep(false)` that never arrived. It must also **exceed logind's
  `InhibitDelayMaxSec`**, since the delay lock is the only awake window inside a
  suspend: a shorter ceiling fires inside a real pre-freeze window and re-enables
  the curve, the very thing it guards against. 60 ticks is 120 s against a default
  of 5 s, with room for the 30 s and 60 s values people configure.
  `internal/daemon/resume_test.go:TestSuspendCeilingExceedsInhibitDelay` pins it.
  `restoreVolatileState` clears the flag via `defer` placed *before* `hwMu` is
  taken, so the early return for a firmware profile reaches it too.
  **Every watcher that writes fan hardware must consult it, not just
  `reconcileOnce`.** `powerSourceOnce` was missed: unplug the charger and suspend
  immediately, and it confirmed the edge, took `hwMu` after `releaseVolatileState`
  dropped it, and `applyCustomHW` put `pwm_enable` back to 1 before the freeze — the
  fans then ran for the whole s2idle period. A third watcher added later needs the
  same treatment.
  **It must stand down *before* `powerTick`, returning `prev` untouched.** The first
  attempt ran the tick and dropped only the apply, which discarded the transition
  permanently: this watcher is edge-triggered, `powerTick` latches `st.onAC` on the
  confirming tick, and every later tick then computed `sourceChanged == false`. The
  machine ran the AC profile on battery until the charger was physically cycled.
  Leaving the edge unobserved is what lets it be re-detected after the resume.
  `TestAutoswitchDeferralSurvivesTheSuspend` asserts the re-application, not just
  that an action was once produced — the test that missed this only checked the
  latter. The same reasoning applies to `d.suspending` being armed only once
  `sleepTick` has decided to write something: arming it unconditionally stood both
  watchers down for suspends that released nothing.
  **The budget is per-suspend, and `d.suspendGen` is what makes it so.** Resetting
  the counter on the first non-suspending tick looks sufficient and is not: a
  machine flapping sleep→wake→sleep faster than the 2 s poll never presents an idle
  tick, so the count accumulated across suspends and eventually expired *inside* a
  real pre-freeze window — the watcher then re-enabled the curve and undid the
  release. `setSuspending` bumps the generation on entry only (not on exit, and not
  on a repeated entry), and `reconcileTick` zeroes its counter whenever the
  generation it is counting for changes.
- **The daemon holds a logind delay inhibitor, because
  `PrepareForSleep(true)` is otherwise advisory.** logind emits it and proceeds
  to freeze; the release's sysfs writes racing that is how the fix would silently
  not apply on a fast-suspending system. `takeSleepInhibitor` must be called
  *before* the signal can arrive (watcher start, and again after each resume), and
  the fd is closed immediately after `releaseVolatileState` returns — logind
  suspends as soon as the last delay lock closes, so deferring it to the end of the
  loop would hold suspend open for `InhibitDelayMaxSec` (5 s by default).
  Best-effort throughout: a refused `Inhibit` logs at Debug and returns -1.
  There is deliberately **no settle delay** after the writes. One was added on the
  theory that the EC answers them with a notification a few hundred milliseconds
  later, which would land in the window where it aborts the suspend; the control
  run disproved the premise, and a fixed delay on every suspend with no evidence
  behind it is cargo cult. If the theory is ever revived it needs the
  `/sys/power/wakeup_count` measurement to support it, not timing coincidence.
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
- **The GUI is an ordinary socket client, and the merge did not change that.**
  `voltaire-gui` reaches the daemon only through `api/` — the same public module
  a Decky plugin or a third-party tool uses — so nothing in `internal/daemon`
  may grow a GUI-shaped shortcut. The two binaries share exactly two things:
  `internal/version.Version` and the `api` contract. That is what keeps the CLI
  free of cgo and lets the daemon be tested without GTK.
- **The drawer's limits come from `device-get`, with `DefaultLimits` as the
  fallback — and the two must stay interchangeable.** `internal/limits` was
  built for this swap and then sat unused for a release: the daemon shipped the
  capability document at M2 while `gui.go` still hardcoded
  `limits.DefaultLimits()`, so a release whose whole point was device
  abstraction had a drawer that believed every machine was a Z13. The tell was
  the package doc still saying "the daemon does not yet serve its limits over
  the API" long after it did. `limits.FromDevice` is the one entry point;
  `deviceLimits()` in `gui.go` is the only caller, and *any* failure —
  no daemon, a pre-M2 daemon answering `unknown command`, a malformed reply —
  falls back rather than erroring, because a drawer with slightly wrong bounds
  is worth having and the daemon validates every write anyway.
  Fetched **once, before any widget exists**: the two TDP scales and the fan
  curve editor bind their ranges at construction, so applying limits later
  means a rebuild path no second device exists to prove. The document is static
  for the daemon's lifetime, so the only case this misses is the drawer
  starting while the daemon is down.
  `TestDocumentMatchesTheDrawersFallback` (`internal/daemon`) is the guard that
  makes the fallback safe: device TOML → driver envelope → wire → `FromDevice`
  must land exactly on `DefaultLimits`, or the drawer behaves differently
  depending on whether the daemon happened to answer. It lives in the daemon
  package because that is the only place both halves are reachable —
  `internal/limits` cannot import the daemon. It caught a real difference on
  its first run: the envelope carries five PPT rails and the drawer compares
  three, so `FromDevice` narrows the table to `PL1SPL`/`PL2SPPT`/`FPPT`.
  Carrying the other two would cost twice — the fallback would stop being
  interchangeable with the fetched value, and the guard would fire on a
  difference that means nothing while saying nothing about the ones that do.
  **Capability *absence* is still not handled.** A nil `Power` or `Fans`
  section means the device lacks that capability and its controls should be
  hidden; `FromDevice` fills in defaults instead, because `Limits` describes
  bounds and cannot say "this control does not exist". Same reasoning defers
  `Curve` becoming a slice (`Shape().Points`) and firmware profile names coming
  from `ProfileInfo` rather than `api.StockProfiles`: all three need a device
  that actually differs before they can be anything but untested generality.
- **A capability the document declares must be one the driver actually reads.**
  Capability discovery is by absence, so a declared-but-unread source does not
  degrade to "nothing shown" — it degrades to a dashboard drawing a graph that
  is flat at zero, which is worse than no graph because it looks like a
  measurement. This bit at `telemetry.power_draw`, which was declared-and-unread
  for a release: the Z13 exposes powercap RAPL (`intel-rapl:0`, name
  `package-0`), but `energy_uj` is `0400 root:root` under the Platypus
  mitigation, so it needs a udev grant — and rather than do the grant, the
  device TOML simply named no source. **Both halves now exist**: `voltaire
  setup` grants group *read* on `energy_uj`, `Sample()` reads the counter, and
  the TOML declares `power_draw = "rapl"`.
  `TestTelemetryDeclarationMatchesWhatIsRead` (`internal/daemon`) replaced the
  test that pinned the absence, and still fails in both directions — a source
  declared with no reader, or a readable counter with no declaration. It
  *skips* when the counter exists but is unreadable, because that is a machine
  that has not run setup rather than a defect, and the skip message says which.
  Any new capability field with a reading behind it wants the same guard.
- **CPU boost is a capability of its own, not a firmware toggle, and the
  difference is who keeps the setting** (`driver.CPUBoost`; Jeff, 2026-08-14).
  It looks exactly like one: a 0/1 sysfs file, an on/off switch, a `--set 0`.
  But a firmware toggle is a BIOS setting the *machine* keeps, which is why
  nothing persists one — while cpufreq comes up boosting on every boot, so a
  user's "off" survives only if the daemon replays it. Rendering it beside the
  BIOS switches would have said the opposite about who is responsible, and
  nothing would have restored it. It sits with fan curves, PPT and the Curve
  Optimizer instead: recorded in state, restored in `Run()`.
  **Only a stored `false` is replayed.** `State.CPUBoost` is a `*bool`, and nil
  means the user never expressed a preference — writing the default back there
  would be voltaire claiming a setting it was never given, and it is the same
  absent-is-not-false rule `State.Features` follows for an unreadable toggle.
  `get-state` reads the kernel rather than serving the stored value, because
  anything with the grant can write that file; the stored value is an
  *instruction*, not a cache. New pointer on `api.State` ⇒ `cloneState` got it.
  **The write verifies.** `SetCPUBoost` reads back and errors when the value
  did not take — amd-pstate refuses it in some modes and the write succeeds
  anyway, which is the `SetBothFanCurves` lesson by another route. A failed
  *readback* is deliberately not an error: the write probably landed, and the
  caller's fallback is "unknown" regardless.
  **The grant is service-only, and it is the first one that has to be.** Every
  other target has a udev rule as a best-effort first pass with the perms unit
  behind it; `/sys/devices/system/cpu/cpufreq/boost` is a plain kobject rather
  than a device, so `udevadm info` answers "Unknown device" and no rule can
  match it at all. `cmd/setup_test.go`'s grant table records that asymmetry,
  and the negative control was run — deleting the line from the packaged unit
  fails the guard. **Existing installs need `sudo voltaire setup` again**, or
  the switch is there and every write is EACCES.
  Measured on this machine: boost off drops `scaling_max_freq` from 5187500 to
  3000000 across all 33 policies, and one write to the global file moves every
  one of them.
- **The powercap grant is the only read-only one, and that is not an
  accident.** Every other target `voltaire setup` touches is `chmod g+w`
  because voltaire writes it. `/sys/class/powercap` holds the package power
  *caps* alongside the energy counter, so a `g+w` grant there would hand every
  member of the group control of the CPU's power limits — through a rule whose
  entire purpose is to draw a graph. `cmd/setup_test.go:TestPowercapIsGranted
  ReadOnly` checks all four artifacts (both generated, both packaged) and
  fails on a `g+w` line mentioning `energy_uj`, because nothing else in the
  grant table expresses the distinction.
- **The expanded telemetry (2026-08-14) matches the z13ctl-plus/z13gui-plus
  data set, through the daemon.** GPU edge temp, busy %, sclk, GFX power and
  UCLK (amdgpu sysfs + a pure gpu_metrics v3.0 parser at offsets 124/186), CPU
  utilisation counters + average core clock + system memory (procfs/cpufreq),
  VRAM carveout, and NPU power/util/clock (amdxdna DRM ioctls, layouts ported
  from the -plus fork, decoded at explicit offsets because 168-byte records
  misalign struct casts). The forks read sensors live inside get-state with no
  history; ours flow driver `Sample()` → 1 Hz sampler → ring → wire, so every
  quantity charts. Three rules earned their comments: (1) **the NPU is queried
  only while `runtime_status` reads active** — opening the accel node resumes
  a suspended NPU, so an unconditional 1 Hz query would pin it awake forever, a
  power cost imposed by the power graph; a suspended NPU reports *zeros with
  Known=true*, since suspended genuinely means drawing nothing and a chart
  that gapped whenever the NPU slept would look broken on every machine not
  running inference. (2) CPU utilisation crosses the boundary as **cumulative
  jiffie counters** on the energy-counter pattern (`cpuUtilPct` in the sampler,
  `prevJiffies` owned by its goroutine); unlike energy it needs no gap ceiling
  — jiffies only advance awake. (3) Presence per field by what zero means:
  utils and GPU/NPU power are wire *pointers* (idle is genuinely 0), temps and
  clocks omit zero (never a reading). `telemetry.{gpu,cpu_stats,npu,net}` are
  declared in the device TOML and the declaration guard now holds all five
  sources both directions. `State.Telemetry` is the full live edge as one
  nested sample (freshness-bounded; derived rates grafted from the ring) so
  the every-quantity-live rule holds without a top-level field per quantity —
  the pre-2.0 named fields stay forever. New State pointer ⇒ `cloneState` got
  `cloneTelemetrySample`.
  **Network throughput (the eighth card; Jeff, 2026-08-14, picked to even the
  grid) is the same shape end to end**: `/proc/net/dev` byte counters cross
  the driver boundary cumulative (`netRateMBps` in the sampler, `prevNet`
  owned by its goroutine, `maxEnergyGap` ceiling — wall-clock interval, unlike
  jiffies), and the rates are wire pointers because an idle link's 0.0 MB/s is
  a reading. The part with judgement in it is *which interfaces count*:
  `ReadNetBytes` sums only interfaces with a `/sys/class/net/<name>/device`
  link — hardware-backed — because summing everything counts VPN and bridge
  traffic twice (once on the tunnel, once on the hardware beneath it), and no
  physical interface at all is an error, never a zero.
- **Package power crosses the driver boundary as an energy *counter*, not as
  watts.** RAPL publishes cumulative microjoules, so power is a difference over
  an interval — arithmetic that needs the *previous* reading, which a driver
  cannot hold: `Telemetry.Sample()` is called by the 1 Hz sampler *and* by every
  `get-state` handler, and drivers are passive by design (no locking, no
  goroutines). The counter therefore goes out as `Sample.PackageEnergyUJ` and
  the daemon's sampler — the one sequential caller — converts it in
  `packagePowerW`, a pure function with a table (`internal/daemon`). A device
  whose hardware reports instantaneous power instead (OXP's pm-table) fills
  `PackagePowerW` directly and the sampler leaves it alone.
  Three of that table's cases are the ones worth knowing: a **wrap** is real
  (262 kJ is ~73 minutes at 60 W) and must be added back rather than dropped; a
  **reset** is indistinguishable from a wrap by the values alone and would
  otherwise be reported as ~262 kW, which is what `implausiblePackageW` exists
  to catch; and a gap longer than `maxEnergyGap` yields nothing, because the
  average across a suspend describes no moment inside it. Every refusal records
  **no** power rather than zero — 0 W is a claim the package drew nothing, and
  a running machine never does.
- **Battery flow is the one telemetry quantity whose zero is a reading**, which
  is why `api.TelemetrySample.BatteryPowerW` is a `*float64` while every other
  field is a plain value with `omitempty`. A full pack on mains genuinely moves
  no energy — the commonest state a laptop is in — so testing the value would
  drop the chart from every plugged-in machine, and testing nothing at all
  would draw one flat at zero on a desktop with no pack. That is the same
  `*bool` reasoning `BatteryInfo.ChargeLimit` already carries. Inside the
  daemon it is a value plus `Sample.BatteryPowerKnown` rather than a pointer,
  because `driver.Sample` is stored in the history ring, which deep-copies in
  both directions precisely so a driver cannot alias what it handed over.
  The sign convention is positive = discharging, and `power_supply` does not
  carry it: the magnitude is in `power_now` (or `current_now` × `voltage_now`
  on a charge-reporting pack — the same both-forms split as battery health, and
  the Z13 has only the energy form) while the direction is in `status`.
- **The battery chart plots state of charge; the flow feeds the card header's
  rate and time estimate** (Jeff, 2026-08-14: the percentage is what you glance
  at a battery chart for). `Sample.BatteryLevelPct`/`Known` →
  `TelemetrySample.BatteryLevelPct *int` (zero is a reading — a flat pack — so
  the pointer, exactly the flow's reasoning) is the series, on a hard 0–100
  frame like Load's; `BatteryPowerW` stays on the wire untouched but no longer
  charts, because watts and percent cannot share an axis. The estimate is
  client-side and pure (`profileui.batteryEstimate`): get-state gains
  `battery_energy_wh`/`battery_energy_full_wh` (via `driver.BatteryStatus` —
  watt-hours on every machine, the driver converting a charge-reporting pack
  through `voltage_now`; a *pair*, since remaining without the pack size
  answers only the discharge half), and the header divides — remaining over
  rate to empty while discharging, gap-to-target over rate while charging,
  where the target is the charge *limit* when one is set because the pack
  genuinely stops there ("to full" would promise what the firmware prevents).
  Two suppressions are deliberate: rates under `minEstimateW` (a resting pack
  wobbles by tenths of a watt, and dividing by that claims false precision) and
  answers beyond a day (noise the floor did not catch) fall back to the plain
  state word. The estimate is instantaneous — remaining over the *current*
  rate — not smoothed; if that ever wants smoothing, the history ring is where
  the average lives, not a second counter in a handler.
- **`battery` and `telemetry` are document *sections*, not presence bools,
  because their contents are independently absent.** A machine can report state
  of health while exposing no charge-limit attribute, and the reverse — a
  single `battery: true` cannot say either, so a client rendering from it
  guesses. `BatteryConfig.ChargeLimit` is a `*bool` for the same reason absent
  and false must differ: every battery driver written so far exists to control
  the threshold, so a bare block means the usual thing and a device that only
  reads a level has to say so. A block declaring neither is a validation error
  ("drop the block instead"), matching the empty-`toggles` rule.
- **State of health crosses the driver boundary as a ratio, never as the pair
  it is computed from.** `power_supply` publishes either
  `charge_full`/`charge_full_design` in µAh or `energy_full`/`energy_full_design`
  in µWh depending on the battery driver — and the Z13's ACPI battery reports
  *energy*, where `driver.BatteryStatus`'s first draft assumed charge and named
  its fields `ChargeFull`/`ChargeFullDesign`. Only the ratio means the same
  thing on every machine, and it is the form a dashboard shows anyway, so
  `HealthPercent` is what the interface carries and
  `asusz13.ReadBatteryHealthPercent` tries both pairs. It is deliberately **not
  clamped to 100**: a freshly calibrated pack genuinely reads above its design
  capacity, and 103% is more useful than a number quietly adjusted to look
  plausible. The read is best-effort inside `Status()` for the same reason RPM
  is inside `Sample()` — a missing full-charge attribute must not make the
  charge level unreadable — and it is gated on the declared capability, so a
  device whose data does not claim health reports zero even where the
  attributes happen to exist.
- **`telemetryring` timestamps with wall-clock time and carries its own lock,
  and both are deliberate.** The ring stores the timestamp its caller supplies,
  because it cannot know whether the caller means wall-clock or monotonic and
  the two answer differently across a suspend: Go's monotonic clock does not
  advance while the machine is asleep (the same fact `reconcileSuspendMaxTicks`
  is counted in *ticks* for), so a ten-hour suspend would look like no elapsed
  time and the pre-suspend samples would sit inside the window forever. The
  daemon passes wall-clock, and the ring is written to survive what that costs —
  a backwards clock step cannot make it panic, reorder its contents, or drop
  anything it was not asked to. The lock is an exception to "serialization lives
  in the daemon" that is safe because it is never held across a call out: it
  guards a slice copy, so it cannot participate in the `hwMu` → `d.mu` order.
  A fifth daemon lock taken by a watcher *and* by socket handlers is the shape
  that produced the `saveState` race. Every value crossing the boundary is
  deep-copied in both directions, because `driver.Sample` carries an RPM slice
  a driver is free to reuse.
- **The drawer's sections come from `internal/controls`, and the default list is
  a transcription of what shipped — not a fresh design.** `buildContent` used to
  answer "which sections exist", "in what order" and "what does each look like"
  with one literal run of `Append` calls. The quickbar M4 wants needs the first
  two as *data*, before any widget exists, so they moved to a pure package and
  `internal/gui` kept only the third. The acceptance test is that a user with no
  `gui.toml` sees the drawer they had before, which is why
  `TestDefaultOrderIsTheShippedLayout` reads as a transcription and should only
  ever change alongside a deliberate decision to move something.
  **`Layout` is part of that, not a convenience.** The heading/separator rule —
  a heading wherever the group changes, a separator before every heading but the
  first — is what the user actually sees, so it belongs where `make test` can
  reach it rather than in the build loop. Group is a field on the *control* so a
  reorder cannot strand a heading above the wrong section; interleaving two
  groups repeats the heading, which is the honest rendering of what was asked
  for.
  **The two halves of a control live in one struct (`controlBuilder`).** The
  build run and the focus-list run were two literal sequences nothing forced to
  agree, and a control present in one but not the other is either invisible or
  unreachable by controller — the second being the failure nobody notices with a
  mouse in their hand. Both now come from one map, walked in the resolved order.
  The footer is deliberately *not* a registry control: it is fixed chrome outside
  the scroll area, so "the things you can reorder" and "the things that scroll"
  stay the same set.
  `Requires` is a slice because autoswitch genuinely needs two capabilities —
  it selects profiles, and it fires on a power-source change the daemon can only
  observe through the battery capability (`acPower` returns unknown without it).
  A nil document means the daemon did not answer, not that the machine has no
  capabilities, so everything is kept — the same posture `limits.FromDevice`
  takes, and for the same reason.
- **The drawer's edge is `panelgeom.Edge`, and every anchor and margin write
  goes through one accessor.** `gui.toml`'s `[quickbar] edge` moves the drawer
  between the left and right screen edges; all three backends take it, and the
  geometry is pure — `Panel` aligns the rect, `HiddenX` says which way "away"
  is, `HasNeighbor` answers whether sliding off that edge would bleed onto
  another output. That last one is why this needed care rather than a flag:
  the layer-shell backend fades in place instead of sliding when a monitor sits
  beyond the edge (KWin does not clip a layer surface's overflow), and the check
  asked exclusively about the *right* until the drawer could move. An assumption
  like that survives a move silently and produces a drawer bleeding onto the
  monitor it used to be nowhere near. `shellEdge()` exists so there is no path
  that relocates the panel while leaving the animation driving the opposite
  side; gamescope has no rectangle at all, so its edge is the append order of
  backdrop and panel.
  **Top and bottom are refused with a message that says why.** They are not an
  anchor change — the drawer is a fixed-width column of stacked sections, so a
  horizontal edge means laying every section out along the other axis, which is
  a different panel rather than a moved one. `focusgrid.Horizontal` is already
  written and tested for the day that lands; until then `ParseEdge` names the
  reason instead of answering "unknown edge", and every parse failure still
  returns a usable edge so a bad config costs a warning and not a drawer that
  will not open.
- **Generic toggle rows needed two api additions; both landed, and the renderer
  is now the full window's Settings tab.** The roadmap had `internal/controls`
  rendering the firmware toggles (and later plugin features) from the device
  document instead of the bottom bar's two bespoke switches.
  (1) **`api.ToggleInfo.Description`** carries the prose those switches held as
  GTK literals. It is *device data*, in the TOML beside the label, because
  whether panel overdrive ghosts is a fact about the panel — a client rendering
  rows generically cannot derive it, and the alternatives were to drop the
  warnings or restate them per-id in every UI, the duplication
  `api.ValidateProfileName` exists to prevent. It crosses four hops (TOML →
  registry → `driver.ToggleSpec` → wire) and a drop at any one is silent, so
  `TestToggleDescriptionsReachTheWire` guards the path and
  `TestPanelOverdriveKeepsItsGhostingWarning` pins the consequence that
  motivated the field. The negative control was run: removing the registry hop
  fails both.
  (2) **`api.State.Features`** is every declared toggle's current value, keyed
  by the id `device-get` and the `feature` commands use, so a row learns its
  state from the `get-state` a client already makes rather than one round trip
  per toggle per sync. `BootSound`/`PanelOverdrive` remain forever as the fixed
  vocabulary pre-2.0 clients know, and are now filled *from the same reads*
  rather than by two hardcoded `Get` calls — a device with different toggles was
  previously undescribable. A toggle whose value cannot be read is **omitted
  rather than zero**: zero is "off", a claim about hardware, and a switch has to
  be able to show "I do not know" — the same rule as a failed telemetry sample
  being a gap. `readFeatures` returns nil (not an empty map) so `omitempty`
  keeps the key off the wire for a device with no toggles.
  A `Kind` field on `controls.Control` is still deliberately absent — see the
  next entry for where the renderer actually landed.
- **The Settings tab renders `DeviceInfo.Toggles` directly, not through
  `internal/controls`.** `internal/controls` lists the *drawer's* sections, and
  a firmware toggle is not one: the rows are device data, so a registry entry
  per toggle would be a second list to keep in step with the document. What
  does live in `controls` is `CapToggles`, so "does this machine have any"
  has one answer for every surface — and it is the odd capability out, since
  every other one is a nil-able document section while toggles are a *list*
  whose emptiness is the question. `internal/settingsui` holds the rules
  (`Rows`, `EmptyReason`) and `gui/settingsview.go` builds a label, a switch
  and a send; nothing in it knows what a boot sound is.
  Three rules earned their place. **Absent from `State.Features` renders as
  insensitive, never as off** — the daemon omits a toggle it could not read
  precisely so a failed read is distinguishable from "off", and reading absence
  as zero here would put that claim back one layer up; insensitive also means
  the gamepad grid skips it, which is the established rule for a control that
  cannot be operated. **An unrecognized `Kind` is skipped** (`api.ToggleKindBool`
  is the only one today): drawing an enumerated toggle as a switch would
  misrepresent it and writing to it would send a 0 or 1 to something that means
  neither. And **an empty page says which kind of nothing it is** — daemon not
  running, device has none, or a kind this build cannot show — because the
  three call for different responses from the user.
  **The drawer's two bespoke switches were then removed** (Jeff, 2026-08-14):
  BIOS settings nobody adjusts often, on the surface meant for the controls you
  reach for in a hurry. Keeping them was the first instinct — drawer/window
  overlap is the established shape for profiles and autoswitch — but those two
  are things you change *because* you are already in the drawer, and a POST beep
  is not. Nothing is lost under gamescope, where the full window is hosted in
  the same surface, so its Settings tab is as reachable as the bar was.
  That took `buildToggle`, both `*Switch` fields, `syncOverdrive`/`syncBootSound`
  and `sendOverdriveSet`/`sendBootSoundSet` with it — the last hardcoded
  per-toggle UI in the tree, which is the point: **the GUI now has exactly one
  toggle write path** (`sendFeatureSet`, by id) and no code anywhere that knows
  what a boot sound is. `api.SendBootSoundSet`/`SendPanelOverdriveSet` stay for
  the CLI and for clients written against them.
  It is also the **first deliberate change to the drawer's own focus-dump line**
  (`main` 42 → 40, footer three items → one); the other six lines were
  byte-identical across the move, which is what said the removal touched
  nothing else.
  One thing came free and is worth knowing: the switch styling was scoped
  `.bottom-bar switch`, so re-scoping it to `.drawer switch` rather than
  deleting it fixed the **autoswitch** enable switch, which had been wearing
  stock Adwaita colours on every theme since it was written.
- **The Telemetry tab became the Dashboard: charts *and* the live controls**
  (Jeff, 2026-08-14 — "our telemetry page is actually supposed to be a general
  use dashboard, not just for monitoring"). Profile, autoswitch, charge limit,
  refresh rate and RGB now sit under the tiles, and the tab's title changed to
  match. Its **ID did not** and must not: `dashboard` is the GTK stack child
  name, the name the focus dump logs the page under, and what will land in the
  user's config once the window remembers its last page.
  The dividing line against the Profiles page is **what a control changes**: the
  dashboard changes what the machine is doing now, the editor changes what a
  saved profile *says*. That is why **autoswitch moved here** — it was put on
  the Profiles page hours earlier on the reading that it selects profiles, and
  selecting a profile is a live act, not an edit to one. Power limits, the fan
  curve and the undervolt stay on the editor by the same rule.
  **The layout was corrected twice, and both corrections are the same lesson.**
  The first cut put the controls in a 320px rail down the left, reasoning that
  320 is the drawer's own width so every block would be used at the size it was
  designed for. Jeff: "you stacked the controls vertically again... we don't
  need it to be in the same format as the drawer. Adjust the layout so that it
  takes advantage of the larger window, and keep the telemetry tiles on the
  top." The second: with the tiles back on top and the controls across the
  width, they were still *drawer units* — a title over a full-width widget, a
  slider whose value floats over its handle, a 3×2 keypad of effect buttons.
  Jeff: "right now it consists mostly of units that match the drawer. Let's work
  to find the best possible UX for a window view like this." They are desktop
  form rows now (`internal/gui/formrow.go`), one setting per line with its name
  in a fixed label column — the boxed-list idiom, and the one the window's own
  Settings tab already used. Reusing a *view* across surfaces is the established
  win; reusing its *arrangement*, or its control shapes, is not — and all three
  are easy to conflate.
  The per-control decisions (why Custom takes an ellipsis, why a slider's value
  moves out of the trough, why brightness is named rather than numbered, why the
  effects are one row of six) are in `internal/gui/CLAUDE.md`, each with the
  thing it was getting wrong.
- **The window shows one lighting card per zone where the drawer shows one card
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
- **Every drawer section is now built twice, and three things had to stop being
  Window-level singletons for that.** `autoswitchSection` was already an
  instance; `profileSection`, `batterySection` and `lightingView` became ones
  (`newXSection` returns the instance and its widgets, `buildXSection` keeps the
  drawer's registration), and each Window-level sync walks both through a
  `xSections()` helper on the pattern `customViews()` set. The three that would
  have failed *silently*:
  (1) the RGB **swatch CSS ids**. Both current-colour squares are painted by a
  provider registered display-wide and keyed on `#color1-swatch`, so two
  instances editing different zones would have fought over one selector; each
  now carries an id prefix.
  (2) `Window.updateSwatches/sendApply/queueApply`. A preset click or an HSL
  slider has to reach *its own* section, and those wrappers reached
  `w.lighting` — the window's picker would have applied the drawer's zone.
  `colorInput` carries its `owner` instead and the wrappers are gone.
  (3) **`refreshState` had to grow `syncBattery` and `syncLightingSection`.**
  Both were synced only by `syncState`, the *drawer's* own fetch, so the
  window's copies would have shown whatever state was current when the page was
  built and never moved again. Identical in shape to the settings-page fix a day
  earlier, and the same rule: `refreshState` is the funnel, `syncState` is one
  surface's.
- **The HSL picker is a popup, not a page, and there is now exactly one of
  them** (`internal/gui/colorpopup.go`; Jeff, 2026-08-14). A page is the right
  answer in a 320px column — it *is* the drawer's way of handling anything that
  would otherwise want a window of its own — and the wrong answer in a window,
  where choosing a colour took away the page being configured and blanked the
  tab highlight to stay honest about a sub-page that is not a tab. The popup
  layer already solves this exact shape for dropdowns, so the picker uses it.
  **The unification is the point, not a side effect.** Keeping the page for the
  drawer and adding a popup for the window would have been two pickers, which
  is the one thing this codebase consistently refuses; one popup body, opened
  into whichever layer `activePopup()` names and routed to the right zone by
  `colorInput.owner`, deleted `colorview.go`, `mainWindow.{colorPicker,
  returnTab, ensureColorView, showColorView, leaveColorView, onColorPage}`,
  `Window.colorView`, the `colorPage` stack child, the `ActionBack` sub-page
  case and the `hide()` reset — and the four rules that non-tab stack child
  needed with them. `VOLTAIRE_GUI_OPEN_FULL=color` still opens it, now over the
  dashboard.
  Two things had to change underneath, and both are load-bearing:
  **(1) `closePopup` detaches the body** (`p.scroll.SetChild(nil)`). A dropdown
  builds a fresh list per open and never noticed; a shared body cannot be
  parented into the second layer while the first still holds it, so the drawer
  and the window would have worked one at a time.
  **(2) A popup body may not size itself — it asks the layer**
  (`popupLayer.minW`, `openPopupSized`). `popupgeom` clamps the *rectangle* to
  the panel while GTK sizes the *child*, and GTK never allocates below a size
  request: a `SetSizeRequest(380)` on the body overflowed the drawer's panel and
  was clipped at the overlay, taking the readout column and the hex with it —
  seen on hardware, and invisible in the window where 380 fits. As a floor
  passed to `popupgeom`, the existing clamp does the work and the body fills
  what it is given. Any future popup wider than its natural size wants the same
  treatment.
  **The sliders paint their own troughs** — hue through the spectrum at the
  current saturation and lightness, saturation grey→colour, lightness
  black→colour→white, all recomputed per change on the provider the preview
  swatch already used. That is the difference between a picker and three
  anonymous sliders. They carry `border: none` because the *desktop* theme's
  trough border survives voltaire's sheet (which sets `background` and nothing
  else) and is invisible only at GTK's default trough height; at the height a
  ramp needs it drew a 1px frame in Breeze's blue.
- **The refresh-rate control is the one GUI control that does not go through the
  daemon, and `internal/display` says why in its package doc.** A video mode
  belongs to the compositor, not to hardware voltaire owns; it is per-session;
  the compositor already persists it; and the daemon is a systemd user service
  with no guaranteed `WAYLAND_DISPLAY`, while a GTK client is by definition in
  the session. That is a deliberate, narrow exception to "the GUI is an ordinary
  socket client" — the rule exists to stop the *daemon* growing GUI-shaped
  shortcuts, not to stop the GUI talking to its own session.
  Capability is by absence, as everywhere else: one backend exists
  (`kscreen-doctor`), `Available()` is a PATH lookup rather than a query so the
  widget tree is not built behind a DBus round trip, and a machine without it
  gets **no card at all** rather than a control that could only ever fail. That
  covers gamescope for free.
  Two rules in the package earned their tests. **Resolution is held fixed** —
  the rates offered are those at the current one, because a list of every mode
  is a resolution picker wearing a refresh rate's label, and changing resolution
  under a running session is the compositor's own settings dialog's job (it has
  a confirmation timer; this control has no business reimplementing one). And
  **the label rounds while the value never does**: panels report 59.868 for what
  every other piece of software calls 60, so two modes a hundredth of a hertz
  apart collapse to one entry — with the mode that is *running* winning the
  collapse, or the control could not display the state it is in.
- **The refresh rate follows the power source, and that switch is the GUI's for
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
- **Every firmware-toggle write path must notify, and two of the three did
  not.** `handlePanelOverdrive` had updated state and called `saveAndNotify`
  since it was written; `handleBootSound` and the generic `handleFeature` did
  neither, so a toggle changed by any other client left every open UI showing
  the old position. That was invisible while the only renderer was the drawer's
  bottom bar, which resyncs whenever the drawer opens, and became visible the
  moment a settings page rendered rows from `State.Features`: the same switch
  updated live or did not, depending on which command wrote it. All three now
  go through `notifyToggleChanged`, whose *notify* is the load-bearing half —
  values reach clients through `get-state`'s live reads, so a client that
  re-reads sees truth; what it had no way to learn was that there was anything
  to re-read. The state write beside it is only the pre-2.0 named-field
  projection, kept so the saved file does not disagree with the machine.
  `TestEveryToggleWritePathNotifies` is a source check because the alternative
  is a hardware write, and **its first version passed with the call deleted**:
  each of those functions *mentions* `notifyToggleChanged` in a comment, so the
  check was reading prose and reporting it as code. `funcBody` strips comments
  now; the negative control is the only thing that caught it, and is the reason
  any source-shaped guard needs one.
- **Which surface each press opens is the *client's* choice, not the
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
- **A double press emits `gui-open-full` *in addition to* `gui-toggle`, and
  never instead of it.** The first press opens the quickbar immediately; a
  client that wants the escalation subscribes to both and hides whatever the
  toggle opened when the second arrives. The brief flash is the accepted trade
  for never adding the window's length to a single press — a daemon that waited
  to see whether a second press was coming would put 400 ms onto every press on
  the machine.
  **Both bounds are measured, and the lower one is the important one.** Some
  firmware revisions report a single Armoury Crate press twice in the same evdev
  instant — the duplicate `internal/togglegate` exists to swallow client-side.
  Without a floor, *every* press on that hardware is a double press, so the full
  window opens every time and the quickbar becomes unreachable. 50 ms is
  togglegate's own figure, from the same hardware. The ceiling sits above the
  measured 129 ms human tapping floor so a deliberate double tap (200–300 ms)
  pairs, and below the gap between two unrelated presses.
  A completed pair clears the state, so a triple tap escalates once rather than
  on every press after the first. The press *timestamp is taken in the watcher*,
  not where `buttonCh` is read: the reader can be held up broadcasting to a slow
  subscriber, and two presses queued behind one of those would be read back to
  back and misread as a hardware duplicate.
  Nothing consumes `gui-open-full` yet — the full window it is meant to open
  does not exist. It is additive and subscription-filtered, so no existing
  client sees it.
- **`gui-open-full`'s consumer is a second GTK surface, and the views it shows
  are second *instances* rather than a second implementation.** The drawer and
  the full window both host `dashboardView` and `customView`; what differs is
  carried in a three-field `viewHost` (how to leave, whether this page is on
  screen, which error bar), because those are the only three things that
  differ. A view that reads `w.viewStack` — the drawer's — is a view that
  cannot live anywhere else, and every such read is now a `host.current()`
  call. Making that true needed the constructors to *return* the view instead
  of assigning `w.dashboard`, and the focus lists to move onto the view; both
  were already how `colorView` and `themeView` worked, so the split was
  finishing a pattern rather than inventing one.
  Three Window-level things had to widen with it, each because it had been
  written against the drawer alone: the gamepad reader's gate and the get-state
  poll's (now `anyVisible`, or the window opens with a dead controller and
  frozen readouts), and the error bar — there is one per *surface* now, and
  `reportError` fans out, because the drawer's bar is hidden whenever the window
  is up and a failure reported only to it is invisible. That is issue #14's
  shape reintroduced by a second surface.
  **Gamescope hosts the full window inside its own surface, and the claim that
  it could not was wrong.** What does not composite there is a second
  *toplevel*: only one window carries `STEAM_OVERLAY` and
  `GetPossibleFocusWindows()` skips `isOverlay`-flagged windows. That is a fact
  about second windows, not about screen space — the gamescope backend's window
  is *already fullscreen*, so the full window there is a different **layout of
  a surface we already own**. HHD is the existence proof and the same shape:
  its sidebar and its larger settings view are one Electron surface
  re-laying-out its contents, which is also why its menus work where
  `GtkDropDown` does not.
  `fullSurfaceHost` (`backend.go`) is the seam — `SetFullChild` + `ShowFull`,
  implemented by gamescope alone. It is an *optional* interface because
  layer-shell and overlay have nothing to implement: they use the real toplevel,
  which is the better surface where it works, and two no-op methods would
  suggest a choice where there is none. `Window.fullHost()` is the only place
  that asks, so `mainWindow.win` being nil is confined to a handful of guards.
  Two things are load-bearing. The stack sits **above** the 320px panel: the
  drawer's own view stack lives *inside* it, so a page added there would be a
  320px "full window" — that part of the old note was right. And `ShowFull(true)`
  with no installed page is **ignored**, because switching a `GtkStack` to a
  missing child leaves the surface blank with no way back, and a blank
  fullscreen overlay over a running game is the worst failure that file can
  produce.
  The hosted path inverts the drawer handling: `openFull` *shows* the drawer
  first (the surface must be up before a page inside it can be), where the
  toplevel path hides it (a separate window replaces it).
  **The drawer's dashboard view is gone with it.** It existed only as the
  gamescope fallback, and with a real full window there it had no caller —
  dead code that reads as a fallback is worse than either option. This also
  matches the standing rule that the dashboard belongs to the full window
  (Jeff, 2026-08-13): the drawer is quick controls, and a chart at 320px is not
  one. `VOLTAIRE_GUI_DUMP_FOCUS` diffed to exactly one removed grid
  (`view=dashboard`), every other list byte-identical including both full-window
  pages. **The gamescope path itself is unverified — there is no Gaming Mode
  session on the development machine — so it needs a hardware pass before
  release.**
- **The telemetry sampler stands down while suspending for a *different reason*
  than the other watchers, and the difference is load-bearing.** `reconcileTick`
  and `powerTick` stand down because they **write hardware**, and a write landing
  between `PrepareForSleep(true)` and the freeze undoes the fan release that lets
  the EC stop the fans overnight — the `powerSourceOnce` bug. The sampler writes
  nothing, so it cannot do that harm; it stands down because the samples would be
  *misleading*. The pre-sleep release has already lowered the PPT and handed the
  fans to firmware auto, so a sample taken there records the released machine and
  the graph shows a thermal cliff seconds before a suspend that explains nothing.
  Cosmetic, not a safety property — but the graph is the whole feature. Stating
  it as "same lesson as powersource" would be wrong in a way that matters if
  anyone relaxes it.
  It needs **no staleness ceiling** for the same reason: `reconcileTick` counts
  awake ticks against `reconcileSuspendMaxTicks` because a lost
  `PrepareForSleep(false)` would leave the fans undefended forever, whereas here
  a lost resume costs a gap in a graph until the next suspend clears the flag,
  and re-arming on a guess would put samples back exactly where they are least
  trustworthy. `sampleOnce` also takes neither `hwMu` nor `d.mu` for the read
  itself, on the same grounds `*-get` handlers do not: blocking the dashboard's
  data behind a fan write sequence is the regression, not the protection.
- **`get-state` is the *live* edge of the same series `telemetry-history`
  plots, and it must carry every quantity the device measures.** The roadmap
  puts live updates on the existing 1 Hz `get-state` poll and history on the
  ring, so the two are one feature seen at two timescales. That was half-built
  for a release: `Sample()` reported temperature, both fans, the energy counter
  and battery flow, and `handleGetState` published `Temperature` and `RPM[0]` —
  a dashboard charting four series beside readouts showing two. `RPM []int`,
  `PackagePowerW` and `BatteryPowerW` close it; `FanRPM` stays forever as
  `RPM[0]` because every pre-2.0 client reads it. Quoting one of two fans that
  cool the same die describes neither — the Z13's differ by hundreds of RPM.
  **Package power is the one quantity a handler may not derive.** It is a rate,
  so on counter hardware it needs the previous reading and the interval since;
  `d.prevEnergy` belongs to the sampler's goroutine and is unguarded *because*
  that goroutine is its only writer, so re-baselining it from a socket handler
  would be both a data race and a corruption of the series being drawn. The
  handler serves the sampler's most recent figure instead, which is also the
  only way the live readout and the right-hand edge of the chart can agree —
  two numbers for one quantity that disagree is indistinguishable from a bug.
  It is served **absent rather than stale**, bounded by `liveTelemetryMaxAge`.
- **A battery flow figure is meaningless without the pack's state beside it,
  and shipping the number alone was reported as a broken sensor.** On a machine
  with a charge limit the commonest reading is the confusing one: a pack resting
  *above* its end threshold on mains is neither charging (it is over the limit)
  nor discharging (mains is attached), so `power_now` is exactly 0 — correct,
  and indistinguishable from a dead reading. Measured on this machine: limit 74,
  level 81, `status` "Not charging", and UPower independently agreeing at
  `energy-rate: 0 W`, `state: pending-charge`. `get-state` was already reading
  `BatteryStatus.Capacity` on every request and **throwing it away**, so no
  client could show the level either. `battery_level` and `battery_state` close
  it; `battery_limit` remains the *setting* and is a different number from
  `battery_level`, the reading.
  `not-charging` is deliberately **not** folded into `full` even though both
  mean no flow — the pack is not full, it is being held back, and that is the
  entire explanation for the zero. `mapBatteryState` is the one place that knows
  power_supply's `status` vocabulary, so the wire state and `signedByStatus`'s
  sign convention cannot drift.
  **`batteryStateIn` also returns whether `status` could be *read*, which is not
  the same as reading it and finding "Unknown".** Both are `BatteryStateUnknown`
  to a client, but an unreadable file leaves `signedByStatus` guessing
  "discharging" (the direction that matters, on a machine running off the pack)
  while an explicit answer means no flow. Merging the two during this refactor
  silently changed what a flow reading meant, and `TestReadBatteryPowerW` caught
  it.
- **`freshEnough` compares Unix seconds, and the reason is that the hazard it
  guards cannot be tested.** Ring timestamps and `time.Now()` both carry
  monotonic readings, which `Sub`/`Since` prefer — and Go's monotonic clock is
  `CLOCK_MONOTONIC`, which does not advance across a suspend (the same fact
  `reconcileSuspendMaxTicks` is counted in ticks for). A monotonic comparison
  serves a pre-suspend figure as the current draw for the second between a
  resume and the sampler's next tick. The first fix was `at.Round(0)`, which is
  correct and undetectably fragile: **the negative control was run, and a test
  named for the wall clock passed with the safeguard deleted**, because
  `time.Now().Add(-10*time.Hour)` moves both readings together and nothing
  in-process separates them. `Unix()` has no monotonic path to remove by
  accident. The cost is second granularity against a three-tick bound on a 1 Hz
  sampler, which is nothing. `TestFreshEnoughBound` covers the window and says
  in its own comment that it does *not* cover the hazard, so a green run is not
  mistaken for evidence.
- **A failed sample is a gap, not a zero, and the wire carries timestamps so a
  client can see it.** `api.TelemetrySample.At` is Unix seconds and samples are
  **not evenly spaced** — the sampler stands down across a suspend and skips a
  failed read — so a client that plots against the array index draws a suspend
  as if no time passed. Recording a zero instead would be worse than the gap: 0°C
  reads as a measurement.
- **`telemetry-history` is a typed `history` field, not JSON stuffed into
  `value`.** `Value`'s embedded-JSON convention exists for the commands that
  predate a structured reply; `device-get` established the typed field, and
  double-encoding hurts most at exactly this size — a full window is 300 samples
  whose every quote would be escaped to travel as a string. Sending it also
  raised `api.sendCommand`'s reader ceiling: `bufio.Scanner` defaults to 64 KiB,
  which every command fitted under until this one, and a device declaring an hour
  of history would answer with 3600 samples and fail as "token too long" — a
  failure that looks like a broken daemon and depends on device data. The ceiling
  is raised to 4 MiB rather than removed, since it is what stops a wedged daemon
  growing the client's memory without bound.
  The **wire default differs from the ring's on purpose**: absent `seconds` means
  the whole retained window, while `telemetryring.Since` refuses a non-positive
  duration. The ring cannot tell a caller that meant "everything" from one that
  failed to parse its own field; the protocol can, and every other read command
  here answers with all it has when given no argument.
- **The dashboard's three rules live in `internal/telemetryplot`, and each is
  there because the obvious alternative draws something false.** (1) Points are
  placed by **timestamp**, never by slice index — the samples are not evenly
  spaced, so an index-placed plot renders a ten-hour suspend as though no time
  passed. (2) A gap **breaks the line**; interpolating across one draws a smooth
  ramp through hours nobody measured. `DefaultMaxGap` is 5 s — four consecutive
  misses — and is deliberately generous, because the gap worth showing is a
  suspend while a single failed sysfs read is noise, and a threshold tight enough
  to catch one would fragment the chart on any machine with a flaky sensor.
  (3) A quantity **no sample carries produces no series at all**: the same
  honesty rule as `TestTelemetryDeclarationMatchesWhatIsRead`, one layer up.
  For most fields the wire cannot distinguish absent from zero (they are
  `omitempty`), so presence is decided per field on what a zero would *mean*:
  0°C is not a plausible APU temperature and reads as absent, while 0 RPM is a
  stopped fan and is a reading — hence the RPM **slice's** presence is the test
  there, not its value. That asymmetry looks like an inconsistency until you
  know why, so `TestZeroMeansAbsentForTemperatureButNotForFans` pins it.
  Battery charge is the case where per-field cleverness ran out and the *wire*
  had to change: its zero is plausible (a flat pack), so it is a pointer and
  presence is the pointer — first established for the flow this chart plotted
  before it became state of charge, and inherited by the level for the same
  reason. Testing the value instead would drop a dying machine's chart at the
  moment it matters, and testing nothing draws one flat at zero on a desktop
  with no pack — the exact false measurement rule (3) exists to prevent
  (`TestBatteryZeroIsAReadingButAbsenceIsNot`).
- **The axis is framed per *kind*, not per series, and the nominal frame is a
  starting point that expands rather than a clamp.** Two fans are drawn on one
  chart, so separate axes would make their line heights incomparable — which is
  the one thing a viewer will use them for. The nominal ranges (30–100 °C,
  0–6000 RPM, 0–60 W package, 0–100% battery charge) keep the axis steady while
  values wander, because a chart
  that rescales every second at a 1 Hz refresh is unreadable; anything outside
  expands the frame to a step boundary, so no reading is ever cut off. That
  invariant is what makes it safe to carry one laptop's numbers in a package
  meant to serve every device, and it is a test
  (`TestTheAxisNeverClipsAReading`) rather than a comment.
  `Plot.Shape()` is the rebuild key: chart widgets are torn down only when the
  *layout* changes (a fan stops being reported, a power source appears), never
  when the values do — the same reasoning as the profile selector's signature.
- **The dashboard polls `telemetry-history` only while it is the visible view.**
  It is a separate loop from `startTelemetryPolling`, which reads `get-state` for
  the header's live numbers on every view: history is a different command and a
  much larger reply, so running it drawer-wide would be a per-second
  few-hundred-sample round trip nobody is looking at. Every path that leaves the
  view calls `stopDashboardPolling`, including `hide()`, which has no view switch
  to be caught by the tick's own visible-child guard.
  Failures are silent for the same reason the telemetry poll's are — a background
  refresh the user did not ask for must not repaint the error bar every second —
  but the *empty* state names which kind of nothing it is: daemon not running,
  daemon too old, or no readings yet. Any error reads as "too old", on the same
  grounds as `probeStoredTarget`; over-claiming is cheap here only because the
  refresh runs every second, so a transient failure shows it for one tick.
- **`internal/apiresult` exists because "the daemon is not running" is not an
  error.** Every `api.Send*` returns `(handled bool, err error)`, where
  `handled == false, err == nil` means the dial failed — a CLI caller falls back
  to direct hardware there, but the GUI has no such path, so it collapses the
  pair into one error with a `ErrNotRunning` sentinel and a message naming
  voltaire. Turning the pair into an error at each call site is how a control
  reports success on a write that never happened (issue #14).
- **`VOLTAIRE_GUI_DUMP_FOCUS=1` exists because the drawer's focus lists were the
  one checkable thing no test could reach.** `internal/gui` needs GTK4 headers,
  so `make test` cannot compile it, and every view but the main one is built on
  first navigation — so four of the five gamepad grids existed at runtime only
  after a person tapped a button, and the parity tests in `internal/focusgrid`
  could say the coordinates were right without saying they described the widgets
  actually built. The flag builds every view at startup and logs all five grids
  as `row:col:section`, giving a five-line fingerprint of the whole drawer that a
  refactor can be diffed against. It is what made the M4 window split verifiable
  rather than merely careful: each view moved out of `Window` with the dump
  byte-identical to the baseline. Any future change that touches widget
  construction or navigation should capture it first. It now covers the full
  window too, logging its pages as `full:<tab>` — a change that left the
  drawer's grids untouched and broke the window's would otherwise pass the
  diff. Its sibling `VOLTAIRE_GUI_OPEN_FULL=1` opens the full window at
  startup, and exists for the same reason: that window is otherwise reachable
  only by double-pressing a key on one laptop, so nothing about it could be
  checked while it was being written.
- **The GUI's environment variables renamed with a fallback:
  `startup.GUIEnv(suffix)`** reads `VOLTAIRE_GUI_<suffix>` and falls back to the
  pre-rename `Z13GUI_<suffix>` when the new name is empty — same 2.x contract as
  the socket path, removed at 3.0. The helper lives in `internal/startup` (pure,
  tested) rather than at the call sites, which are in the untestable cgo island;
  a new GUI env var must go through it, not `os.Getenv`. A non-empty new name
  wins; both variables treat non-empty as their active state, so set-to-empty
  needs no distinguishing.
- **The 2.0 GUI migrations are copies, on the same terms as the daemon's.**
  `theme.MigrateFromZ13gui` copies `~/.config/z13gui/*` to
  `~/.config/voltaire/*` **only while the voltaire directory does not exist at
  all**, and never moves: the old directory staying intact is what makes a
  downgrade to 1.x safe for the whole 2.x line, and the existence check means it
  can never overwrite something saved since. Only top-level regular files are
  copied — everything the 1.x GUI ever wrote. It must run **before anything reads
  config**, which is why it is the first statement in `main`, ahead of
  `--print-theme` and any GTK call.
- **The profile name rules live in `api` (`ValidateProfileName`,
  `MaxProfileNameLen`); `cli.ValidateProfileName` is a delegating wrapper.**
  They moved for the drawer's inline name entry: the GUI may not import
  `internal/cli` (the binaries share only `api` and `internal/version`), and a
  second copy of the rules would drift from the one the daemon refuses with.
  Any client-side pre-check must call the api function, never re-state a rule.
- **`State.SourceKnown`: `on_ac` false with `source_known` false means
  *unknown*, never battery.** `cli.OnACPower` errors when no Mains supply
  exists (VM, desktop), and get-state used to flatten that to `on_ac: false` —
  indistinguishable from running on battery. Clients must claim nothing when
  it is false (`profileui.PowerLabel` returns ""), and a pre-2.0 daemon omits
  the field, so old daemons read as unknown by construction.
- **The custom profiles live in the custom view, not the main view.** The main
  view offers the three firmware profiles and one `Custom` button standing for
  the whole family (`profileui.StockRows` / `profileui.Custom`); the custom
  view's selector offers the profiles themselves (`profileui.CustomRows`). One
  row per saved profile pushed RGB and battery off the bottom of a 320px
  drawer — with the split, the entire main view fits on screen without
  scrolling, which is the property to preserve when adding to it. The `Custom`
  button is labelled with the *running* custom profile, so the main view still
  says what is in force without listing anything.
- **The profile selector expands in the flow of the view; it is not a
  `GtkDropDown`.** It reads as a dropdown — one row collapsed, `▾`/`▴`, the
  current target highlighted — but a real dropdown's popup is a separate
  window that gamescope does not composite, so Gaming Mode would be choosing
  from an invisible list. Expanding in place also means the rows are ordinary
  widgets the existing focus grid navigates via `isVisible`, with no second
  mechanism for a popup's contents. Same reasoning as the autoswitch cycle
  buttons and the inline name entry; `grep Popover internal/` must stay empty.
- **The selector is rebuilt only when `profileui.Signature` changes**;
  highlights, sensitivity and tooltips move on the existing widgets otherwise,
  so a background state refresh cannot tear buttons out from under the
  pointer. Selecting a profile in it only re-targets the editor —
  **`Activate` is what applies it** — and every refusal reason a tooltip shows
  comes from `profileui` (`ActivateBlock`, `DeleteBlockFor`, `SaveAsBlock`,
  `CreateNameProblem`), mirroring the daemon's own text so the tooltip and the
  error agree. Delete sits at the bottom of the editor with a two-tap arm
  rather than a confirm dialog, for the same no-popup reason.
- **The autoswitch target rows are shown only while autoswitch is enabled.**
  They are meaningless when it is off, and this is the main view, where three
  permanent rows for a feature most users leave alone is the crowding the
  profile list was moved out to avoid. Enabling with both sides unset is
  harmless — "leave alone" on both sources is a no-op — so nothing is lost by
  configuring after enabling.
- **The editor addresses its target through `profileui.PlanEdit`, and every
  stored-target send is preceded by `probeStoredTarget`.** A plan is live —
  bare sends, hardware applied — in exactly two cases: the target is the
  active profile, or it is "custom" while a firmware profile is active (the
  drawer's historical create-and-activate flow, kept byte-identical for old
  daemons). Everything else goes through the `...For` variants and stores
  only; the probe (SendProfileList, exactly `cmd.ensureProfileTargetSupported`'s
  trick) is what keeps a pre-1.3 daemon from silently applying the edit to
  the running machine behind an `ok`. The plan is resolved per operation, not
  stored — the active profile can move underneath an open editor. The fan
  floor is evaluated against `editorFloorPL1` (live PL1 for a live target,
  the profile's *own* saved TDP for a stored one — hardware says nothing
  about a profile that is not running), and `ForEditor`/`CurveToShow` decide
  what the widgets display, including that a stored curve is adopted on
  presence while a live one requires `FanCurveIsCustom`.
- **The mouse wheel scrolls the drawer even over a slider
  (`Window.wheelScrollsView`).** `GtkRange` consumes scroll events to adjust
  itself, and the drawer is a tall scrolling panel that is mostly sliders, so
  a wheel flick passing over one silently changed a hardware setting instead
  of scrolling — found on hardware, where scrolling past PL3 moved it from
  90W to 30W. A **capture-phase** `EventControllerScroll` is what makes this
  work: it sees the event before GtkRange's own bubble-phase handler, so
  consuming it there is what stops the range acting, and the scroll is then
  applied to the enclosing scroller by hand. The step is a fraction of
  `PageSize`, not the adjustment's `StepIncrement` (a couple of pixels, which
  moved the view almost not at all) — page-relative also keeps the feel
  identical under gamescope, where every dimension is scaled. Returning false
  when there is no scroller leaves the colour picker's sliders, in the one
  view that does not scroll, answering the wheel as before. Every new
  `gtk.Scale` must be passed through it.
- **The gamepad focus grid skips *insensitive* widgets, not just hidden ones.**
  `gtk_widget_activate()` does not consult sensitivity — it emits the activate
  signal, which GtkButton turns straight into `clicked` — so the controller
  path could fire controls a pointer physically cannot. Every desensitized
  control in the drawer is desensitized because the daemon would refuse it, so
  this presented as an error bar for controller users where mouse users get a
  greyed button and a tooltip. Caught on hardware: a gamepad reached Delete on
  the *active* profile and got `profile-delete: gaming is the active profile`.
  The check lives in `focusItem.visible()`, whose result feeds
  `focusgrid.Item.Visible`, so the pure navigation logic needs no change and
  treats these exactly as it already treats hidden items; `activateOrEdit`
  re-checks, because a state refresh can desensitize the focused widget after
  focus landed on it. Skipping is deliberately not "focus but refuse": a
  controller cannot read a tooltip, so a focusable-but-dead item says nothing.
- **The autoswitch UI uses cycle buttons and a debounced single send.**
  Cycle-on-tap instead of a dropdown for the standing gamescope reason;
  empty custom profiles are unusable targets even though the daemon accepts
  them (a target that fails at every transition is a trap) — shown **greyed
  with an "(empty)" label** rather than hidden, because silently omitting them
  read as "my profiles aren't offered", a broken list rather than a rule
  (`profileui.TargetRows`; `TargetOptions` remains the selectable subset, and
  the two are derived from one function so they cannot disagree; Jeff,
  2026-08-14); and the 300ms debounce is about ordering, not chattiness —
  per-click goroutines can land on the daemon out of order and store an
  intermediate choice.
- **`BuildThemeCSS` defines every token twice — `@z13-*` and `@voltaire-*` —
  which is what let the token rename be *staged*.** The generated
  `@define-color` block is prepended to the bundled `theme-default.css`.
  Emitting only the new names while that sheet still referenced the old ones
  would have dropped every rule using them and left the drawer unstyled, so
  defines and references could not flip in the same commit: both names first,
  bundled sheet second. **The sheet is migrated** — it defines and references
  `@voltaire-*` throughout — so the `@z13-*` aliases now have no in-tree
  consumer at all and 3.0's removal is a pure deletion.
  `TestLegacyAliasesAreStillEmitted` is what makes that deletion deliberate
  rather than accidental, since nothing else would notice them going.
  Note the asymmetry that makes the whole thing safe: a user's `theme.css` is
  loaded **verbatim** (it supplies its own defines and never sees ours), while
  a `theme.toml` is substituted into the bundled template — so the alias only
  ever affected the template path, and a hand-written sheet on the old names
  keeps working because it never depended on ours.
- **The token regexes must match *both* prefixes, and widening them was the
  load-bearing half of the migration.** `definePattern` and `referencePattern`
  were `z13-`-only, which was correct while that was the only name and would
  have silently ended the self-containedness guard the moment the sheet moved:
  a `@voltaire-*` token referenced but not defined would have matched nothing,
  and `UndefinedColorTokens` would have reported a clean stylesheet. That is
  precisely the bug the guard exists for — the template shipped for months
  referencing `@z13-error` without defining it — reintroduced by making its own
  regex stop seeing the names in use. Widening them found the real gap
  immediately (the migrated sheet defined none of what it now referenced).
  **There is no runtime signal to fall back on.** GTK4 drops a rule with an
  unresolvable colour without logging anything — checked by breaking a token
  deliberately and watching a full `--debug` run stay silent — so the symptom
  is a wrong-coloured border, not an error. `TestTheComposedSheetHasNoUndefinedTokens`
  therefore checks the sheet the app actually loads (defines plus the
  stripped template), because the two halves each passing their own test is not
  the same as the composition being sound.

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
- **`make test`/`race`/`cover` run `HERMETIC_PKGS`, not `./...`.** That is
  `go list ./...` minus `internal/gui` and `voltaire-gui`, **plus
  `internal/gui/gamepad/...` added back**. `./...` *compiles* everything it
  lists, which would drag GTK4 headers and cgo into a run that must work on any
  machine and in CI without them. The boundary is why every rule worth testing
  on the GUI side lives in a pure-Go package (`limits`, `lighting`,
  `profileui`, `theme`, `colorconv`, `focusgrid`, `keyrepeat`, `panelgeom`,
  `telemetryplot`, `popupgeom`, `uiscale`, `togglegate`, `startup`,
  `apiresult`) rather than in `internal/gui` — adding logic to the cgo island
  puts it beyond every test.
  **The boundary is cgo/GTK, not the path**, and writing it as a path cost a
  real test. The exclusion is a substring grep, so it swallowed the whole
  subtree — including `gamepad` and `hidblocker`, which are evdev and ebpf and
  compile fine under `CGO_ENABLED=0`. `hidblocker_test.go` existed all along and
  had never once run, and the gamepad classification tests ported from z13gui
  would have been skipped the same way. That is the exact failure mode the
  upstream fix was written about: an untestable classifier is how an unqualified
  multitouch rule reached a release. If a package under `internal/gui` compiles
  cgo-free, it belongs in the run.
  `make lint` deliberately runs the **full** tree, GTK island included; it is a
  local/dev gate where the headers are present.
- Coverage, measured 2026-08-14: cli 84%, device 70%, asusz13 67%, aura 65%,
  safety 64%, hid 43%, daemon 42%, api 35%, cmd 8%. Every pure package on the
  GUI side — `limits`, `profileui`, `telemetryplot`, `focusgrid`, `mainwin`,
  `popupgeom`, `controls`, `colorconv`, `startup`, `display` — sits at 91–99%, which is
  the whole argument for the cgo boundary: logic moved out of `internal/gui`
  gets tested, logic left inside it cannot be.
  Re-measure with `go test -cover ./...` (plus `cd api`) rather than trusting
  these. The previous set in this file had drifted 13 points on aura and 6 on
  api, in both directions — a stale number here reads as a measurement, which
  is the same failure as a chart drawn flat at zero.
- aura error branches (write failures) are not covered because mockWriter never errors.

### Fake sysfs (`internal/drivers/asusz13`)

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

**Never let a test reach a real sysfs write.** `internal/daemon` handlers reach
hardware through the assembled `device.Device`, and `asusz13`'s path vars are
unexported, so daemon tests must stay on validation/rejection paths that return
before any hardware access — `server_test.go` documents this at
`TestHandleTDPForceBoundaryRejections`. A daemon test that gets past
`handleTDP` validation will change the machine's actual power limits.
`deviceinfo_test.go` is the exception and says why: `deviceInfoFor` is a pure
projection of constructor data, so it can drive the real Z13 assembly safely.

## Build / release

```sh
make build              # go build voltaire (CGO_ENABLED=0), version from git tags via ldflags
make build-gui          # go build voltaire-gui (CGO=1; needs gtk4 + gtk4-layer-shell headers)
                        #   → voltaire-gui/voltaire-gui
make build-all          # both — what `make install` wants (see below)
make test               # go test over HERMETIC_PKGS (both modules; no cgo)
make race               # same set under -race
make cover              # test + coverage report
make fmt-check          # fail if any file needs gofmt (generated bpf2go bindings excluded)
make lint               # fmt-check, then golangci-lint over the FULL tree (both modules)
make mod-tidy           # go mod tidy for all modules (main + api/)
sudo make install            # install pre-built binaries to /usr/local/bin (+ z13ctl/z13gui symlinks)
make install-service         # install + enable all three user units (socket, daemon, GUI)
make uninstall-service       # stop, disable, and remove systemd user units
sudo make install-perms-service    # install system oneshot service for sysfs permissions on boot
sudo make uninstall-perms-service  # remove system permissions service
make snapshot           # goreleaser release --snapshot --clean  (no publish)
make release            # goreleaser release --clean             (requires pushed v* tag)
make docs               # serve the website locally (pnpm install + astro dev)
make docs-build         # build the website as CI does
make docs-api           # regenerate website/.../reference/api-go.md from api/ doc comments
make clean              # remove both binaries, dist/, coverage artifacts
```

Version is injected at link time:
`-X github.com/dahui/voltaire/v2/internal/version.Version={{.Version}}`.
The single `internal/version.Version` var serves every binary (there is no
separate cmd.Version); its source default `"2.0.0-dev"` is used only in local
builds without ldflags.

## goreleaser

Config: `.goreleaser.yml`. GitHub Actions workflow: `.github/workflows/release.yml`.
- Builds for `linux/amd64` only (hidraw is Linux-specific; Z13 is x86_64).
- **Two builds, but ONE archive and ONE package.** `voltaire`
  (`CGO_ENABLED=0`) and `voltaire-gui` (`main: ./voltaire-gui`,
  `CGO_ENABLED=1`) are two binaries of one application, shipped together.
  Splitting them into two packages was the plan's original wording and is
  **wrong for the stated goal**: the whole reason the projects merged is that
  the users who most need the GUI never discovered it as a separate install, so
  a separate package preserves exactly that problem. One package also makes
  CLI/GUI version skew structurally impossible. The cost is gtk4 +
  gtk4-layer-shell as hard dependencies of every install (deb names differ:
  `libgtk-4-1`, `libgtk4-layer-shell0`) — the right trade on a laptop that has
  them already, and a headless install can take the tarball and ignore the GUI
  binary, which pulls in nothing until it is run. The package `provides`,
  `replaces` and `conflicts` **all four** pre-2.0 names, and ships
  `LICENSE-Inter.txt` for the embedded typeface (the OFL requires the notice to
  travel with the font).
- **`nfpms` entries need an `ids:` filter when there is more than one build.**
  Without it nfpm takes *every* build, which is how the CLI package came to
  ship `/usr/bin/voltaire-gui` while a separate gui package owned the same
  path — a hard file conflict on all three formats, so installing the
  documented pair failed outright. Moot now that there is one package (it wants
  both builds), but any future second package must carry `ids:`.
- **The pre-2.0 command names ship as symlinks: `/usr/bin/z13ctl` →
  `voltaire`, `/usr/bin/z13gui` → `voltaire-gui`.** The CLI was otherwise the
  one interface with no compatibility path — the socket, state file, config
  directory, env vars and unit names all have their own — so a rename would
  have silently broken every user script on upgrade. `cmd.warnIfLegacyName`
  prints a deprecation line when `filepath.Base(os.Args[0])` is the old name,
  **on stderr only and without touching the exit code**: a script parsing
  `z13ctl profile --get` must keep working byte-for-byte, which is the entire
  point. Matching is on the base name, not a substring, so `z13ctl-wrapper` and
  a directory called `z13ctl` do not trigger it. Removed at 3.0.
- `before.hooks`: `go mod tidy` only.
- Archives include `LICENSE`, `contrib/systemd/**/*` (user + system unit files) and
  `contrib/udev/*` (the rules file).
- **`contrib/udev/*`, not `contrib/udev/**/*`.** The doublestar form requires an
  intervening directory, so it matched nothing for that flat directory and the
  tarball shipped with no udev rules — a tarball install then loses every sysfs
  grant on the next reboot, the same failure as issue #12 by another route. The
  `contrib/systemd` glob works only because it has `user/` and `system/` beneath it.
  goreleaser logs `no files matched glob=...` and carries on rather than failing, so
  that line in the release output is the only warning you get. The nfpm packages
  were never affected — they list files individually under `nfpms.contents`.
  `tar tzf dist/*.tar.gz` after `make snapshot` is the check.
- `prerelease: auto` — tags with a pre-release suffix (e.g. `v1.0.0-beta`) are marked
  as pre-release on GitHub automatically.
- To release: `git tag v1.0.0 && git push origin v1.0.0`

## Documentation

- `README.md` — user-facing (installation, commands, colors, contributing);
  `voltaire-gui/README.md` — the GUI's own subdirectory readme.
- `website/` — the docs site: Astro Starlight, built with **pnpm** (`pnpm-lock.yaml`
  is committed; build scripts for esbuild/sharp are approved in
  `website/pnpm-workspace.yaml`, which pnpm ≥11 reads instead of a package.json
  `pnpm` field). `make docs` serves it locally, `make docs-build` builds it as CI
  does. It replaced the mkdocs `docs/` tree at 2.0; page sources live in
  `website/src/content/docs/`, nav in `astro.config.mjs` (`sidebar`), and the
  site deploys from `.github/workflows/docs.yml` (withastro/action → GitHub
  Pages) on pushes to main touching `website/**` — no longer from release.yml.
  `site` is `https://dahui.github.io` with `base: '/voltaire'`, so internal
  links are root-relative **including the base** (`/voltaire/reference/...`)
  and `starlight-links-validator` fails the build on any that do not resolve.
- **`website/src/content/docs/reference/api-go.md` is generated — do not
  hand-edit it.** Regenerate with `make docs-api` whenever `api/` doc comments
  change; the target prepends the frontmatter and strips the H1. The page is
  excluded from link validation (its `#Symbol` fragments target gomarkdoc's raw
  `<a name>` anchors, invisible to the validator); every other page is checked.
- `reference/daemon.md` holds the user-facing socket protocol tables; keep them
  in sync with `dispatch()` in `internal/daemon/server.go`.
- `reference/protocol.md` — technical HID protocol reference for developers.
- `dev/smoke-z13.md` — the hardware smoke checklist (edited in place during
  smoke runs; it is a site page now, same content).
- `migrating-from-z13ctl.md` — the 2.0 migration story, linked from the README,
  release notes, and AUR notices. New shims and their removal timeline belong
  in its compatibility table.

## Current status and next steps

**Everything below M6 ships as voltaire 2.0. There is exactly one release.**
The milestones M0–M6 in `~/.claude/plans/i-want-to-explore-snoopy-puffin.md`
are sequencing, not release boundaries; an earlier revision of that plan
assigned 2.1.0/2.2.0/2.3+ to M4/M5/M6, which was never requested and
contradicts the plan's own title. Do not reintroduce dot releases.

| Milestone | State |
|---|---|
| M0 — two live GUI bugs + AllEvents subscription | done |
| M1 — driver extraction, registry, device TOMLs, safety engine | done |
| M2 — `device-get` protocol, generic `feature` commands, GUI adopts limits | done; three items land with M5 (see below) |
| M3 — rename, repo merge, two binaries, shims, docs, packaging | code done; all three parity gates passed 2026-08-09. Release mechanics outstanding: merge to main, GitHub repo rename, `api/v2.0.0` then `v2.0.0` tags, drop the `replace` in go.mod, `GOPROXY=direct` rehearsal, archive z13gui, AUR playbook, comms |
| M4 — window split, control registry, movable quickbar, full window + dashboard + double-tap, telemetry ring | **feature-complete but for quickbar customization.** Landed: `internal/telemetryring`; the device-document prerequisites (`battery.health`, `telemetry.{power_draw,history_seconds}`); the 1 Hz sampler + `telemetry-history`; `internal/controls` + `gui.toml`; `panelgeom.Edge` + the movable quickbar; double-tap `gui-open-full`; the in-surface popup layer (`popupgeom` — not in the original list, and it replaced both the expanding selector and the cycle buttons); and the window split, each of `errBarView`, `colorView`, `themeView`, `lightingView`, `profileSection`, `autoswitchSection`, `dashboardView`, `customView` owning its own widgets and focus list. The full window is a real toplevel with Dashboard, Profiles and Settings tabs hosting second *instances* of those views through the `viewHost` seam, `internal/mainwin` deciding tabs and geometry; under gamescope the same pages live inside voltaire's own fullscreen surface via `fullSurfaceHost`, since what does not composite there is a second *toplevel*, not a second layout. Its dashboard is eight cards over the full expanded telemetry set, its Profiles page commits through one dirty-tracked button, and the bundled CSS is migrated to `@voltaire-*` (leaving the `@z13-*` aliases with no in-tree consumer). Every rule behind those three design passes — desktop density 2026-08-13, one-commit and autoswitch card 2026-08-14, expanded telemetry / battery state-of-charge / Net card the same day — has its own entry above or in `internal/gui/CLAUDE.md`, and the `VOLTAIRE_GUI_DUMP_FOCUS` fingerprint held byte-identical through all of them bar three deliberate re-baselines (twice `full:custom`, once the picker: `full:color` gone and `color` re-shaped, since a popup carries neither a back item nor the error-bar sentinel). The **Settings tab** landed 2026-08-14: generic toggle rows rendered from `DeviceInfo.Toggles` through `internal/settingsui`, plus the daemon-side notify fix two of the three toggle write paths were missing (both have their own entries above). Later the same day the Telemetry tab became the **Dashboard**: the eight tiles keep the top of the page and the live controls — profile, autoswitch, charge limit, refresh rate (`internal/display`, the one control that does not go through the daemon) and RGB — flow across the width beneath them, with autoswitch moved off the Profiles page on the rule that this page changes what the machine is *doing* while the editor changes what a profile *says*. That made every drawer section a second instance. Later still the **HSL picker stopped being a page at all** and became a single popup shared by both surfaces (`colorpopup.go`), which deleted `colorview.go` and the whole non-tab-stack-child apparatus with it; all of it has entries above. **Remaining:** (1) **quickbar customization**, on top of the `internal/controls` + `gui.toml` machinery that already exists — the last M4 feature. (2) The **gamescope path has never run in a Gaming Mode session** — there is none on this machine — so it is built, reviewed and unverified: the standing pre-release hardware risk, in both this table and the smoke checklist. |
| M5 — external plugin tier + OXP X2 Mini Pro device | not started |
| M6 — ROG Ally + generic-AMD device TOMLs | not started |
| OXP RGB | deferred past 2.0 — needs Linux 7.2 `hid-oxp` in CachyOS |

Carried into M5 from M2, because its OXP device is what makes them testable:
capability *absence* hiding controls (`limits.FromDevice` fills defaults
instead); `limits.Curve` becoming a slice gated by `Shape().Points`; firmware
profile names from `api.ProfileInfo` rather than the hardcoded
`api.StockProfiles`. The two device-document fields the plan specified and M2
left out — `battery.health` and `telemetry.{power_draw,history_seconds}` — are
**done**; they were M4 prerequisites, since the dashboard and telemetry ring
were specified to read them. `undervolt_available` is still a live probe rather
than derived from capabilities.

### The daemon — COMPLETE

The daemon is fully implemented and passing `make build && make test && make lint`.

**Key daemon details:**
- Socket paths: `$XDG_RUNTIME_DIR/voltaire/voltaire.sock` (canonical) + `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock` (compat, through 2.x)
- State file: `$XDG_STATE_HOME/voltaire/state.json` (migrated by copy from the z13ctl path on first run)
- Protocol: one newline-terminated JSON request → one JSON response; long-lived
  connections for `{"cmd":"subscribe","events":["gui-toggle"]}` (GUI use)
- Button watcher uses `github.com/holoplot/go-evdev`; systemd integration uses
  `github.com/coreos/go-systemd/v22` (activation + daemon packages)
- `ATTR{name}=="asus-wmi"` was removed from the platform-profile udev rule because
  platform-profile class devices do not have a `name` sysfs attribute file. The rule
  now matches `SUBSYSTEM=="platform-profile"` alone.
- `voltaire-perms.service` (system-level oneshot) runs `chmod g+w` on
  `BAT*/charge_control_end_threshold` and firmware-attributes `current_value` files
  after `sysinit.target`. This is necessary because these attributes may be created
  after observable udev events — so no udev `RUN+=` hook can catch them reliably.

**To activate on this machine (must be done once after each `sudo voltaire setup`):**
```sh
sudo voltaire setup                # rewrites rules file; applies sysfs perms immediately
sudo make install-perms-service  # installs battery sysfs permissions service
make install-service             # installs daemon socket + service units
```

### The GUI and the api module — merged into this repo at 2.0

(This section and the one above predate the M0–M6 roadmap and describe the
pre-merge "Phase 1 / Phase 2" split. Kept for the still-current detail; the
table above is the authoritative status.)

The `api/` submodule (now `github.com/dahui/voltaire/api/v2`; the frozen
pre-2.0 releases live at `github.com/dahui/z13ctl/api`) is complete. Its
changes:
- Module path renamed from `z13ctl` to `github.com/dahui/z13ctl`
  (and again to `github.com/dahui/voltaire/v2` for the 2.0 rename — see
  CONTRIBUTING.md for the tag/module-path matrix)
- `api/` submodule created with `State`, `LightingState` types and all `Send*`, `Subscribe` client functions
- `internal/daemon/` refactored to use `api.State`/`api.LightingState`
- `cmd/*.go` updated to call `api.Send*` directly
- Multi-module dev setup: `go.work` (gitignored) + `replace` directive in `go.mod` for pre-publication

The GUI shipped as the separate `github.com/dahui/z13gui` repo through 1.x and
was merged here for 2.0 with its history intact (`--allow-unrelated-histories`,
after a path-rewrite commit on its side). It is `voltaire-gui`: a right-edge
overlay drawer on `gotk4` + `gotk4-layer-shell`, with three display backends
(layer-shell, fullscreen overlay for GNOME, gamescope X11 for Gaming Mode),
gamepad and touch navigation, and the Armoury Crate button reaching it as the
daemon's `gui-toggle` subscribe event. `internal/gui/CLAUDE.md` holds its
GTK-level decisions (its names are pre-merge — see the table at its head).
The old repo is archived at release time with a pointer README; the AUR
`z13gui-bin` package is superseded via `provides`/`replaces`/`conflicts`.

**Multi-module release workflow (v2, post-rename):**
1. Tag `api/v2.0.0` → `git tag api/v2.0.0 && git push origin api/v2.0.0`
2. Update `go.mod`: confirm `require github.com/dahui/voltaire/api/v2 v2.0.0`,
   remove the `replace` directive
3. Tag main module: `git tag v2.0.0 && git push origin v2.0.0`

Tags are repo-global; the old-path tags (`v*` ≤ 1.3.1, `api/v1.*`) are frozen
and must never be deleted — the proxy serves them to pre-rename importers via
the GitHub redirect. Never recreate a repo named `z13ctl` (severs the
redirect). Details in CONTRIBUTING.md.
