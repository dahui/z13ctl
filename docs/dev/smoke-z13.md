# Z13 hardware smoke test

The hardware parity gate for the driver-registry refactor (v1.4.0). The
hermetic suite proves the wiring, the safety rules, and the wire protocol; it
cannot prove that the drivers still drive the machine. This checklist is the
other half: run it on a real 2025 ROG Flow Z13 before merging a branch that
touches the driver layer, and before tagging a release from one.

Extra attention goes to the paths that have **never run against hardware in
their extracted form**:

- device detection and assembly (DMI match → TOML → registry)
- the `aura-hid` lighting driver (apply, off, brightness, hotplug reopen)
- the `evdev-key` button driver (Armoury Crate button, shared open — no grab)
- the safety engine driving real sysfs (fan floor, fail-closed TDP, release
  ordering) from both the daemon and the no-daemon CLI

Every unchecked box blocks the merge. A failed box gets a note (what happened,
journal lines) rather than a workaround.

## Conventions

Run everything as your ordinary user. Helpers used throughout:

```sh
SOCK="$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock"
CURVE=$(dirname "$(grep -l asus_custom_fan_curve /sys/class/hwmon/hwmon*/name)")
PPT=/sys/devices/platform/asus-nb-wmi
PROFILE=/sys/firmware/acpi/platform_profile
```

`nc` is openbsd-netcat. Sysfs is ground truth: after any set, believe the file,
not the command's output.

## 0. Build and preconditions

- [ ] `make build && make test && make lint` — all green, 0 issues, on the
      exact commit under test (plus `cd api && go test ./... && golangci-lint
      run ./...`).
- [ ] `./z13ctl --version` prints the expected version.
- [ ] `sudo ./z13ctl setup` succeeds (rules written, perms applied).
- [ ] Daemon units installed but **stopped** for the first sections:
      `systemctl --user stop z13ctl.service z13ctl.socket`

## 1. Detection and assembly

- [ ] `./z13ctl list` shows both Aura devices (keyboard `0b05:1a30`, lightbar
      `0b05:18c6`).
- [ ] `./z13ctl status` prints APU temperature, fan RPM + mode, profile, TDP,
      and battery — and **no** `note: no device data matches this machine` on
      stderr. That note means DMI matching failed and everything below is
      running on the fallback assumption; stop and fix the device file first.

## 2. Lighting, no daemon (aura-hid driver)

- [ ] `./z13ctl apply --mode static --color red` — keyboard and lightbar both
      light red.
- [ ] `./z13ctl apply --mode cycle --speed fast` — both animate.
- [ ] `./z13ctl apply --mode static --color 00FF88 --device lightbar` — only
      the lightbar changes; the keyboard keeps cycling.
- [ ] `./z13ctl brightness low`, then `./z13ctl brightness high` — brightness
      changes without restarting the effect's animation.
- [ ] `./z13ctl brightness off` — both zones dark; `./z13ctl brightness high`
      brings them back.
- [ ] `./z13ctl off` — everything off.
- [ ] `./z13ctl apply --mode static --color red --dry-run` — prints the packet
      hex, changes nothing.

## 3. Fan curves, no daemon

- [ ] `./z13ctl fancurve --get` shows the mode and an 8-point curve.
- [ ] `./z13ctl fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"`
      succeeds; `cat $CURVE/pwm1_enable $CURVE/pwm2_enable` → `1` / `1`, and the
      point files hold the values as written.
- [ ] `./z13ctl fancurve --reset` — both `pwm*_enable` back to `2`.

## 4. TDP and the fan floor, no daemon

The engine's fail-closed ordering, on real sysfs. Keep the high-TDP step brief.

- [ ] `./z13ctl tdp --get` matches `cat $PPT/ppt_pl1_spl` and friends.
- [ ] `./z13ctl tdp --set 45` — all five `ppt_*` files read 45.
- [ ] `./z13ctl tdp --set 80` (no `--force`) — refused, names the 75 W safe max
      and the 93 W ceiling. `ppt_pl1_spl` still 45.
- [ ] `./z13ctl tdp --set 80 --force` — succeeds, **and** `pwm*_enable` are `1`
      with every curve point at or above the floor ramp (bottom 127). The
      warning about running without the daemon prints.
- [ ] `./z13ctl fancurve --reset` while at 80 W — refused (releasing to
      firmware auto above the safe max is exactly what the floor forbids).
- [ ] `./z13ctl tdp --reset` — `$PROFILE` reads `balanced`, `ppt_*` hold
      balanced's stock row, `pwm*_enable` back to `2`.

## 5. Undervolt, toggles, battery — no daemon

- [ ] `./z13ctl undervolt --get` → `not set (daemon not running)` (or the
      module-missing message if ryzen_smu isn't installed — then skip CO boxes
      throughout).
- [ ] `./z13ctl undervolt --set -10` applies; `--set -41` and `--set 1` are
      refused with the range message. `./z13ctl undervolt --reset` afterwards.
- [ ] `./z13ctl bootsound --get`, flip it with `--set`, confirm in
      `/sys/class/firmware-attributes/asus-armoury/attributes/boot_sound/current_value`,
      restore it.
- [ ] `./z13ctl paneloverdrive --get` / `--set` — same round-trip, restored.
- [ ] `./z13ctl batterylimit --set 80` —
      `/sys/class/power_supply/BAT*/charge_control_end_threshold` reads 80.
      Restore your usual value.

## 6. Daemon start and state restore

- [ ] `./z13ctl apply --mode static --color blue`, then
      `systemctl --user start z13ctl.socket z13ctl.service` — service reaches
      `active (running)`; journal shows the startup line and no errors.
- [ ] Lighting still blue after the start (state restore ran; the daemon opened
      its own HID handle).
- [ ] Socket activation: `systemctl --user stop z13ctl.service` (leave the
      socket), run `./z13ctl profile --get` — the daemon auto-starts and
      answers.
- [ ] `printf '{"cmd":"get-state"}\n' | timeout 5 nc -U "$SOCK"` — one JSON
      line, `"ok":true`, with `undervolt_available` and `on_ac` present.

## 7. Profiles and custom profiles (daemon)

- [ ] `./z13ctl profile --set performance` — `$PROFILE` reads `performance`,
      `ppt_*` hold performance's stock row (issue #12: z13ctl writes it, the
      firmware does not).
- [ ] `./z13ctl tdp --set 40` while on a firmware profile — creates and
      activates `custom`; `./z13ctl profile --get` says `custom`, sysfs PPT
      reads 40.
- [ ] `./z13ctl profile --create gaming`, `./z13ctl tdp --set 35 --profile
      gaming` — prints "stored… not applied", and sysfs PPT is **unchanged**
      (still 40).
- [ ] `./z13ctl profile --set gaming` — PPT now 35. `./z13ctl profile --set
      custom` — PPT back to 40 (A→B→A gives the same machine).
- [ ] `./z13ctl profile --set balanced` — stock row restored, fans auto, CO
      cleared; `./z13ctl profile --list` still shows both custom profiles.
- [ ] `./z13ctl profile --create balanced` — refused (reserved name).
- [ ] `./z13ctl profile --set no-such-profile` — refused, not forwarded to
      `platform_profile`.

## 8. Reconcile watcher (issue #15)

- [ ] `./z13ctl profile --set custom` with a fan curve set (give `custom` one
      via `./z13ctl fancurve --set …` if needed). Then
      `powerprofilesctl set power-saver`: `$CURVE/pwm1_enable` drops to `2` and
      returns to `1` within ~4 s without any z13ctl command. Restore with
      `powerprofilesctl set balanced` (the watcher must **not** write
      `platform_profile` back — `$PROFILE` stays whatever PPD set).

## 9. Autoswitch

- [ ] `./z13ctl autoswitch --on --ac balanced --battery gaming`, then unplug
      the charger — after the ~4 s settle window the gaming profile is applied
      (PPT reads 35). Replug — balanced comes back, custom profiles intact.
- [ ] A quick plug-bounce (unplug, replug within ~2 s) causes **no** profile
      churn.
- [ ] `./z13ctl autoswitch --clear` when done.

## 10. Armoury Crate button (evdev-key driver)

- [ ] Subscribe and press the button:

      ```sh
      ( printf '{"cmd":"subscribe","events":["gui-toggle"]}\n'; sleep 20 ) | nc -U "$SOCK"
      ```

      Each press during the window prints `{"ok":true,"event":"gui-toggle"}` —
      note `"ok":true` on the streamed event (the v1.2.1 wire fix).
- [ ] No grab regression (issue #10): with the session running, detach the
      keyboard cover and reattach it — the desktop leaves tablet mode and the
      cover keyboard types. If it stays in tablet mode, something grabbed the
      hotkeys device exclusively.
- [ ] `systemctl --user stop z13ctl.service z13ctl.socket`, run
      `./z13ctl daemon --no-button` in a terminal — button presses produce no
      events and the journal shows the watcher was never started. Ctrl-C, then
      `systemctl --user start z13ctl.socket z13ctl.service`.

## 11. Keyboard hotplug (aura-hid Reopen)

- [ ] With the daemon running and a visible effect applied: detach the cover,
      wait a few seconds, reattach — the keyboard's RGB effect comes back on
      its own within ~4 s (poll tick + udev chmod retry). Per-zone overrides
      survive: set the lightbar a different color first and confirm both zones
      restore to their own states.

## 12. Sleep / resume

Set up the full volatile state first: `./z13ctl profile --set custom` with a
fan curve, a custom TDP (safe range, e.g. 40 W), and `./z13ctl undervolt --set
-10`.

- [ ] Suspend (lid or `systemctl suspend`). While asleep: the fans spin down
      and stay down, and the lightbar is dark (the pre-sleep release +
      lightbar-off hook).
- [ ] Resume: lighting restored on both zones, `$CURVE/pwm1_enable` back to
      `1`, PPT back to the custom value, and the journal shows the CO
      reapply. The machine **stays** asleep until woken — a suspend that
      aborts within seconds is a wakeup-source problem; read
      `/sys/power/pm_wakeup_irq` before blaming the hook.
- [ ] High-TDP variant: `./z13ctl tdp --set 80 --force`, suspend, resume — the
      journal shows power lowered *before* the fan release on the way down,
      and the floor curve re-applied with the limit on the way back.
- [ ] Cleanup: `./z13ctl tdp --reset`.

## 13. Cleanup

- [ ] `./z13ctl tdp --reset`, `./z13ctl fancurve --reset` (if not already on
      a stock profile), `./z13ctl undervolt --reset`.
- [ ] `./z13ctl profile --delete gaming` (and any other test profiles),
      `./z13ctl profile --set balanced`.
- [ ] `./z13ctl batterylimit --set <your usual>`, boot sound / panel overdrive
      back to preference, lighting back to preference.
- [ ] `systemctl --user status z13ctl.service` — still healthy, journal free
      of errors from the whole run.
