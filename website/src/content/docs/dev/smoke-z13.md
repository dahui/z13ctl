---
title: Z13 Hardware Smoke Test
description: The manual on-hardware verification checklist run before each release.
---

The hardware parity gate for the 2.0 refactor — the driver registry, the
safety engine, the capability protocol, and the voltaire rename. The hermetic
suite proves the wiring, the safety rules, and the wire protocol; it cannot
prove that the drivers still drive the machine. This checklist is the other
half: run it on a real 2025 ROG Flow Z13 before merging a branch that touches
the driver layer, and before tagging a release from one.

Run it against the binary under test (`./voltaire`), not an installed one.
Sections 6 onward need a daemon: either install the build, or stop the units
and run `./voltaire daemon` yourself. The units are `voltaire.socket` /
`voltaire.service`; the daemon answers on both the voltaire socket path and
the pre-rename z13ctl one for the whole 2.x line, and one §6 box checks
exactly that.

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
SOCK="$XDG_RUNTIME_DIR/voltaire/voltaire.sock"   # the z13ctl compat path answers identically
CURVE=$(dirname "$(grep -l asus_custom_fan_curve /sys/class/hwmon/hwmon*/name)")
PPT=/sys/devices/platform/asus-nb-wmi
PROFILE=/sys/firmware/acpi/platform_profile
```

`nc` is openbsd-netcat. It is **not** installed on a stock CachyOS/Arch
desktop, so where a box below pipes into `nc -U "$SOCK"`, this works anywhere
Python does:

```sh
ask() {  # ask '{"cmd":"get-state"}'  — one request, one reply
  python3 -c 'import socket,sys,os
s=socket.socket(socket.AF_UNIX); s.settimeout(5)
s.connect(os.environ["XDG_RUNTIME_DIR"]+"/voltaire/voltaire.sock")
s.sendall(sys.argv[1].encode()+b"\n"); print(s.makefile().readline().strip())' "$1"
}
```

For the streaming case (`subscribe`), drop the `settimeout` and loop over
`readline()` instead.

Sysfs is ground truth: after any set, believe the file, not the command's
output.

## 0. Build and preconditions

- [ ] `make build && make test && make lint` — all green, 0 issues, on the
      exact commit under test (plus `cd api && go test ./... && golangci-lint
      run ./...`).
- [ ] `./voltaire --version` prints the expected version.
- [ ] `sudo ./voltaire setup` succeeds (rules written, perms applied).
- [ ] Daemon units installed but **stopped** for the first sections:
      `systemctl --user stop voltaire.service voltaire.socket`

## 1. Detection and assembly

- [ ] `./voltaire list` shows both Aura devices (keyboard `0b05:1a30`, lightbar
      `0b05:18c6`).
- [ ] `./voltaire status` prints APU temperature, fan RPM + mode, profile, TDP,
      and battery — and **no** `note: no device data matches this machine` on
      stderr. That note means DMI matching failed and everything below is
      running on the fallback assumption; stop and fix the device file first.

## 2. Lighting, no daemon (aura-hid driver)

- [ ] `./voltaire apply --mode static --color red` — keyboard and lightbar both
      light red.
- [ ] `./voltaire apply --mode cycle --speed fast` — both animate.
- [ ] `./voltaire apply --mode static --color 00FF88 --device lightbar` — only
      the lightbar changes; the keyboard keeps cycling.
- [ ] `./voltaire brightness low`, then `./voltaire brightness high` — brightness
      changes without restarting the effect's animation.
- [ ] `./voltaire brightness off` — both zones dark; `./voltaire brightness high`
      brings them back.
- [ ] `./voltaire off` — everything off.
- [ ] `./voltaire apply --mode static --color red --dry-run` — prints the packet
      hex, changes nothing.

## 3. Fan curves, no daemon

- [ ] `./voltaire fancurve --get` shows the mode and an 8-point curve.
- [ ] `./voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"`
      succeeds; `cat $CURVE/pwm1_enable $CURVE/pwm2_enable` → `1` / `1`, and the
      point files hold the values as written.
- [ ] `./voltaire fancurve --reset` — both `pwm*_enable` back to `2`.

## 4. TDP and the fan floor, no daemon

The engine's fail-closed ordering, on real sysfs. Keep the high-TDP step brief.

- [ ] `./voltaire tdp --get` matches `cat $PPT/ppt_pl1_spl` and friends.
- [ ] `./voltaire tdp --set 45` — all five `ppt_*` files read 45.
- [ ] `./voltaire tdp --set 80` (no `--force`) — refused, names the 75 W safe max
      and the 93 W ceiling. `ppt_pl1_spl` still 45.
- [ ] `./voltaire tdp --set 80 --force` — succeeds, **and** `pwm*_enable` are `1`
      with every curve point at or above the floor ramp (bottom 127). The
      warning about running without the daemon prints.
- [ ] `./voltaire fancurve --reset` while at 80 W — refused (releasing to
      firmware auto above the safe max is exactly what the floor forbids).
- [ ] `./voltaire tdp --reset` — `$PROFILE` reads `balanced`, `ppt_*` hold
      balanced's stock row, `pwm*_enable` back to `2`.

## 5. Undervolt, toggles, battery — no daemon

- [ ] `./voltaire undervolt --get` → `not set (daemon not running)` (or the
      module-missing message if ryzen_smu isn't installed — then skip CO boxes
      throughout).
- [ ] `./voltaire undervolt --set -10` applies; `--set -41` and `--set 1` are
      refused with the range message. `./voltaire undervolt --reset` afterwards.
- [ ] `./voltaire bootsound --get`, flip it with `--set`, confirm in
      `/sys/class/firmware-attributes/asus-armoury/attributes/boot_sound/current_value`,
      restore it.
- [ ] `./voltaire paneloverdrive --get` / `--set` — same round-trip, restored.
- [ ] `./voltaire batterylimit --set 80` —
      `/sys/class/power_supply/BAT*/charge_control_end_threshold` reads 80.
      Restore your usual value.

## 6. Daemon start and state restore

- [ ] `./voltaire apply --mode static --color blue`, then
      `systemctl --user start voltaire.socket voltaire.service` — service reaches
      `active (running)`; journal shows the startup line and no errors.
- [ ] Lighting reflects the daemon's **saved** state after the start — not
      darkness, and not a half-applied zone (state restore ran; the daemon
      opened its own HID handle). It will *not* be the blue you just applied:
      a no-daemon `apply` writes hardware without persisting, so startup
      restores what the state file holds. To see blue survive, apply it with
      the daemon already running.
- [ ] Socket activation: `systemctl --user stop voltaire.service` (leave the
      socket), run `./voltaire profile --get` — the daemon auto-starts and
      answers.
- [ ] `printf '{"cmd":"get-state"}\n' | timeout 5 nc -U "$SOCK"` — one JSON
      line, `"ok":true`, with `undervolt_available` and `on_ac` present.
- [ ] The same request against the **compat socket**
      (`$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock`) answers identically — this is
      what keeps the Decky plugin and pre-2.0 clients working, and it must
      hold under systemd (both `ListenStream=` fds) and under a hand-run
      `./voltaire daemon` (self-created sockets) alike.
- [ ] `ask '{"cmd":"device-get"}'` returns the capability document, and its
      numbers are the device file's, not defaults: fan shape, `tdp_min` /
      `tdp_max_safe` / `tdp_max_forced`, the floor curve, profile names,
      lighting zones, the toggle list, and the undervolt range. A capability
      the machine lacks is an **absent key**, never an error.
- [ ] `./voltaire feature --list` names the same toggles as the document;
      `--get <id>` reads hardware; `--set <id>=<v>` round-trips in sysfs; and
      an unknown id is refused by name. This is the generic path to the same
      attributes `bootsound` / `paneloverdrive` reach by name.

## 7. Profiles and custom profiles (daemon)

- [ ] `./voltaire profile --set performance` — `$PROFILE` reads `performance`,
      `ppt_*` hold performance's stock row (issue #12: voltaire writes it, the
      firmware does not).
- [ ] `./voltaire tdp --set 40` while on a firmware profile — creates and
      activates `custom`; `./voltaire profile --get` says `custom`, sysfs PPT
      reads 40.
- [ ] **Promotion starts fresh**: give `custom` a fan curve, return to
      `balanced`, then `./voltaire tdp --set 40` — `custom` now holds only the
      TDP: `$CURVE/pwm1_enable` stays `2` (fans on firmware auto, the stored
      curve did **not** come back), and it stays `2` through at least two
      reconcile ticks (~5 s). `./voltaire fancurve --get` shows no stored
      curve for it.
- [ ] `./voltaire profile --create gaming`, `./voltaire tdp --set 35 --profile
      gaming` — prints "stored… not applied", and sysfs PPT is **unchanged**
      (still 40).
- [ ] `./voltaire profile --set gaming` — PPT now 35. `./voltaire profile --set
      custom` — PPT back to 40 (A→B→A gives the same machine).
- [ ] `./voltaire profile --set balanced` — stock row restored, fans auto, CO
      cleared; `./voltaire profile --list` still shows both custom profiles.
- [ ] `./voltaire profile --create balanced` — refused (reserved name).
- [ ] `./voltaire profile --set no-such-profile` — refused, not forwarded to
      `platform_profile`.

## 8. Reconcile watcher (issue #15)

- [ ] `./voltaire profile --set custom` with a fan curve set (give `custom` one
      via `./voltaire fancurve --set …` if needed). Then
      `powerprofilesctl set power-saver`: `$CURVE/pwm1_enable` drops to `2` and
      returns to `1` within ~4 s without any voltaire command. Restore with
      `powerprofilesctl set balanced` (the watcher must **not** write
      `platform_profile` back — `$PROFILE` stays whatever PPD set).

## 9. Autoswitch

> Disable autoswitch (`./voltaire autoswitch --off`) before sections 12–13,
> and give the watcher one poll (~2 s) to see it. Otherwise the first tick
> after any daemon start latches `enabled=false`, the next tick reads an
> enable edge, and the one-shot re-applies the AC/battery target over
> whatever profile the section just set up — which silently invalidates the
> sleep/resume setup. See the note at the end of this file.

- [ ] `./voltaire autoswitch --on --ac balanced --battery gaming`, then unplug
      the charger — after the ~4 s settle window the gaming profile is applied
      (PPT reads 35). Replug — balanced comes back, custom profiles intact.
- [ ] A quick plug-bounce (unplug, replug within ~2 s) causes **no** profile
      churn.
- [ ] `./voltaire autoswitch --clear` when done.

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
- [ ] `systemctl --user stop voltaire.service voltaire.socket`, run
      `./voltaire daemon --no-button` in a terminal — button presses produce no
      events and the journal shows the watcher was never started. Ctrl-C, then
      `systemctl --user start voltaire.socket voltaire.service`.

## 11. Keyboard hotplug (aura-hid Reopen)

- [ ] With the daemon running and a visible effect applied: detach the cover,
      wait a few seconds, reattach — the keyboard's RGB effect comes back on
      its own within ~4 s (poll tick + udev chmod retry). Per-zone overrides
      survive: set the lightbar a different color first and confirm both zones
      restore to their own states.

## 12. Sleep / resume

Set up the full volatile state first: `./voltaire profile --set custom` with a
fan curve, a custom TDP (safe range, e.g. 40 W), and `./voltaire undervolt --set
-10`.

- [ ] Suspend (lid or `systemctl suspend`). While asleep: the fans spin down
      and stay down, and the lightbar is dark (the pre-sleep release +
      lightbar-off hook).
- [ ] Resume: lighting restored on both zones, `$CURVE/pwm1_enable` back to
      `1`, PPT back to the custom value, and the journal shows the CO
      reapply. The machine **stays** asleep until woken — a suspend that
      aborts within seconds is a wakeup-source problem; read
      `/sys/power/pm_wakeup_irq` before blaming the hook.
- [ ] High-TDP variant: `./voltaire tdp --set 80 --force`, suspend, resume — the
      journal shows power lowered *before* the fan release on the way down,
      and the floor curve re-applied with the limit on the way back.
- [ ] Cleanup: `./voltaire tdp --reset`.

## 13. The drawer's profile UI (voltaire-gui, 2.0)

Run the voltaire-gui build under test against the daemon under test. These are
the drawer paths that cannot run in the hermetic suite (the widget layer is
cgo); the rules behind them are unit tested in `internal/profileui`.

- [ ] The main view's PROFILE section is quiet/balanced/performance on one row
      plus a single Custom button, and the **whole main view fits without
      scrolling** (profile through brightness). The Custom button is labelled
      with the running custom profile's name, and highlighted, when one is
      active.
- [ ] The mouse wheel over any slider — battery, brightness, TDP, undervolt —
      scrolls the view and leaves the value alone (`batterylimit --get` and
      the PL readouts unchanged afterwards).
- [ ] Custom view: the selector dropdown names the profile being edited;
      opening it dims the view behind a scrim, picking a name re-targets the
      editor and closes the list, and the running profile carries a dot
      marker distinct from the selected highlight. Tapping the scrim,
      pressing Escape, and gamepad B each dismiss without selecting. Create a
      profile from the CLI (`./voltaire profile --create smoke-gui`) while
      the view is open — it appears the next time the list opens; with the
      list **open**, the view behind it does not change until it closes.
- [ ] Both autoswitch targets open as dropdowns the same way, by pointer,
      touch, and gamepad; picking a target sends once after the debounce.
- [ ] Selecting a profile does **not** activate it; Activate does. Activate is
      insensitive for the running profile and for one with no settings — and
      the reason appears as a note directly beneath the button row, readable
      by touch and on a controller (no hover anywhere).
- [ ] Editor on a profile that is **not** running shows the "Not active —
      changes are stored…" note, displays the profile's own stored values (not
      the live machine's), and Save TDP does not change `ppt_pl1_spl`.
      Activating the profile afterwards applies what was stored.
- [ ] Editor on the **running** profile shows no note and Save TDP moves
      sysfs, exactly as 1.x did.
- [ ] Fan floor in a stored edit follows the *profile's* TDP: store 80W in a
      non-running profile — its editor draws the floor line even while the
      machine sits at stock limits, and Reset Fans is refused/insensitive
      there.
- [ ] + New prefills a free name; OK on the prefill creates it (gamepad-only
      path). Save As is insensitive on a stock profile, works from a custom
      one.
- [ ] Delete Profile needs two taps, is insensitive for the active profile
      and for autoswitch targets (the note beneath it names the reason), and
      returns to the main view on success.
- [ ] Hints: hovering the theme button (or any hinted control) for ~half a
      second shows its help text anchored to it, on KDE **and** in Gaming
      Mode; gamepad focus shows the same text immediately; it never appears
      over an open dropdown.
- [ ] AUTOSWITCH section: switch + both targets mirror
      `./voltaire autoswitch --get`; picking a target and toggling the switch
      land in `autoswitch --get` after the debounce; targets offered exclude
      empty profiles and include "(don't change)".
- [ ] The header shows `AC · <temp> · <rpm>` on mains and `Battery · …`
      unplugged, updating on plug/unplug without reopening (power-source
      event); on a daemon without `source_known` (pre-2.0) it shows no power
      label at all.
- [ ] Gamepad: D-pad reaches the firmware buttons and Custom in the main view,
      and in the custom view the selector, Activate/New/Save As, OK/Cancel,
      and Delete; the autoswitch dropdowns are reachable while enabled and
      skipped while not. Section jump (L1/R1) includes "profile" and
      "autoswitch". Inside an open dropdown, D-pad walks the options, A
      picks, B dismisses — and a long list scrolls with focus. After closing
      a dropdown, focus is back on the drawer control that opened it (or its
      nearest visible neighbour if a selection just desensitized it).
- [ ] Gamepad cannot reach a control the pointer cannot use: with a custom
      profile **active**, its editor's Delete is skipped by D-pad navigation
      (focus wraps past it), as is an empty profile's activate button — the
      ✎ beside it stays reachable. `gtk_widget_activate()` ignores
      sensitivity, so this is a real path, not a theoretical one.
- [ ] Cleanup: `./voltaire profile --delete smoke-gui`.

:::note[Driving this checklist without a physical controller]
A uinput virtual gamepad plus an *absolute* pointer (the QEMU usb-tablet
shape: `ABS_X`/`ABS_Y` over 0–65535 with `BTN_LEFT` and no `BTN_TOUCH`, which
udev tags `ID_INPUT_MOUSE`) drives the whole of this section with screenshots
for verification. Two traps found the hard way: park the pointer away from
screen **corners**, because a hot corner takes focus and the layer-shell
backend then treats it as a genuine click-elsewhere and dismisses the drawer;
and keep it inside the drawer while screenshotting, since the backend
deliberately ignores focus loss with the pointer inside and dismisses with it
outside.
:::

### The full window (double press)

- [ ] Double-press the Armoury Crate button — the drawer flashes and the full
      window replaces it. The **Telemetry** tab shows all seven cards
      *immediately* (framed charts, "—" readouts at worst for the first
      second), never a blank grid.
- [ ] The expanded cards read sensibly: Temp shows CPU and GPU, Power shows
      Pkg/GPU/NPU (NPU 0.0 W while nothing uses it — a reading, not a gap),
      Load shows all three utilisations, Clocks in GHz, Memory RAM+VRAM in
      GB. Run something GPU-heavy and the GPU load/clock/power traces move
      together; `voltaire status` and the Temp card's CPU figure agree.
- [ ] Profiles tab: move only the TDP slider — the bottom bar reads
      "Unsaved: TDP" and **Apply Changes** enables. Apply: only the TDP
      changes (fans stay as they were); the bar clears and the button disables.
- [ ] Drag the fan curve, touch nothing else — bar reads "Unsaved: fan curve";
      Apply sends only the curve.
- [ ] Retarget the selector at a profile that is **not** running — the button
      reads **Save Changes** and an applied edit changes no sysfs.
- [ ] Toggling Advanced with no value moved does **not** enable the commit
      button; moving PL2 in advanced then unchecking Advanced sends basic
      semantics (the PL2 edit is not applied).
- [ ] The AUTOSWITCH card on the Profiles tab mirrors the drawer's section:
      flip it in one place, the other follows on the next sync; its dropdowns
      open inside the window, not on the hidden drawer.
- [ ] Reset TDP / Reset Fans sit right-aligned in their cards and still gate:
      Reset Fans is refused above the safe sustained limit with the note under
      the fan card.

## 14. Cleanup

- [ ] `./voltaire tdp --reset`, `./voltaire fancurve --reset` (if not already on
      a stock profile), `./voltaire undervolt --reset`.
- [ ] `./voltaire profile --delete gaming` (and any other test profiles),
      `./voltaire profile --set balanced`.
- [ ] `./voltaire batterylimit --set <your usual>`, boot sound / panel overdrive
      back to preference, lighting back to preference.
- [ ] `systemctl --user status voltaire.service` — still healthy, journal free
      of errors from the whole run.

## Known behaviour: the enable one-shot fires again after a daemon restart

Not a checklist item — a trap that invalidates sections 12–13 if you meet it
unaware, and a real (if narrow) defect in its own right.

`powerTick` latches `enabled=false` on the first observation after every
daemon start, regardless of what autoswitch is actually configured to
(`internal/daemon/powersource.go`, the `!st.known` branch). The next tick
therefore sees an enable edge and runs the enable-time one-shot. That is
deliberate: it catches an autoswitch configured in the two seconds before the
watcher started, and it normally costs nothing, because the one-shot skips
when the target is already the active profile — which it is whenever `Run()`
has just applied it.

The window is only harmful when the *profile changes* inside it. Select a
custom profile within roughly two to four seconds of the daemon starting, and
the one-shot reads target ≠ active and reverts you to the autoswitch target:

```
21:02:05  starting voltaire daemon
21:02:08  profile set=custom          <- deliberate
21:02:10  autoswitch source=AC profile=balanced reason="autoswitch enabled"
```

Reproduced against the released v1.3.1 logic as well (`git show
v1.3.1:internal/daemon/powersource.go`) — it predates the driver refactor and
the rename. The realistic user path is logging in and touching a profile
immediately, in the GUI or from a shell in a startup script.

Fixing it means distinguishing "autoswitch was enabled before we started"
from "autoswitch was just enabled", which the current latch cannot express:
persisting the enablement alongside the profile in the state file, or having
`Run()` seed `st.enabled` from the config it already read, would both do it.
Until then, disable autoswitch before any test that sets a profile right
after starting the daemon.
