# Package layout

File-by-file map of the repository (moved out of CLAUDE.md, 2026-11-26).

```
api/                         Public client API submodule (github.com/dahui/voltaire/api/v2)
  go.mod                     Separate module; stdlib only; importable by voltaire-gui and external tools
  types.go                   State, LightingState, FanCurvePoint, FanCurveState, TDPState, UndervoltState,
                             CustomProfile, AutoswitchState; IsCustomProfile/InCustomProfile/ActiveCustomProfile
  device.go                  DeviceInfo + section types — the device-get capability/limits document
  client.go                  SocketPath, Send*, Subscribe (all client functions)
  events.go                  event names (gui-toggle, gui-open-full, power-source, state-changed)
  profilename.go             ValidateProfileName, MaxProfileNameLen — the one copy of the name rules
  example_test.go            testable examples for all Send* and Subscribe functions
main.go                      entry point (voltaire CLI/daemon)
voltaire-gui/                entry point for the GUI binary (built to voltaire-gui/voltaire-gui
  main.go                    locally — a root dir and a root binary cannot share the name);
                             runs theme.MigrateFromZ13gui() before anything reads config
cmd/                         Cobra subcommands
  root.go                    root command (Version from internal/version), dryRunFlag, deviceFlag,
                             noButtonFlag, noSleepReleaseFlag
  hardware.go                how the CLI reaches the assembled device: readLive/daemonState (EC
                             readings come from the daemon while it is up), liveFanCurve
  lighting.go                shared lighting flag parsing (parseLightingBrightness: names or 0..max)
  tuning.go                  tuning --reset (fan curve + power limits + CO in one ordered operation)
  legacyname.go              warnIfLegacyName: stderr notice when invoked as z13ctl
  live_test.go               the ecWedged gate on status/fancurve --get (negative control run)
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
  fancurve.go                get/set/reset custom fan curves (hwmon sysfs); --preset/--list-presets
                             resolve a named device curve into the same --set path
  cpuboost.go                get/set cpufreq boost clocks
  rumble.go                  get/set the controller's vibration strength (hid-oxp)
  button.go                  which hardware button opens voltaire (--list/--get/--set via the daemon)
  tdp.go                     get/set/reset TDP power limits (asus-armoury PPT, asus-nb-wmi fallback)
  undervolt.go               get/set/reset CPU Curve Optimizer offsets via ryzen_smu
  status.go                  display system status (temperature, fans, profile, TDP, battery)
  setup.go                   setup: finds the machine, writes the rules + boot unit, runs
                             udevadm/systemctl, --remove; the grants are internal/perms'
  setup_inputplumber.go      setup --inputplumber-passthrough: whether the user opted in,
                             the plan printed, InputPlumber restarted after a change
  setup_test.go              setup's orchestration (seams: commands, root, DMI, the passes)
                             and the packaging that runs it
  undervolt_test.go          --set parse rejections (CLI/daemon parity; no SMU access)
  autoswitch_test.go         flag-combination rejections; profile-name parity; --profile guards
internal/
  aura/                      Aura HID protocol implementation
    aura.go                  Writer interface + Init/SetPower/SetBrightness/SetMode/Apply/TurnOff;
                             MaxBrightness; Apply takes the zone bytes (device data)
    modes.go                 Mode and Speed constants, the Modes table (which inputs each mode's
                             packet carries) and SpeedNames, ModeFromString/SpeedFromString
  cli/                       pure presentation remainder — NOT hardware access.
                             M1 moved everything that touches sysfs out of here;
                             see "Where the hardware code lives" below.
    doc.go                   package doc file only
    colors.go                ResolveColor/ColorDisplay/PrintColorList over aura's named table
    parse.go                 ParseColor, ParseFanCurve, TDPStateFor, ValidateProfileName (wrapper)
    dryrun.go                DryRunApply, DryRunOff, DryRunBrightness, DryRunProfile, DryRunProfileCreate/Save/Delete,
                             DryRunAutoswitch, DryRunBatteryLimit, DryRunBootSound, DryRunPanelOverdrive, DryRunFanCurve,
                             DryRunFanCurveReset, DryRunTdp, DryRunTdpReset, DryRunUndervolt, DryRunUndervoltReset
  daemon/                    long-running daemon (socket server, state, button watcher)
    daemon.go                Package doc, Daemon struct, Options, Run(), getListeners() (socket activation)
    state.go                 XDG state file persistence (uses api.State/api.LightingState)
    button.go                button watcher + pure pressTick seam: every press of the chosen
                             button emits gui-toggle, a second within [50ms, 400ms] also emits
                             gui-open-full; drawerButton/resolveDrawerButton + button-set
    button_test.go           pressTick decision table (no evdev device, no clock)
    drawerbutton_test.go     the choice: resolution, button-set, the press filter, wire keys
    hotplug.go               detachable-keyboard reattach watcher (polls sysfs, reopens HID + restores lighting)
    inputplumber.go          the InputPlumber button route: keeps voltaire's mapping in the
                             profile of the composite device holding the chosen button's
                             node (pure ipTarget/ipReconcile over an ipClient), pressRoute
    reconcile.go             custom fan curve / high-TDP floor watcher; pure reconcileTick seam + reconcileOnce;
                             stands down while d.suspending, with a tick-counted staleness ceiling
    policywatch.go           poll(POLLPRI) on the attributes the profile driver names (driver.PolicyWriteNotifier):
                             reports every profile write and fan release, same-value ones included, to the
                             reconcile watcher (z13ctl issue #22)
    profile.go               applyProfileLocked/applyCustomHW/applyStockHW, edit-target resolution,
                             profile create/save/delete/list handlers
    powersource.go           AC/battery autoswitch watcher; pure powerTick seam + powerSourceOnce;
                             UPower nudge + sysfs poll; autoswitchTarget for the Run() startup case
    powersource_test.go      powerTick decision table, settle window, charger flap (no hardware)
    reconcile_test.go        reconcileTick decision table (no hardware) + reconcileOnce race guard +
                             the suspend gate and its ceiling
    server.go                JSON request handler; handleConn(), dispatch(), command handlers, restoreStockPPT(), effectiveProfile()
    deviceinfo.go            device-get capability document (pure deviceInfoFor) + feature/feature-get handlers
    deviceundervolt.go       a slider device's undervolt setting (State.DeviceUndervolt): bare
                             undervolt set/reset, apply where no profile carries an offset
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
                             on sleep turns off lightbar + releases fans to firmware auto (releaseVolatileState)
                             and holds the suspend for the device's settle (releaseHold); on resume restores
                             lighting + volatile state (restoreVolatileState) off the signal loop
                             (sleepCycle), so a re-suspend during the EC wait abandons it
    resume_test.go           sleepTick decision table + the sleep/resume symmetry invariant (no hardware);
                             sleepCycle cancel/join; releaseHold budget arithmetic; settle from device data
    unitlinks.go             removes pre-2.0 user-unit enable symlinks the old packages left behind
    profile_test.go / undervolt_gate_test.go / undervolt_reset_test.go / release_guard_test.go /
    toggle_test.go / events_test.go / hotplug_test.go / deviceinfo_test.go / devices_test.go
                             the guards the decision entries below cite by name
  hid/
    doc.go                   package doc file only
    device.go                Device type, Known (zone name + USB vendor:product, passed in by
                             the caller — the device file's zone table), Write, SetFeature, Close
    scan.go                  FindDevice, ListDevices, HasDevice (all take []Known), sysfs
                             discovery, hasAuraReport (sysfs report_descriptor, ioctl fallback)
    export_test.go           test-only exports: NewTestDevice, NewTestDeviceAnon,
                             UeventToDevPath, DeviceNameFromUevent, HasDeviceGlob,
                             DescriptorHasAuraReport
  wakeup/                    the wake report the resume watcher logs: wakeup-source and
                             SCI/GPE counters snapshotted before a suspend (Take), diffed
                             after it (Diff → Report.Log); read-only, safe while wedged
  perms/                     what `voltaire setup` grants on one machine: For(device file)
                             → Grants, rendered as the udev rules (Rules), the boot pass
                             (Apply/Revert) and the boot unit (Unit); testdata/ holds
                             one rules golden per device file
  session/                   whether the session is Steam Gaming Mode (logind)
  version/                   Version var — one -X ldflag serves both binaries
                             (— device abstraction, M0–M2 —)
  driver/                    the per-hardware-class interfaces the daemon and CLI program
                             against (Fans, Power, Profile, Lighting, Buttons, …) +
                             driver.PowerEnvelope; device support is implementations
                             selected by data, not calls into one machine's sysfs layout.
                             doc.go is the driver contract
  device/                    assembles a Device (per-class driver fields, nil where the
                             machine has no such capability) from DMI identity matched
                             against the embedded devices/*.toml
    devices/                 device data — asus-rog-flow-z13-2025.toml (schema: config.go;
                             documented key by key in website/.../dev/device-files.md)
  devicefixture/             a synthetic second device (TOML + the document the daemon
                             produces from it) for tests of the capability-absence rules
  drivers/
    (split by vendor/subsystem on 2026-11-15 — was one asusz13/ package; method
     names in device files unchanged. No driver package imports another: the
     couplings are injected in register/.)
    asus/                    ASUS firmware interfaces, shared by every asus-wmi/asus-armoury
                             machine; doc.go says what is data vs code
      drivers.go             fanController, powerLimiter, toggles; ThrottlePolicyPath
      fan.go                 hwmon discovery; channels, points and fans enumerated from attribute
                             names; curve read/write (every channel), RPM, mode control,
                             SetAllFansFullSpeed, VerifyFanCurveActive
      tdp.go                 PPT interface choice (asus-armoury, else asus-nb-wmi) and I/O: ReadAllPPT,
                             SetTDPState (clamps into armoury's range), PlanTDPWrites; the envelope
                             overlay; which limits exist (presentLimits) and their wording
                             (pptLimitText, completeLimits)
      armoury.go             boot sound / panel overdrive helpers, charge_mode (ReadCharger,
                             ChargerFromArmoury), pending_reboot
      paths.go / main_test.go / sysfs_fake_test.go   its own roots, guard and fake tree
      fan_sysfs_test.go / fan_enum_test.go / tdp_test.go / ppt_test.go / toggles_test.go
    amd/                     AMD silicon
      smu.go                 SMU sysfs mailbox: SMUAvailable, SMUProbeUndervolt, SendSMUCommand
      undervolt.go           Curve Optimizer: SetCurveOptimizer, ResetCurveOptimizer, CurveOptimizerCommand;
                             the per-CPU command table (coPlatforms, from ryzenadj)
      undervolter.go         driver.Undervolter (NewUndervolter) + Text(): the CO's wording
      pmtable.go             pure pm_table parser (per-version layout); nothing reads the table —
                             fixtures under load in testdata/
      gpu.go                 amdgpu, all from one card: edge temp and sclk (its own hwmon, by
                             label), busy %, VRAM carveout, and the pure gpu_metrics parser
                             (per-revision offset table, 2.0–3.0); GPUSource adapter
      npu.go                 amdxdna NPU power/util/clock over DRM ioctls; queried only
                             while runtime_status reads active (opening it resumes the NPU); NPUSource
      k10temp.go             APU temperature (ReadAPUTemperature)
      paths.go / main_test.go / sysfs_fake_test.go   its own roots, guard, fake tree + fakeSMU
    linux/                   generic Linux interfaces
      profile.go             platform_profile: FindProfilePath, SetProfile (+ PPD sync via ppdRunner)
      drivers.go             profileController (extra policy paths injected), battery
                             (ChargerFunc injected), cpuBoost, and the telemetry composer
                             (TelemetrySources{Temp, GPU, NPU} injected); RPM is the daemon's
      power.go               FindACOnlinePath, OnACPower (Mains-only discovery), battery threshold,
                             health, capacity, ReadChargerFromSupplies
      rapl.go                powercap package-energy counter (read-only grant) + battery
                             flow, state and the Wh energy pair from power_supply
                             (power_now, or current x voltage; energy_now, or charge x voltage)
      cpu.go                 procfs/cpufreq: jiffie counters, average core clock, memory,
                             and the global boost switch (read/write/verify)
      net.go                 /proc/net/dev byte counters, summed over physical interfaces
                             only (a /sys/class/net/*/device link) so tunnels never double-count
      paths.go / main_test.go / sysfs_fake_test.go   its own roots, guard and fake tree
    internal/sysfsio/        path-parameterised read/write/find-hwmon helpers the three share
    register/                blank-import package: method name → factory, and the one place
                             drivers from different packages meet (charger, policy paths,
                             telemetry sources). docs_test.go holds the developer guide's
                             drift guards (the device-file example assembles; every schema
                             key is documented)
    aurahid/                 driver.Lighting over the Aura HID protocol ("aura-hid")
    evdevkey/                driver.Buttons over one key ("evdev-key") or a chord of keys
                             ("evdev-chord") on an evdev node, found by name and optionally
                             USB interface (NodeUSB); read-only, never grabbed, and
                             refuses a typing keyboard unless keyboard_node opts it in
    hidrawbtn/               driver.Buttons over vendor HID report bytes ("hidraw-report",
                             `match` pairs; the Ally's buttons, the OXP's Home); refuses a
                             keyboard interface by its report descriptor
    hidoxp/                  driver.Rumble over hid-oxp's rumble_intensity (the OneXPlayer
                             controller's vibration strength; range from rumble_intensity_range)
    inputplumber/            the InputPlumber route: pure profile merge (SetMapping, YAML),
                             a DBus client (composite devices, Get/LoadProfile, change
                             signals) and the driver.Buttons for voltaire_<id> InputEvents;
                             override.go: the opt-in device-config override that passes the
                             lighting interface through (edit, check, DMI match, plan)
  safety/                    the thermal-safety rules between handlers and drivers, pure and
                             parameterized by driver.PowerEnvelope
    safety.go                pure rules: FanCurveForTDP, FloorPWMAt, FloorAdjustsCurve,
                             CheckCurveAgainstTDP, CheckFanFloorReleaseAt
    limits.go                the ranges: ResolveTDP (shared CLI/daemon validation; raises PL2/PL3 to
                             their minimums), EffectiveTDP, CacheStale, PL1/PL2/PL3Range, TDPStateFor
    engine.go                Engine{Fans, Power} — the only path to a PPT write:
                             ApplyTDPSafely, ReleaseTDP, Read, ReadEffective, RestoreStock,
                             CheckFanFloorRelease, Envelope. Device carries the Engine, not
                             the raw driver.PowerLimiter, so the floor cannot be bypassed.
  controls/                  M4: which drawer sections exist, what each needs from the device,
                             and their order — Resolve(gui.toml, device) + Layout (headings and
                             separators). Pure; internal/gui holds only ID→builder.
  mainwin/                   M4: the full window's page list (Dashboard, Profiles, Settings), its
                             window controls (Controls(hosted))
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
  buttonpref/                which surface the hardware button opens: one value
                             (what a *single* press raises), its parse and its labels
    chooser.go               which button opens voltaire: Choices, Drawer (the daemon's
                             resolved answer), the Settings row's text
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
  audio/                     the GUI's volume slider: wpctl on @DEFAULT_AUDIO_SINK@, pactl
                             fallback; pure reply parsing, exec seam. Session-owned, like display
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
    editor.go                which editor cards a device gets (SectionsFor), the hints
                             naming what a custom profile holds, the undervolt note and
                             readout, the header line
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
    span.go                  the history windows a dashboard offers (Spans/Offered, filtered
                             against what the device retains) and how often each is worth
                             redrawing (RefreshInterval — cost must not grow with the window)
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
  udev/                      99-voltaire-gamepad.rules (uaccess for any controller; the
                             only packaged rules file — setup writes 99-voltaire.rules)
  nfpm/                      package scripts: postinstall/preremove/postremove for the one
                             voltaire package (both binaries; each migrates the pre-rename units)
  aur/                       single voltaire-bin PKGBUILD (both binaries) + its
                             .install; release.yml patches the placeholders and pushes
                             on tag. provides/conflicts cover all four old names;
                             replaces=() is inert on AUR but declared anyway.
                             The retirement runbook for the old AUR packages is
                             .claude/plans/voltaire-aur-playbook.md (Jeff executes).
  voltaire-gui.desktop       desktop entry
examples/themes/             shipped theme TOMLs (catppuccin, gruvbox, nord, rog-*, …)
website/                     the docs site (VitePress; see Documentation)
  docs/                      page sources (.md); dev/api-go.md is generated
    .vitepress/config.mts    site config: base /voltaire/, cleanUrls, sidebar (the table of contents)
    .vitepress/anchors.ts    post-build #fragment check (VitePress checks pages only)
```

