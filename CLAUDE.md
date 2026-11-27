# voltaire — Project Context for Claude

## What this project is

`voltaire` (formerly `z13ctl` and `z13gui`) controls power limits, fan curves,
firmware profiles, RGB lighting, the charge limit and firmware switches on the
machines its device files describe (`internal/device/devices/*.toml`): the
2025 ASUS ROG Flow Z13, the OneXPlayer X2 Mini Pro and the ASUS ROG Xbox Ally X,
through hidraw, asus-wmi / asus-armoury sysfs, the AMD SMU (ryzen_smu) and
kernel HID drivers. Lighting speaks the ASUS Aura HID protocol,
reverse-engineered from G-Helper. Module path: `github.com/dahui/voltaire/v2`.
License: Apache 2.0.

**Two binaries from one module**: `voltaire` (the CLI + daemon, `CGO_ENABLED=0`)
and `voltaire-gui` (the GTK4 overlay drawer, `CGO_ENABLED=1`), the latter merged
in from the former z13gui repo at 2.0 with its history intact. They share
`internal/version.Version` (one `-X` ldflag serves both) and talk to each other
only through `api/` — the GUI is a socket client like any other.

## Package layout

`docs/package-layout.md` is the file-by-file map. The shape: `api/` (the public
client module), `cmd/` (CLI), `internal/daemon` (socket server, state,
watchers), `internal/device` (device files → assembled Device),
`internal/driver` (the driver contract), `internal/drivers/*` (asus, amd, linux,
aurahid, evdevkey, hidrawbtn, hidoxp, inputplumber, register), `internal/safety`
(the thermal rules and the only path to a power write), `internal/perms` (what
`voltaire setup` grants), `internal/gui` (the GTK island) plus its pure rule
packages, `contrib/` (units, the gamepad rules, package scripts), `website/`
(the docs site).

## Invariants

The structural rule: `Device` carries a `safety.Engine`, never the raw power
limiter, and `Device.Fans` is a `driver.FanCurves` with no `Release`, so no
handler can reach a power write without the fan floor or a release without
the limit being restored. Drivers are passive — no locking, no goroutines, no
policy; serialization stays in the daemon (`hwMu` then `d.mu`) and safety in
`internal/safety`. The decision records below say why each rule exists; read
the one for anything you are about to change.

## Key architectural decisions

Each decision is a record in `docs/decisions/` (moved there verbatim from this
file on 2026-11-26, old symbol names renamed in place). Titles only here; open
the record before changing what it covers.

### Daemon, drivers, device data, setup and packaging

- [`aura.Writer` interface (not `*hid.Device`) —](docs/decisions/0001-aura-writer-interface-not-hid-device.md)
- [`hid.Device` holds `[]hidrawNode`;](docs/decisions/0002-hid-device-holds-hidrawnode.md)
- [Zone 0 = keyboard (`0b05:1a30`), Zone 1 = lightbar (`0b05:18c6`) —](docs/decisions/0003-zone-0-keyboard-0b05-1a30-zone-1-lightbar-0b05-18c6.md)
- [Lighting is described by the driver, and every layer reads that description](docs/decisions/0004-lighting-is-described-by-the-driver-and-every-layer-reads-th.md)
- [Profile and battery-threshold paths (`linux/profile.go`) use discovery via `/sys/class/*/` rather than hardcoded paths, so udev-chmoded device inodes are used (not the ACPI alias)](docs/decisions/0005-profile-and-battery-threshold-paths-linux-profile-go-use-dis.md)
- [Setup applies its grants twice, and both come from one value](docs/decisions/0006-setup-applies-its-grants-twice-and-both-come-from-one-value.md)
- [No permission artifact is packaged; every package runs `voltaire setup`](docs/decisions/0007-no-permission-artifact-is-packaged-every-package-runs-voltai.md)
- [Package scripts: guard preremove, and run postinstall on every upgrade path](docs/decisions/0008-package-scripts-guard-preremove-and-run-postinstall-on-every.md)
- [The AUR package is setup-driven, like the others](docs/decisions/0009-the-aur-package-is-setup-driven-like-the-others.md)
- [`systemctl --user disable` cannot undo a `--global` enable, so `install-service` masks what it cannot disable](docs/decisions/0011-systemctl-user-disable-cannot-undo-a-global-enable-so-instal.md)
- [The gamepad blocker needs cilium/ebpf ≥ 0.22 on Linux 7.1+](docs/decisions/0015-the-gamepad-blocker-needs-cilium-ebpf-0-22-on-linux-7-1.md)
- [`--dry-run` is a global persistent flag;](docs/decisions/0016-dry-run-is-a-global-persistent-flag.md)
- [`--no-button` is a global persistent flag;](docs/decisions/0017-no-button-is-a-global-persistent-flag.md)
- [`--no-sleep-release` is likewise daemon-only: the pre-sleep fan release is skipped and a custom curve stays in force through suspend](docs/decisions/0018-no-sleep-release-is-likewise-daemon-only-the-pre-sleep-fan-r.md)
- [`voltaire setup` grants what this machine's device file declares, and nothing else](docs/decisions/0019-voltaire-setup-grants-what-this-machine-s-device-file-declar.md)
- [The hardware button's name is device data](docs/decisions/0020-the-hardware-button-s-name-is-device-data.md)
- [Which button opens voltaire is a choice among the device file's buttons, never a key learned from a keyboard](docs/decisions/0021-which-button-opens-voltaire-is-a-choice-among-the-device-fil.md)
- [No button grant may reach a typing keyboard, and each kind of node has two guards](docs/decisions/0022-no-button-grant-may-reach-a-typing-keyboard-and-each-kind-of.md)
- [Lighting's hidraw grant is one interface; it was every interface of the device until 2026-11-15, the cover's typing interface included](docs/decisions/0023-lighting-s-hidraw-grant-is-one-interface-it-was-every-interf.md)
- [A button InputPlumber manages reaches voltaire through InputPlumber's profile, loaded over DBus, never a file](docs/decisions/0024-a-button-inputplumber-manages-reaches-voltaire-through-input.md)
- [What the OXP's captures changed in the button drivers](docs/decisions/0025-what-the-oxp-s-captures-changed-in-the-button-drivers.md)
- [A capability whose out-of-tree interface is missing is *unavailable*, not broken, and says why](docs/decisions/0027-a-capability-whose-out-of-tree-interface-is-missing-is-unava.md)
- [A kernel interface that exists but does not work is declared off, and turned on by kernel version](docs/decisions/0028-a-kernel-interface-that-exists-but-does-not-work-is-declared.md)
- [On a slider device the undervolt is a device setting, like the TDP](docs/decisions/0029-on-a-slider-device-the-undervolt-is-a-device-setting-like-th.md)
- [Controller vibration strength is a daemon setting like CPU boost](docs/decisions/0031-controller-vibration-strength-is-a-daemon-setting-like-cpu-b.md)
- [Setup leaves a node at mode 0000 alone](docs/decisions/0032-setup-leaves-a-node-at-mode-0000-alone.md)
- [An absent attribute is nothing to do, everywhere](docs/decisions/0033-an-absent-attribute-is-nothing-to-do-everywhere.md)
- [Daemon socket fallback](docs/decisions/0034-daemon-socket-fallback.md)
- [Daemon systemd integration](docs/decisions/0035-daemon-systemd-integration.md)
- [State persistence](docs/decisions/0036-state-persistence.md)
- [Always snapshot state with `cloneState()` before releasing `d.mu`](docs/decisions/0037-always-snapshot-state-with-clonestate-before-releasing-d-mu.md)
- [Button watcher](docs/decisions/0038-button-watcher.md)
- [Never call EVIOCGRAB on the button device](docs/decisions/0039-never-call-eviocgrab-on-the-button-device.md)
- [Daemon socket protocol](docs/decisions/0041-daemon-socket-protocol.md)
- [Broadcast writes are deadline-bounded (`broadcastWriteTimeout`)](docs/decisions/0043-broadcast-writes-are-deadline-bounded-broadcastwritetimeout.md)
- [Every handler that mutates profile or thermal state calls `saveAndNotify`, not `saveState`](docs/decisions/0044-every-handler-that-mutates-profile-or-thermal-state-calls-sa.md)
- [Streamed events must set `OK: true`](docs/decisions/0045-streamed-events-must-set-ok-true.md)
- [Socket I/O must be deadline-bounded on both ends](docs/decisions/0046-socket-i-o-must-be-deadline-bounded-on-both-ends.md)
- [`api.Subscribe`'s cancel func must release the reader goroutine](docs/decisions/0047-api-subscribe-s-cancel-func-must-release-the-reader-goroutin.md)
- [`applyLightingState` and the lighting driver's handle require `d.mu`](docs/decisions/0048-applylightingstate-and-the-lighting-driver-s-handle-require.md)
- [IPC library](docs/decisions/0049-ipc-library.md)
- [Shared sysfs helpers](docs/decisions/0050-shared-sysfs-helpers.md)
- [asus-armoury firmware-attributes](docs/decisions/0051-asus-armoury-firmware-attributes.md)
- [Fan curves](docs/decisions/0052-fan-curves.md)
- [The fan hardware's shape is enumerated, and a mismatch with the device data fails closed](docs/decisions/0053-the-fan-hardware-s-shape-is-enumerated-and-a-mismatch-with-t.md)
- [A custom fan curve is dropped by any `platform_profile` write](docs/decisions/0054-a-custom-fan-curve-is-dropped-by-any-platform-profile-write.md)
- [`hwMu` guards hardware mutation sequences; lock order is `hwMu` then `d.mu`](docs/decisions/0055-hwmu-guards-hardware-mutation-sequences-lock-order-is-hwmu-t.md)
- [TDP (PPT power limits)](docs/decisions/0056-tdp-ppt-power-limits.md)
- [The high-TDP fan floor is a *per-point minimum against the whole `FloorCurve`*, not against its `HighTDPMinPWM` bottom, and not a replacement curve](docs/decisions/0057-the-high-tdp-fan-floor-is-a-per-point-minimum-against-the-wh.md)
- [`safety.Engine.ApplyTDPSafely` is the only way to apply a custom TDP, and it takes the curve the caller intends to run](docs/decisions/0058-safety-engine-applytdpsafely-is-the-only-way-to-apply-a-cust.md)
- [Fan-floor checks read hardware for the *live* profile, and the profile's own TDP for any other](docs/decisions/0059-fan-floor-checks-read-hardware-for-the-live-profile-and-the.md)
- [Fan-floor checks read hardware, not cached state](docs/decisions/0060-fan-floor-checks-read-hardware-not-cached-state.md)
- [Daemon tests cannot exercise the fan-floor guards](docs/decisions/0061-daemon-tests-cannot-exercise-the-fan-floor-guards.md) (superseded: see Testing)
- [Per-device lighting states must be normalized before use](docs/decisions/0062-per-device-lighting-states-must-be-normalized-before-use.md)
- [A per-zone `off` keeps the zone's effect and flips `Enabled`](docs/decisions/0063-a-per-zone-off-keeps-the-zone-s-effect-and-flips-enabled.md)
- [`linux.setProfileFor` must write the primary path even when the loop misses it](docs/decisions/0064-linux-setprofilefor-must-write-the-primary-path-even-when-th.md)
- [`amd.smuProbeUndervolt()` is destructive — call it only where an offset is about to be written](docs/decisions/0065-amd-smuprobeundervolt-is-destructive-call-it-only-where-an-o.md)
- [Every route to a stock profile must clear the undervolt](docs/decisions/0066-every-route-to-a-stock-profile-must-clear-the-undervolt.md)
- […but it must clear it only when something is actually applied: `d.uvApplied()`, never availability alone](docs/decisions/0067-but-it-must-clear-it-only-when-something-is-actually-applied.md)
- [`tuning-reset` exists because the *ordering* is the feature, not the convenience](docs/decisions/0068-tuning-reset-exists-because-the-ordering-is-the-feature-not.md)
- [`pending_reboot` is a property of the firmware *interface*, not of any one toggle, so it is an optional interface rather than a `Toggles` method](docs/decisions/0069-pending-reboot-is-a-property-of-the-firmware-interface-not-o.md)
- [The charger kind comes from `power_supply`, not `charge_mode`](docs/decisions/0070-the-charger-kind-comes-from-power-supply-not-charge-mode.md)
- [`charge_mode` was the only thing that could tell the Z13's two power inputs apart, and its vocabulary was established by swapping the charger — not by inference](docs/decisions/0071-charge-mode-was-the-only-thing-that-could-tell-the-z13-s-two.md)
- [The fan-curve editor draws the operating point, and it marks what the curve *commands* rather than what the fans are doing](docs/decisions/0072-the-fan-curve-editor-draws-the-operating-point-and-it-marks.md)
- [A fan-curve preset is sugar for a curve, and the single most important thing about it is that nothing applies one but the user](docs/decisions/0073-a-fan-curve-preset-is-sugar-for-a-curve-and-the-single-most.md)
- [A power limit cannot be verified on an idle machine](docs/decisions/0074-a-power-limit-cannot-be-verified-on-an-idle-machine.md)
- [`tdp_limits` exists on get-state for z13ctl 1.4 clients; `device-get` is where 2.0 serves the same ranges](docs/decisions/0075-tdp-limits-exists-on-get-state-for-z13ctl-1-4-clients-device.md)
- [EPP belongs to power-profiles-daemon, not to voltaire — the ownership test](docs/decisions/0076-epp-belongs-to-power-profiles-daemon-not-to-voltaire-the-own.md)
- [The 2026-08-14 feature survey (HHD, G-Helper, z13ctl-plus/z13gui-plus) — what was rejected, so it is not re-proposed](docs/decisions/0077-the-2026-08-14-feature-survey-hhd-g-helper-z13ctl-plus-z13gu.md)
- [A corrupt state file is preserved, not silently replaced](docs/decisions/0078-a-corrupt-state-file-is-preserved-not-silently-replaced.md)
- [`make test` and `make lint` must run both modules](docs/decisions/0079-make-test-and-make-lint-must-run-both-modules.md)
- [`--color 000000` is not black](docs/decisions/0080-color-000000-is-not-black.md)
- [Custom profiles are named records; `state.CustomProfiles` is the only in-memory truth](docs/decisions/0081-custom-profiles-are-named-records-state-customprofiles-is-th.md)
- [The active profile is the default edit target; `--profile` overrides it](docs/decisions/0082-the-active-profile-is-the-default-edit-target-profile-overri.md)
- [Promotion starts fresh: a bare edit from a firmware profile commits `custom` containing only that edit](docs/decisions/0083-promotion-starts-fresh-a-bare-edit-from-a-firmware-profile-c.md)
- ["Create and activate `custom`" is right for `--set` and wrong for `--reset`; `editTarget.implicit()` is the distinction](docs/decisions/0084-create-and-activate-custom-is-right-for-set-and-wrong-for-re.md)
- [`reconcileCurveFor` may return nil, and the caller must honour that rather than substituting the tick's own `act.Curve`](docs/decisions/0085-reconcilecurvefor-may-return-nil-and-the-caller-must-honour.md)
- [The reconcile watcher's undervolt arm is signal-driven, not observation-driven, because CO has no readback](docs/decisions/0086-the-reconcile-watcher-s-undervolt-arm-is-signal-driven-not-o.md)
- [`cli.DryRunTdp` takes the curve as a parameter; it must not read hwmon](docs/decisions/0087-cli-dryruntdp-takes-the-curve-as-a-parameter-it-must-not-rea.md)
- [`Run()` persists a startup autoswitch decision that lands on a firmware profile](docs/decisions/0088-run-persists-a-startup-autoswitch-decision-that-lands-on-a-f.md)
- [`handleTDPReset` writes `platform_profile` even when already on `balanced`, and that is deliberate](docs/decisions/0089-handletdpreset-writes-platform-profile-even-when-already-on.md)
- [Activating a custom profile *clears* the subsystems it does not set](docs/decisions/0090-activating-a-custom-profile-clears-the-subsystems-it-does-no.md)
- [`asus.resetAllFanCurves` verifies the release, as `asus.setFanCurves` verifies the curve](docs/decisions/0091-asus-resetallfancurves-verifies-the-release-as-asus-setfancu.md)
- [There is exactly one apply-a-custom-profile sequence: `applyCustomHW`](docs/decisions/0092-there-is-exactly-one-apply-a-custom-profile-sequence-applycu.md)
- [A daemon restart does not reset the fan controller or the Curve Optimizer](docs/decisions/0093-a-daemon-restart-does-not-reset-the-fan-controller-or-the-cu.md)
- [Only a firmware profile *this device offers* may reach `platform_profile`](docs/decisions/0094-only-a-firmware-profile-this-device-offers-may-reach-platfor.md)
- [The firmware profile list is the kernel's, filtered by device data, and reads are normalized to it](docs/decisions/0095-the-firmware-profile-list-is-the-kernel-s-filtered-by-device.md)
- [Resets land on `[profiles] default`, never a literal `"balanced"`](docs/decisions/0096-resets-land-on-profiles-default-never-a-literal-balanced.md)
- [The profile CRUD handlers take `hwMu` even though they write no hardware](docs/decisions/0097-the-profile-crud-handlers-take-hwmu-even-though-they-write-n.md)
- [The `profile` request field is silently ignored by older daemons, which makes an unguarded `--profile` edit apply to the running machine](docs/decisions/0098-the-profile-request-field-is-silently-ignored-by-older-daemo.md)
- [Every kernel firmware profile name is reserved, at four layers](docs/decisions/0099-every-kernel-firmware-profile-name-is-reserved-at-four-layer.md)
- [Switching to a firmware profile preserves every custom profile](docs/decisions/0100-switching-to-a-firmware-profile-preserves-every-custom-profi.md)
- [AC/battery autoswitch is edge-triggered on `online`, never level-triggered, and never reads `platform_profile`](docs/decisions/0101-ac-battery-autoswitch-is-edge-triggered-on-online-never-leve.md)
- [`linux.FindACOnlinePath` filters on `type == "Mains"`, never on the presence of an `online` file](docs/decisions/0102-linux-findaconlinepath-filters-on-type-mains-never-on-the-pr.md)
- [The stock row is never the last write to a stock profile](docs/decisions/0103-the-stock-row-is-never-the-last-write-to-a-stock-profile.md)
- [A fan release or a `platform_profile` write resets the power limits, and sysfs cannot see it](docs/decisions/0104-a-fan-release-or-a-platform-profile-write-resets-the-power-l.md)
- [A firmware profile reports its stock row, never the readback, and only for display: `safety.Engine.ReadDisplayed`](docs/decisions/0105-a-firmware-profile-reports-its-stock-row-never-the-readback.md)
- [`safety.Engine.ReadEffective` must be passed the *effective* profile](docs/decisions/0106-safety-engine-readeffective-must-be-passed-the-effective-pro.md)
- [Undervolt (Curve Optimizer)](docs/decisions/0107-undervolt-curve-optimizer.md)
- [Sleep/resume hook](docs/decisions/0108-sleep-resume-hook.md)
- [On some machines a custom fan curve must be released before sleep, or the fans never stop](docs/decisions/0109-on-some-machines-a-custom-fan-curve-must-be-released-before.md)
- [Do not blame the sleep hook for a machine that will not stay asleep](docs/decisions/0110-do-not-blame-the-sleep-hook-for-a-machine-that-will-not-stay.md)
- [`sleepTick` is gated on ownership, and that is what makes sleep and resume symmetric](docs/decisions/0111-sleeptick-is-gated-on-ownership-and-that-is-what-makes-sleep.md)
- [The pre-sleep release lowers power before it touches the fans, and fails closed](docs/decisions/0112-the-pre-sleep-release-lowers-power-before-it-touches-the-fan.md)
- [`d.suspending` stands the reconcile watcher down between the two signals, with a staleness ceiling](docs/decisions/0113-d-suspending-stands-the-reconcile-watcher-down-between-the-t.md)
- [A resume that times out on a wedged EC latches `d.ecWedged`, and every path into the EC stands down on it — not just the resume apply](docs/decisions/0114-a-resume-that-times-out-on-a-wedged-ec-latches-d-ecwedged-an.md)
- [The daemon holds a logind delay inhibitor, because `PrepareForSleep(true)` is otherwise advisory](docs/decisions/0115-the-daemon-holds-a-logind-delay-inhibitor-because-preparefor.md)
- [A suspend straight after a fan release is woken by the EC; the hook holds it for the device's settle (3 s on the Z13) first](docs/decisions/0116-a-suspend-straight-after-a-fan-release-is-woken-by-the-ec-th.md)
- [The post-resume restore runs off the signal loop, and a sleep edge cancels it (`sleepCycle`)](docs/decisions/0117-the-post-resume-restore-runs-off-the-signal-loop-and-a-sleep.md)
- [Keyboard hotplug watcher](docs/decisions/0118-keyboard-hotplug-watcher.md)
- [The GUI is an ordinary socket client, and the merge did not change that](docs/decisions/0119-the-gui-is-an-ordinary-socket-client-and-the-merge-did-not-c.md)
- [The drawer's limits come from `device-get`, with `DefaultLimits` as the fallback — and the two must stay interchangeable](docs/decisions/0120-the-drawer-s-limits-come-from-device-get-with-defaultlimits.md)
- [One ASUS driver for every ASUS machine; text and controls follow the device](docs/decisions/0121-one-asus-driver-for-every-asus-machine-text-and-controls-fol.md)
- [A capability the document declares must be one the driver actually reads](docs/decisions/0122-a-capability-the-document-declares-must-be-one-the-driver-ac.md)
- [CPU boost is a capability of its own, not a firmware toggle, and the difference is who keeps the setting](docs/decisions/0123-cpu-boost-is-a-capability-of-its-own-not-a-firmware-toggle-a.md)
- [The powercap grant is the only read-only one, and that is not an accident](docs/decisions/0124-the-powercap-grant-is-the-only-read-only-one-and-that-is-not.md)
- [The expanded telemetry (2026-08-14) matches the z13ctl-plus/z13gui-plus data set, through the daemon](docs/decisions/0125-the-expanded-telemetry-2026-08-14-matches-the-z13ctl-plus-z1.md)
- [`gpu_metrics` is read through a per-revision offset table, and GFX power only where it is the GFX rail](docs/decisions/0126-gpu-metrics-is-read-through-a-per-revision-offset-table-and.md)
- [A declared telemetry source is offered only where its hardware is present](docs/decisions/0127-a-declared-telemetry-source-is-offered-only-where-its-hardwa.md)
- [Package power crosses the driver boundary as an energy *counter*, not as watts](docs/decisions/0128-package-power-crosses-the-driver-boundary-as-an-energy-count.md)
- [Battery flow is the one telemetry quantity whose zero is a reading](docs/decisions/0129-battery-flow-is-the-one-telemetry-quantity-whose-zero-is-a-r.md)
- [The battery chart plots state of charge; the flow feeds the card header's rate and time estimate](docs/decisions/0130-the-battery-chart-plots-state-of-charge-the-flow-feeds-the-c.md)
- [`battery` and `telemetry` are document *sections*, not presence bools, because their contents are independently absent](docs/decisions/0131-battery-and-telemetry-are-document-sections-not-presence-boo.md)
- [State of health crosses the driver boundary as a ratio, never as the pair it is computed from](docs/decisions/0132-state-of-health-crosses-the-driver-boundary-as-a-ratio-never.md)
- [`telemetryring` timestamps with wall-clock time and carries its own lock, and both are deliberate](docs/decisions/0133-telemetryring-timestamps-with-wall-clock-time-and-carries-it.md)
- [Which toggles exist is the kernel's answer through a code allowlist](docs/decisions/0137-which-toggles-exist-is-the-kernel-s-answer-through-a-code-al.md)
- [Generic toggle rows needed two api additions; both landed, and the renderer is now the full window's Settings tab](docs/decisions/0138-generic-toggle-rows-needed-two-api-additions-both-landed-and.md)
- [The refresh rate follows the power source, and that switch is the GUI's for the same reason the manual control is](docs/decisions/0145-the-refresh-rate-follows-the-power-source-and-that-switch-is.md)
- [Every firmware-toggle write path must notify, and two of the three did not](docs/decisions/0146-every-firmware-toggle-write-path-must-notify-and-two-of-the.md)
- [The telemetry sampler stands down while suspending for a *different reason* than the other watchers, and the difference is load-bearing](docs/decisions/0150-the-telemetry-sampler-stands-down-while-suspending-for-a-dif.md)
- [`get-state` is the *live* edge of the same series `telemetry-history` plots, and it must carry every quantity the device measures](docs/decisions/0151-get-state-is-the-live-edge-of-the-same-series-telemetry-hist.md)
- [A battery flow figure is meaningless without the pack's state beside it, and shipping the number alone was reported as a broken sensor](docs/decisions/0152-a-battery-flow-figure-is-meaningless-without-the-pack-s-stat.md)
- [`freshEnough` compares Unix seconds, and the reason is that the hazard it guards cannot be tested](docs/decisions/0153-freshenough-compares-unix-seconds-and-the-reason-is-that-the.md)
- [A failed sample is a gap, not a zero, and the wire carries timestamps so a client can see it](docs/decisions/0154-a-failed-sample-is-a-gap-not-a-zero-and-the-wire-carries-tim.md)
- [`telemetry-history` is a typed `history` field, not JSON stuffed into `value`](docs/decisions/0155-telemetry-history-is-a-typed-history-field-not-json-stuffed.md)
- [The dashboard's three rules live in `internal/telemetryplot`, and each is there because the obvious alternative draws something false](docs/decisions/0156-the-dashboard-s-three-rules-live-in-internal-telemetryplot-a.md)
- [The dashboard offers five history windows, and the refresh cadence scales with the one on screen](docs/decisions/0157-the-dashboard-offers-five-history-windows-and-the-refresh-ca.md)
- [The axis is framed per *kind*, not per series, and the nominal frame is a starting point that expands rather than a clamp](docs/decisions/0158-the-axis-is-framed-per-kind-not-per-series-and-the-nominal-f.md)
- [The dashboard polls `telemetry-history` only while it is the visible view](docs/decisions/0159-the-dashboard-polls-telemetry-history-only-while-it-is-the-v.md)
- [`internal/apiresult` exists because "the daemon is not running" is not an error](docs/decisions/0160-internal-apiresult-exists-because-the-daemon-is-not-running.md)
- [The GUI's environment variables renamed with a fallback: `startup.GUIEnv(suffix)`](docs/decisions/0162-the-gui-s-environment-variables-renamed-with-a-fallback-star.md)
- [The profile name rules live in `api` (`ValidateProfileName`, `MaxProfileNameLen`); `api.ValidateProfileName` is a delegating wrapper](docs/decisions/0164-the-profile-name-rules-live-in-api-validateprofilename-maxpr.md)
- [`State.SourceKnown`: `on_ac` false with `source_known` false means *unknown*, never battery](docs/decisions/0165-state-sourceknown-on-ac-false-with-source-known-false-means.md)
- [The custom profiles are one control on the main view, never one row each](docs/decisions/0166-the-custom-profiles-are-one-control-on-the-main-view-never-o.md)
- [The profile selector expands in the flow of the view; it is not a `GtkDropDown`](docs/decisions/0168-the-profile-selector-expands-in-the-flow-of-the-view-it-is-n.md)
- [The selector is rebuilt only when `profileui.Signature` changes](docs/decisions/0169-the-selector-is-rebuilt-only-when-profileui-signature-change.md)
- [The autoswitch target rows are shown only while autoswitch is enabled](docs/decisions/0170-the-autoswitch-target-rows-are-shown-only-while-autoswitch-i.md)
- [The editor addresses its target through `profileui.PlanEdit`, and every stored-target send is preceded by `probeStoredTarget`](docs/decisions/0171-the-editor-addresses-its-target-through-profileui-planedit-a.md)
- [The gamepad focus grid skips *insensitive* widgets, not just hidden ones](docs/decisions/0173-the-gamepad-focus-grid-skips-insensitive-widgets-not-just-hi.md)
- [The autoswitch UI uses cycle buttons and a debounced single send](docs/decisions/0174-the-autoswitch-ui-uses-cycle-buttons-and-a-debounced-single.md)
- [The token regexes must match *both* prefixes, and widening them was the load-bearing half of the migration](docs/decisions/0176-the-token-regexes-must-match-both-prefixes-and-widening-them.md)
- [Where InputPlumber manages the controller, the drawer takes it through intercept mode, owned by the daemon](docs/decisions/0177-where-inputplumber-manages-the-controller-the-drawer-takes-it-through-intercept-mode.md)

### GUI (see also `internal/gui/CLAUDE.md`)

- [`make install-service` installs all three user units, and a source install is the only path where forgetting one is invisible](docs/decisions/0010-make-install-service-installs-all-three-user-units-and-a-sou.md)
- [Two enabled GUI units make the drawer open itself at login, and the compatibility symlinks are why](docs/decisions/0012-two-enabled-gui-units-make-the-drawer-open-itself-at-login-a.md)
- [`make install` must not pair a fresh daemon with a stale drawer](docs/decisions/0013-make-install-must-not-pair-a-fresh-daemon-with-a-stale-drawe.md)
- [voltaire-gui drops CAP_BPF/CAP_PERFMON before the Go runtime starts, except under gamescope](docs/decisions/0014-voltaire-gui-drops-cap-bpf-cap-perfmon-before-the-go-runtime.md)
- [On a slider device the GUI offers the TDP as one control, and the editor stores "custom" instead of sending bare](docs/decisions/0026-on-a-slider-device-the-gui-offers-the-tdp-as-one-control-and.md)
- [Brightness and volume are the GUI's, and DE-agnostic; HDR was dropped](docs/decisions/0030-brightness-and-volume-are-the-gui-s-and-de-agnostic-hdr-was.md)
- [The same lesson has a second door: `EVIOCGRAB` on a device the drawer only *thinks* is a controller's](docs/decisions/0040-the-same-lesson-has-a-second-door-eviocgrab-on-a-device-the.md)
- [`subscribe`'s `events` list is honoured; it was ignored until v1.3.0](docs/decisions/0042-subscribe-s-events-list-is-honoured-it-was-ignored-until-v1.md)
- [The drawer's sections come from `internal/controls`, and the default list is a transcription of what shipped — not a fresh design](docs/decisions/0134-the-drawer-s-sections-come-from-internal-controls-and-the-de.md)
- [Quickbar customization is a Settings card, saved to config.toml, with gui.toml as a hand-written override](docs/decisions/0135-quickbar-customization-is-a-settings-card-saved-to-config-to.md)
- [The drawer's edge is `panelgeom.Edge`, and every anchor and margin write goes through one accessor](docs/decisions/0136-the-drawer-s-edge-is-panelgeom-edge-and-every-anchor-and-mar.md)
- [The Settings tab renders `DeviceInfo.Toggles` directly, not through `internal/controls`](docs/decisions/0139-the-settings-tab-renders-deviceinfo-toggles-directly-not-thr.md)
- [The Telemetry tab became the Dashboard: charts *and* the live controls](docs/decisions/0140-the-telemetry-tab-became-the-dashboard-charts-and-the-live-c.md)
- [The window shows one lighting card per zone where the drawer shows one card and a zone switch](docs/decisions/0141-the-window-shows-one-lighting-card-per-zone-where-the-drawer.md)
- [Every drawer section is now built twice, and three things had to stop being Window-level singletons for that](docs/decisions/0142-every-drawer-section-is-now-built-twice-and-three-things-had.md)
- [The HSL picker is a popup, not a page, and there is now exactly one of them](docs/decisions/0143-the-hsl-picker-is-a-popup-not-a-page-and-there-is-now-exactl.md)
- [The refresh-rate control is the one GUI control that does not go through the daemon, and `internal/display` says why in its package doc](docs/decisions/0144-the-refresh-rate-control-is-the-one-gui-control-that-does-no.md)
- [Which surface each press opens is the *client's* choice, not the daemon's](docs/decisions/0147-which-surface-each-press-opens-is-the-client-s-choice-not-th.md)
- [A double press emits `gui-open-full` *in addition to* `gui-toggle`, and never instead of it](docs/decisions/0148-a-double-press-emits-gui-open-full-in-addition-to-gui-toggle.md)
- [`gui-open-full`'s consumer is a second GTK surface, and the views it shows are second *instances* rather than a second implementation](docs/decisions/0149-gui-open-full-s-consumer-is-a-second-gtk-surface-and-the-vie.md)
- [`VOLTAIRE_GUI_DUMP_FOCUS=1` exists because the drawer's focus lists were the one checkable thing no test could reach](docs/decisions/0161-voltaire-gui-dump-focus-1-exists-because-the-drawer-s-focus.md)
- [The 2.0 GUI migrations are copies, on the same terms as the daemon's](docs/decisions/0163-the-2-0-gui-migrations-are-copies-on-the-same-terms-as-the-d.md)
- [The drawer has no profile editor; it has a profile *picker](docs/decisions/0167-the-drawer-has-no-profile-editor-it-has-a-profile-picker.md)
- [The mouse wheel scrolls the drawer even over a slider (`Window.wheelScrollsView`)](docs/decisions/0172-the-mouse-wheel-scrolls-the-drawer-even-over-a-slider-window.md)
- [`BuildThemeCSS` defines every token twice — `@z13-*` and `@voltaire-*` — which is what let the token rename be *staged*](docs/decisions/0175-buildthemecss-defines-every-token-twice-z13-and-voltaire-whi.md)

## Go conventions established in this project

- Per-file descriptive comments go **below** the `package` line, not before it.
  Before-package comments must be `// Package X ...` format (revive `package-comments`).
- Every package has exactly one file with the package-doc comment (`// Package X ...`).
  For `cmd/`, that comment is in `root.go`. For `aura`, `hid`, and `cli`, there are
  dedicated doc files (`aura/aura.go`, `hid/doc.go`, `cli/doc.go`).
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
  (exported + unused-parameter + package-comments), gocritic (diagnostic +
  style tags). Listing revive rules replaces its defaults, so package-comments
  was silently off until 2026-11-15; turning it on found three packages with no
  doc comment (`togglegate`, `hidblocker`, the `voltaire-gui` main package).
- Verify config with: `golangci-lint config verify`

## Testing

- Hardware not required. `internal/aura` uses `mockWriter`; `internal/hid` uses
  `os.Pipe()` backed devices via `NewTestDevice`; `internal/drivers/asus`,
  `amd` and `linux` each use a fake sysfs tree (see below).
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
- Coverage, measured 2026-11-15: safety 89%, asus 82%, linux 82%, amd 68%,
  register 92%, cli 76%, device 70%,
  aura 62%, daemon 52%, hid 43%, api 31%, cmd 14%. Every pure package on the
  GUI side — `limits`, `profileui`, `telemetryplot`, `focusgrid`, `mainwin`,
  `popupgeom`, `controls`, `colorconv`, `startup`, `display`, `theme` — sits at
  91–100%, which is
  the whole argument for the cgo boundary: logic moved out of `internal/gui`
  gets tested, logic left inside it cannot be.
  Re-measure with `make cover` (plus `cd api && go test -cover ./...`; a bare
  `./...` compiles the cgo island) rather than trusting
  these. The previous set in this file had drifted 13 points on aura and 6 on
  api, in both directions — a stale number here reads as a measurement, which
  is the same failure as a chart drawn flat at zero.
- aura error branches (write failures) are not covered because mockWriter never errors.

### Fake sysfs (`internal/drivers/asus`, `amd`, `linux`)

Each driver package keeps the sysfs roots it touches in its own `paths.go` as
package **vars**, not consts, purely so tests can redirect them, and has its own
`sysfs_fake_test.go` building only its own subtree: hwmon curve + readings, PPT
and firmware-attributes (`asus`); ryzen_smu, k10temp, amdgpu, the NPU (`amd`);
platform-profile devices, power_supply, powercap, procfs (`linux`). Each
`newFakeSysfs(t)` points its package's vars at a temp tree and restores them on
cleanup. The split was mechanical: all 141 tests kept their names; only four
assertions changed (RPM moved to a daemon test, telemetry uses injected stub
sources, the policy-path test moved to `register`, `NewBattery(caps, nil)`).

Two seams exist specifically to stop tests from touching the developer's machine
— both were added after tests did exactly that:

- `ppdRunner` (`linux/profile.go`) wraps the `powerprofilesctl` exec. `newFakeSysfs`
  replaces it with a recorder; without this, exercising `SetProfile` changes the
  live power-profiles-daemon profile.
- `smuReadFile` / `smuWriteFile` (`amd/smu.go`) wrap the ryzen_smu mailbox I/O.
  `fakeSMU` emulates the driver's write-then-read-response protocol, which plain
  files cannot. `resetSMUProbe(t)` clears the `sync.Once` behind
  `SMUProbeUndervolt` so each case re-probes.

**Never let a test reach a real sysfs write — and in `internal/daemon` none
can** (2026-11-26). `testDev` is the real Z13 assembly (the device-document tests
need what the real drivers describe), but every write method is guarded
(`writeguard_test.go`: it returns `errTestWrite`) and its power limiter is
`deviceFileEnvelope`, which reads no hardware; `TestTestDevCannotWrite` pins it.
A test of a write path uses `fakeDevice`/`fakeDaemon` (`fakes_test.go`):
in-memory drivers with an ordered write log that fails any write made without
`hwMu` (`TestEveryECWriteHoldsHwMu`, `TestHighTDPWritesTheFloorFirst`). Rules
about where a call may appear use the go/ast helper in `astguard_test.go`, not
line scans.

Each driver package's `main_test.go` `TestMain` points every writable sysfs
root at a nonexistent directory before any test runs (a new package gets one in
the same commit that creates it), so a test that forgets the
fake fails with "no such file" rather than touching the machine. It was added
during the armoury port, after `tdp_test.go`'s `usePPTTempDir` (which swapped
only `pptBasePath`) let three tests find the real armoury attributes and try to
write them; their permissions were all that stopped it. `asus`'s fake has the five
asus-nb-wmi PPT files at the 5 W boot cache; `f.withArmouryPPT(t)` adds armoury's
three with the GZ302EA bounds, making armoury the interface in use.

The PPT driver's envelope now reads the kernel, so the daemon's shared `testDev`
reports the device file's envelope (`deviceFileEnvelope` in `devices_test.go`)
and `testEnv` is built from the device file directly — otherwise every
envelope-reading test would depend on whether the machine running it has
asus-armoury. Reads and writes still reach the real driver, so the
rejection-paths-only rule stands: a TDP rejection case must be refused on both
interfaces' ranges, and a PL2/PL3 below its minimum is raised, not refused.

### Hardware verification (on the development machine)

Unit tests cannot see firmware behaviour — #22's limit reset and #24's early wake
were both invisible to them — so a change to power, fan or sleep handling gets a
run on the Z13. The procedure that has worked, and the traps found the hard way:

- **Stop the installed daemon** (`systemctl --user stop voltaire.service
  voltaire.socket`) and run the build under test in the foreground,
  `daemon --no-button`, logging to a file. Build it with `make build-hwtest`,
  which points `amd.smuDriverPath` nowhere so nothing can send a Curve
  Optimizer write (see the 2026-08-14 lockup), and fails unless the override
  really landed. Never hand-write the `-X` flag: it silently does nothing for a
  symbol that no longer exists — the variable moved from `asusz13` to `amd`
  on 2026-11-15 — and Go records the flag in the binary's build info, so even
  grepping the binary for the path passes a build that can still write the
  mailbox. The target checks the symbol table instead.
- **Back up `~/.local/state/voltaire/state.json` first, and never overwrite an
  existing backup** — a rerun after an aborted run would otherwise back up the
  test's own state. Record `platform_profile` before starting and set it back by
  name: the test daemon restores whatever profile its state file names on startup,
  so reading the profile after a hung run gives the wrong answer. Restore the state
  file, then `systemctl --user start voltaire.socket voltaire.service`, and check
  `voltaire-gui` came back too.
- **Power** is measured with RAPL under a `yes` ×32 load, never from the `ppt_*` or
  armoury readback (which does not move when the firmware resets the limits).
- **Suspends:** `sudo -n rtcwake -m no -s 20 && systemctl suspend` gives a suspend
  of known length; the daemon's `resume: wake report` then shows whether it ran the
  full time (`slept`, `hw_sleep`). For kernel-side attribution set
  `/sys/power/pm_debug_messages` to 1 and `file drivers/acpi/ec.c +p` in
  `/sys/kernel/debug/dynamic_debug/control`, and **set both back** afterwards. Each
  suspend interrupts Jeff's session, so ask before a run.
- **Noise in the wake report:** `wake_irq` shows IRQ 1 (i8042) or 9 (acpi) on
  clean resumes too, and the lid `PNP0C0D:00` counts one event on every resume.
  An SD-card `mmc0` suspend failure with errno -84 is the known harmless one
  (see "Do not blame the sleep hook").
- **Sample size:** the #24 early wake comes in streaks; a batch of three can come
  out clean. Interleave the variants and run a dozen or more before concluding
  anything, and keep a no-change control.
- **A/B variants are separate binaries**, built from a temporarily edited copy of
  the source that is restored immediately and checked with `cmp`. Never
  `git stash` for this: one stash reverted a whole port in three files.
- **Shell traps in test harnesses:** a bare `wait` also waits for the
  backgrounded daemon and hangs the script — `wait $pid`; `pkill -f <script>`
  matches the invoking shell's own command line and kills the caller — kill by
  PID; a log-file label with a space breaks the redirect, so the daemon never
  starts and the "test" runs with no daemon at all.

### GUI verification (on the development machine)

`internal/gui` has no tests, so a GUI change is checked with the two
instruments in the decision entries above, and each has traps found the hard
way (2026-08-15):

- **`VOLTAIRE_GUI_DUMP_FOCUS=1 ./voltaire-gui/voltaire-gui -d`** — the lists
  are logged at **Debug**; without `-d` the run prints nothing and an empty
  diff looks like success. Grep `focus list built`, diff before/after.
- **Stop `voltaire-gui.service` first.** A second launch is a GApplication
  `activate` forwarded to the running instance, and the new process exits
  without building anything.
- **The running daemon must be the build under test.** Widgets are built from
  `device-get`, so a GUI feature driven by a new document field renders nothing
  against the installed daemon — and the focus diff comes back *identical*,
  which reads as "the change added no focusable widget". `make build-all`,
  `sudo -n make install`, `systemctl --user restart voltaire.service`.
- **Screenshots:** `VOLTAIRE_GUI_OPEN_FULL=<dashboard|profiles|settings|color>`
  opens the full window on that page; `spectacle -b -n -o <file>` captures it.
  The focus dump names the profiles tab's view `full:custom`.
- **Last baseline** (2026-11-15, after the drawer's expand button): `main` 41,
  `theme` 73, `color` 11, `full:dashboard` 75, `full:custom` 23,
  `full:settings` 24. A reference only — recapture before relying on it.
- Restart `voltaire-gui.service` afterwards; killing test instances by
  `pgrep -x voltaire-gui` PID, never `pkill -f` (it matches the invoking shell).

### Hardware verification on the OXP X2 Mini Pro

The OXP is a second machine reached over ssh, with a few traps of its own
(found 2026-11-17/18):

- **Deploy from the Z13**: the OXP's checkout lags, so build both binaries
  here with `make build-all VERSION=$(git describe --tags --match 'v*'
  --always --dirty)`, copy them over, `sudo install` them to
  `/usr/local/bin`, re-run `setcap cap_bpf,cap_perfmon+ep` on
  `voltaire-gui` (install drops it), then restart `voltaire.service` and
  `voltaire-gui.service`.
- **Suspend over ssh** needs `sudo -n systemctl suspend` (logind refuses a
  non-interactive session) and `sudo -n rtcwake -m no -s 30` for the timed
  wake. Run `sync` first: s2idle without the kernel parameters hard-locks
  the machine, the only way out is a forced power-off, and `/boot` there is
  the EFI system partition, written by `limine-update`.
- **Kernel parameters** live in `/etc/default/limine` (`KERNEL_CMDLINE`);
  `sudo limine-update` regenerates `/boot/limine.conf` (root-only). Back up
  both first, and never overwrite an existing backup.
- **Charge tests**: see the charge-limit decision entry (drain below 90 %,
  watch `power_now`, restart UPower after a module swap). Swapping in a test
  `oxpec` lasts until reboot; `rmmod oxpec && modprobe oxpec` restores the
  stock one. Never write EC registers directly.
- **Ask before** a suspend, a reboot or a module swap: each interrupts Jeff's
  session or changes the machine's charging.

## Build / release

```sh
make build              # go build voltaire (CGO_ENABLED=0), version from git tags via ldflags
make build-gui          # go build voltaire-gui (CGO=1; needs gtk4 + gtk4-layer-shell headers)
                        #   → voltaire-gui/voltaire-gui
make build-all          # both — what `make install` wants (see below)
make build-hwtest       # voltaire-hwtest: SMU mailbox pointed nowhere, for hardware runs (verified)
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
make docs               # serve the website locally (pnpm install + vitepress dev)
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
- `website/` — the docs site: **VitePress** (Jeff, 2026-11-17: replaced Astro
  Starlight, which had replaced the mkdocs `docs/` tree at 2.0), built with
  **pnpm** (`pnpm-lock.yaml` is committed; esbuild's build script is approved
  in `website/pnpm-workspace.yaml`, which pnpm ≥11 reads instead of a
  package.json `pnpm` field, and which also sets `minimumReleaseAge` to two
  weeks so no dependency, transitive ones included, is fresher than that).
  VitePress is pinned to the stable 1.x line (1.6.4); 2.0 was still alpha.
  `make docs` serves it locally, `make docs-build` builds it as CI does. Page
  sources live in `website/docs/`, the sidebar in `docs/.vitepress/config.mts`,
  and the site deploys from `.github/workflows/docs.yml` (pnpm build →
  upload-pages-artifact → deploy-pages) on pushes to main touching
  `website/**`. **There is no landing page** (Jeff: drop straight into the
  overview and table of contents): `docs/index.md` is an ordinary doc page,
  so the root shows the sidebar. Rules for writing pages:
  - **Links are written without the base and without a trailing slash**
    (`/reference/commands#setup`; `/gui/` only because it is a directory
    index). `base: '/voltaire/'` is added by VitePress, and `cleanUrls`
    serves `/voltaire/reference/commands` (GitHub Pages maps it to the
    `.html`). The README, CONTRIBUTING, the GUI README, `cmd/legacyname.go`
    and the AUR install script link to the site in the same form.
  - **Every page carries its own `# Title`** after the frontmatter; VitePress
    does not render `title:` as a heading (Starlight did).
  - **Callouts are `::: info|tip|warning|danger Title`**, labelled Note / Tip /
    Caution / Danger in the config to match Starlight's names.
  - **Heading anchors are VitePress's slugs**, which differ from Starlight's:
    `ryzen_smu` becomes `ryzen-smu`, and an em-dash survives into the id. Four
    headings pin their old anchor with `{#id}` so existing links still work.
    Pages are compiled as Vue templates, so a raw `<word>` or `{{` in prose
    (outside backticks) breaks or vanishes.
  - **Links are checked twice at build time**: VitePress fails on a link to a
    missing page, and `docs/.vitepress/anchors.ts` (a `buildEnd` hook) fails
    on any internal `#fragment` with no matching `id`/`name` on its target,
    the coverage starlight-links-validator gave. Its negative control was run
    (a misspelt fragment fails the build). It checks the generated API page
    too: gomarkdoc's `<a name>` anchors count.
- **`website/docs/reference/api-go.md` is generated — do not
  hand-edit it.** Regenerate with `make docs-api` whenever `api/` doc comments
  change; the target prepends the frontmatter and renames gomarkdoc's `# api`
  heading to `# Go API Reference`.
  Each symbol links to its source line. gomarkdoc takes the repository from
  the git remote unless told otherwise, which here is `dahui/z13ctl`'s `main`
  — a different api with different line numbers — so the target passes
  `--repository.url`/`--repository.default-branch` (`API_REPO_URL`,
  `API_REPO_BRANCH`: `dahui/voltaire`, `main`) and pins gomarkdoc (v1.1.0)
  so a regeneration diffs only what the api changed. The links resolve once
  the repo is renamed and this branch is on main, which is also when the site
  starts deploying from it.
- `reference/daemon.md` holds the user-facing socket protocol tables; keep them
  in sync with `dispatch()` in `internal/daemon/server.go`.
- `reference/protocol.md` — technical HID protocol reference for developers.
- `dev/architecture.md`, `dev/device-files.md`, `dev/writing-a-driver.md`,
  `dev/plugins.md` — the developer guide (sidebar "Developers"). The
  device-file page documents every schema key and holds a complete example
  between `<!-- example-device:start/end -->` markers; both are checked by
  `internal/drivers/register/docs_test.go`, so a new `toml:` key fails
  `make test` until it is documented there. `plugins.md` describes the M5 tier
  and is marked planned until `plughost` exists; the package comment of
  `internal/driver` (`doc.go`) is the driver contract the guide restates.
- `devices/` — the user-facing compatibility pages (Jeff, 2026-11-17: "users
  will want to know [what a device does not support] and why"). `index.md`
  holds the feature × device matrix, every "No" linking to a *why* section on
  that device's page; each device file has a page at `devices/<id>.md` with
  the same rows as a Compatibility table, what it needs installed, and a
  "Not supported yet, and why" section. `register/docs_test.go`'s
  `TestEveryDeviceHasACompatibilityPage` fails a device file with no page or
  no link from the matrix (negative control run). A change to a device file's
  capabilities, or a kernel that closes a gap, means updating both pages.
  The OXP's RGB gap needs **two** things to close, which the page says: the
  hid-oxp X2 series in a released kernel, and a voltaire lighting driver for
  kernel LED devices (only `aura-hid` exists) plus an OXP `[lighting]` block.
- `dev/smoke-z13.md` — the hardware smoke checklist (edited in place during
  smoke runs; it is a site page now, same content).
- `migrating-from-z13ctl.md` — the 2.0 migration story, linked from the README,
  release notes, and AUR notices. New shims and their removal timeline belong
  in its compatibility table.

## Current status and next steps

**Everything below M6 ships as voltaire 2.0. There is exactly one release.**
The milestones M0–M6 in `.claude/plans/voltaire-2.0-roadmap.md`
are sequencing, not release boundaries; an earlier revision of that plan
assigned 2.1.0/2.2.0/2.3+ to M4/M5/M6, which was never requested and
contradicts the plan's own title. Do not reintroduce dot releases.

**Long-lived plans live in this repo's `.claude/plans/`** (gitignored, so it
survives branch switches), not in `~/.claude/plans/`, which is cleaned
periodically: the roadmap, the AUR playbook and the pm_table plan were all lost
from there between August and October and had to be recovered from a session
transcript (2026-10-09). Write new ones here and point at them from this file.

| Milestone | State |
|---|---|
| M0 — two live GUI bugs + AllEvents subscription | done |
| M1 — driver extraction, registry, device TOMLs, safety engine | done |
| M2 — `device-get` protocol, generic `feature` commands, GUI adopts limits | done (capability absence hiding controls landed 2026-11-15) |
| M3 — rename, repo merge, two binaries, shims, docs, packaging | code done; all three parity gates passed 2026-08-09. Release mechanics outstanding: merge to main, GitHub repo rename, `api/v2.0.0` then `v2.0.0` tags, drop the `replace` in go.mod, `GOPROXY=direct` rehearsal, archive z13gui, AUR playbook, comms |
| M4 — window split, control registry, movable quickbar, full window + dashboard + double-tap, telemetry ring | **complete** but for the Gaming Mode re-check of the 2026-11-15 fixes (Open work item 2). Landed: `internal/telemetryring`; the device-document prerequisites (`battery.health`, `telemetry.{power_draw,history_seconds}`); the 1 Hz sampler + `telemetry-history`; `internal/controls` + `gui.toml`; `panelgeom.Edge` + the movable quickbar; double-tap `gui-open-full`; the in-surface popup layer (`popupgeom` — not in the original list, and it replaced both the expanding selector and the cycle buttons); and the window split, each of `errBarView`, `themeView`, `lightingView`, `profileSection`, `autoswitchSection`, `dashboardView`, `customView` owning its own widgets and focus list. The full window is a real toplevel with Dashboard, Profiles and Settings tabs hosting second *instances* of those views through the `viewHost` seam, `internal/mainwin` deciding tabs and geometry; under gamescope the same pages live inside voltaire's own fullscreen surface via `fullSurfaceHost`, since what does not composite there is a second *toplevel*, not a second layout. Its dashboard is eight cards over the full expanded telemetry set, its Profiles page commits through one dirty-tracked button, and the bundled CSS is migrated to `@voltaire-*` (leaving the `@z13-*` aliases with no in-tree consumer). Every rule behind those three design passes — desktop density 2026-08-13, one-commit and autoswitch card 2026-08-14, expanded telemetry / battery state-of-charge / Net card the same day — has its own entry above or in `internal/gui/CLAUDE.md`, and the `VOLTAIRE_GUI_DUMP_FOCUS` fingerprint held byte-identical through all of them bar three deliberate re-baselines (twice `full:custom`, once the picker: `full:color` gone and `color` re-shaped, since a popup carries neither a back item nor the error-bar sentinel). The **Settings tab** landed 2026-08-14: generic toggle rows rendered from `DeviceInfo.Toggles` through `internal/settingsui`, plus the daemon-side notify fix two of the three toggle write paths were missing (both have their own entries above). Later the same day the Telemetry tab became the **Dashboard**: the eight tiles keep the top of the page and the live controls — profile, autoswitch, charge limit, refresh rate (`internal/display`, the one control that does not go through the daemon) and RGB — flow across the width beneath them, with autoswitch moved off the Profiles page on the rule that this page changes what the machine is *doing* while the editor changes what a profile *says*. That made every drawer section a second instance. Later still the **HSL picker stopped being a page at all** and became a single popup shared by both surfaces (`colorpopup.go`), which deleted `colorview.go` and the whole non-tab-stack-child apparatus with it; all of it has entries above. On 2026-08-14/15 a batch of small features from the feature survey landed, each with an entry above: the SMU **applied-gate** on every Curve Optimizer clear (after the hard lockup), **`tuning-reset`** (CLI + the editor's Reset All), **`status --watch`**, **`pending_reboot`** (Settings banner) and **`charge_mode`** (dashboard battery header), the fan-curve **operating-point dot**, and **named fan-curve presets**. **Quickbar customization** landed 2026-10-09 (its own entry above). **Remaining:** the Gaming Mode re-check of the 2026-11-15 fixes (expand button, refresh rate hidden, window controls, the gamepad blocker with its capabilities now kept only there) — the five unticked boxes in `dev/smoke-z13.md`; see Open work below. |
| M5 — external plugin tier + OXP X2 Mini Pro device | not started |
| M6 — ROG Ally + generic-AMD device TOMLs | not started |
| OXP RGB | deferred past 2.0 — needs Linux 7.2 `hid-oxp` in CachyOS |

**z13ctl fixes are ported through 1.4.3** (2026-11-15): every main commit since
v1.3.2 has a counterpart here. Fixes usually land on main first and are ported
in one commit per release; `git log v1.4.3..main` on main is the list still to
port. 1.4.3 ran the other way: the per-zone lighting-off fix was written here
first and back-ported (main's version is a pure `zoneOffState`, because main's
`handleOff` writes HID directly); #29's missing `go.sum` entry never applied
here (the api module resolves through the `replace`); and the dependency
update — `x/sys` v0.50.0, which needs Go 1.26, `godbus/dbus` v5.2.4, a newer
`go-evdev`, `pflag` v1.1.0 — was applied to both, with CI's golangci-lint pin
moved to v2.15.0 in step with the Go version.
Audited commit by commit on 2026-10-09: every fix on main since the branch point
has its counterpart here. The only gap was wording from main's 1.4.0 docs commit
(`b0066d9`): README, the site index and the api guide's power-limit ranges, now
ported.

Carried into M5 from M2, and **done** 2026-11-15 against a synthetic device
rather than waiting for the OXP: capability *absence* hiding controls (see the
device-genericity entry). (Firmware profile names from `api.ProfileInfo` and `limits.Curve`
as a slice sized by the served `fans.points` both landed 2026-10-09 with the
device-values audit.) The two device-document fields the plan specified and M2
left out — `battery.health` and `telemetry.{power_draw,history_seconds}` — are
**done**; they were M4 prerequisites, since the dashboard and telemetry ring
were specified to read them. `undervolt_available` is still not derived from
capabilities (it is no longer a live probe: see `Undervolter.Available`).

**API parity with main** (checked 2026-10-09 by diffing every exported symbol
and JSON key): main's only addition since the branch, `State.TDPLimits` from
z13ctl 1.4.0, is served here too — see the `tdp_limits` entry above. Repeat the
diff whenever a main release touches `api/`.

### Open work (as of 2026-11-26, in rough priority order)

0. **The quality pass** (`.claude/plans/quality-pass.md`, its Progress list
   says what landed): per-machine permissions (`internal/perms`; packages run
   setup), the safety boundary as types, daemon tests on fakes, the protocol
   and api cleanup, GUI sync/refresh/slider consolidation, the lint ratchet,
   and this file split into an index plus `docs/decisions`. Verified on
   hardware 2026-11-26: the Z13 (setup from an installed binary, four reboots,
   the boot pass resetting udev's renumbered-node strays, daemon writes on the
   new grants, a `setup --remove` round trip restoring the exact permissions)
   and the Ally (old static install replaced, a reboot); `make snapshot`
   package scripts checked (guarded preremove, rpm posttrans, Arch
   post_upgrade, no permission artifact in any package). **Left:** the OXP
   (wiped; once CachyOS is back) and a cover detach/reattach on the Z13.
1. **ASUS ROG Xbox Ally X** (`.claude/plans/xbox-ally-x.md`): device file,
   Steam yield, stock rows (AC and battery) and the charge limit done; the ring
   lighting (Aura over hidraw interface 2, 0x5D feature reports — proven on the
   machine) and the Gaming Mode check remain; controller settings wait for the
   upstream hid-asus Ally series.

1. ~~Quickbar customization~~ — landed 2026-10-09 (entry above;
   `.claude/plans/quickbar-customization.md`). M4 is complete but for the
   gamescope pass.
2. **Gamescope Gaming Mode hardware pass** — run by Jeff 2026-11-15: the GUI,
   the hosted full window and the larger UI all worked, with two exceptions,
   both fixed the same day and needing a re-check in Gaming Mode: the
   refresh-rate card showed "No screen" (now hidden there; see the
   refresh-rate entry), and a controller had no way into the full window (the
   drawer footer now has an expand button beside the theme button, calling
   `openFull`; `main` focus grid 40 → 41, `15:1:footer`, every other line
   byte-identical).
   The same pass found the hosted full window had no window controls at all;
   the window now draws its own title row with minimize/maximize/close
   (minimize/close under gamescope), which also fixed minimize leaving the
   controller grabbed. Entry in `internal/gui/CLAUDE.md`; plan in
   `.claude/plans/window-controls.md`. Needs the Gaming Mode re-check too.
   Since then the GUI keeps CAP_BPF/CAP_PERFMON only under gamescope (the
   caps-drop entry), so the same session must confirm the blocker still
   attaches there with a real controller and a game running. All five checks
   are the unticked boxes in `dev/smoke-z13.md`; they are the last thing
   before the M3 release mechanics.
3. **pm_table telemetry** — parser and fixtures done 2026-10-09; the sampler
   and card are deliberately not built (every read is an SMU message; see the
   pm_table entry). Revisit with M5, where the OXP reads the same table:
   `.claude/plans/pm-table-telemetry.md`.
4. ~~`viewHost.back` dead branches~~ — removed 2026-10-09; focus dump
   byte-identical.
5. ~~`handleUndervoltReset` test with a fake undervolter~~ — landed
   2026-10-09 (`undervolt_reset_test.go`).
6. ~~`make docs-api` emits wrong source links~~ — fixed 2026-10-09: the
   target names the repository and pins gomarkdoc.
7. ~~Read device values from the device, not from literals~~ — **done** (Jeff,
   2026-10-09). The precedence is: autodetected from the kernel or firmware, then
   the device TOML, then a built-in default. That applies to every limit, range,
   count or vocabulary, in validation, UI, help text and docs alike.
   **Done 2026-10-09** (the power audit found no decision logic using literals;
   everything it found was text):
   - Help names no device numbers and points at `tdp --get`, which now prints the
     safe maximum.
   - The floor notices read `env.FloorCurve` (`safety.FloorSpan`) and the fan
     shape's `PWMMax`.
   - The charge-limit range is device data (`battery.charge_limit_min/max` →
     `api.BatteryInfo`; the kernel publishes none).
   - The GUI's undervolt and battery sliders bind to `limits` and follow the
     daemon live (see the device-get entry above).
   - The docs scope Z13 numbers as the GZ302EA's.

   **The wider audit is done and is now a plan:**
   `.claude/plans/device-values-audit.md`, covering profiles, fans, lighting,
   toggles/charger/battery, GPU and telemetry, SMU, and setup. It subsumes the
   roadmap's M2→M5 carry items. **Every section (§0–§7) is done** (the plan's
   Progress list); its §0 safety fix — `status`/`status --watch` and
   `fancurve --get` reading the EC while the daemon had `ecWedged` latched —
   is `readLive` in `cmd/hardware.go`, guarded by `cmd/live_test.go`.
   *Armoury verified live 2026-10-09* after re-running setup. Under `yes` ×32,
   measured with RAPL on performance (firmware alone 70.0 W at 92 °C):
   - 30 W custom held 31.9 W through a foreign `pwm_enable=2` and a same-value
     profile write. The policy watcher re-applied within the same second each
     time.
   - 20 W was refused (below 28).
   - 80 W `--force` put the floor in place before the PPT write and held
     80.0 W at 94 °C.
   - `tdp --reset` landed on balanced at 52.0 W and skipped the SMU write.
8. ~~Full hardware smoke of the final tree~~ — run 2026-11-15 on
   `feat/v2-refactor`; every desktop box passed (`dev/smoke-z13.md` is ticked
   and carries a run note). It found five faults, all fixed and re-verified
   the same day (1517879, 2967c14 and the findings commit): the drawer
   reopening on the window's focus list, oversized text from the blocker's
   file capabilities (the caps-drop entry), stale armoury values shown on
   firmware profiles (`ReadDisplayed`), a WARN on every charger edge, and
   Adwaita showing through on GNOME (`internal/gui/CLAUDE.md`). The checklist's
   five Gaming Mode boxes are item 2's re-check.
9. ~~Per-zone `off` discards the zone's lighting state~~ — fixed 2026-11-15
   (entry above; `lighting_off_test.go`). The same code is on main, where it
   is still live in every z13ctl 1.x release.
10. **M3 release mechanics** (table above), with the AUR side in
   `.claude/plans/voltaire-aur-playbook.md` (corrected 2026-11-15 for the
   single `voltaire-bin` package and the 1.4.x releases).
11. **M5** (plugin tier + OXP device, carrying the remaining M2 item above) and
   **M6** (Ally + generic AMD TOMLs).
12. **Ports from main**: none outstanding (1.4.3 ported 2026-11-15); after any
    new main release, `git log v1.4.3..main`.
13. **Port the hidraw interface fix to main (z13ctl 1.4.4)** — the lighting
    grant there still covers every interface of the cover keyboard, its typing
    interface included (entry above). The port is the rule change plus setup's
    revocation; Jeff tags and decides on disclosure.
14. **Z13 button candidates**: an evtest capture on "Asus WMI hotkeys" while
    pressing the Fn keys, keeping only keys that arrive there and do nothing
    else, would give the Settings chooser more than the Armoury Crate button.
15. **Buttons through InputPlumber** (Jeff, 2026-11-16) — **built and verified
    on the OXP X2 Mini Pro 2026-11-17** (decision entries above), with the
    buttons-only OXP device file. Remaining: the Z13 under InputPlumber (the
    plan's phase 0: does the grab of "Asus WMI hotkeys" break tablet mode, and
    add `inputplumber = "QuickAccess"` to `armoury-crate` once seen working),
    the GUI chooser's wording about taking the button from Steam, and the Ally
    with M6. Plan: `.claude/plans/inputplumber-buttons.md`.
16. **OXP X2 Mini Pro power (M5)** (Jeff, 2026-11-16): real TDP through
    ryzen_smu (amkillam fork) when present, `amd-pmf` platform profiles as the
    fallback with an install note; no kernel module of ours. The lockup rules
    learned in oxp-tdpd are requirements. Daemon slider model, pin, GUI,
    read-only fan RPM, the no-ryzen_smu fallback, undervolt as a device
    setting (verified on hardware) and the device page
    (`website/.../devices/onexplayer-x2-mini-pro.md`) built and verified
    2026-11-17 (entries above). **Done for now** (Jeff, 2026-11-26: "we are solid on voltaire
    work for that machine"). Left, each blocked outside voltaire:
    - the charge limit: cause confirmed on hardware (BIOS 0.22 and 0.25;
      decision entry above), one-line patch (X2 → `oxp_apex`) sent by Jeff
      to platform-driver-x86 2026-11-17 and not yet taken; then a
      kernel-version gate in the device file;
    - joystick RGB: needs the unmerged hid-oxp X2 series;
    - suspend needs `amd_iommu=off mem_sleep_default=s2idle` on 0.25 too
      (without them it still hard-locks), which costs the NPU card.
    This EC reports `status` "Charging" while the pack is held or full, so
    the battery headline said "Charging" with no flow. Fixed 2026-11-26 as
    device data: `battery.charging_needs_flow = true` makes the linux
    battery driver's `Status` check a Charging read against the unsigned flow
    (`ReadBatteryFlowW`, `chargingByFlow`: under 0.1 W is not charging, or
    full at 100 %). Only a Charging read is questioned, an unreadable flow
    keeps the kernel's word, and a device that does not declare it is
    untouched. `TestChargingNeedsFlow` (negative control run). Not yet seen
    on the OXP: the machine was offline when it landed.
    The drawer's gamepad hints now wait for a 1 s rest (`internal/gui/CLAUDE.md`,
    found on the OXP); its smoke box is unticked until re-checked.
    Plan: `.claude/plans/oxp-power.md`;
    platform state and evidence: `.claude/plans/oxp-x2-mini-pro-status.md`.

`docs/history.md` keeps what this file said about the daemon and the GUI
before the 2.0 merge.
