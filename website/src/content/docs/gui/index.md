---
title: The GUI — voltaire-gui
description: The GTK4 overlay drawer for Voltaire on Wayland — features, display backends, GNOME support, and troubleshooting.
---

`voltaire-gui` is a GTK4 overlay drawer for **Voltaire** on Wayland —
graphical controls for the 2025 ASUS ROG Flow Z13 on Linux. Press the Armoury
Crate button and it slides in from the right edge of the screen.

## What voltaire-gui does

- **Profile switching** — quiet, balanced and performance in the drawer, with
  every saved custom profile behind the Custom dropdown beside them. Named
  profiles can be created, copied (Save As), edited without activating them,
  and deleted from the full window's Profiles tab
- **Autoswitch** — pick a profile per power source and the daemon switches
  when the charger is plugged or unplugged; the header shows AC/Battery
- **Custom TDP control** — configurable power limits (PL1/PL2/PL3) on the full
  window's Profiles tab, with basic and advanced modes
- **Fan curve editor** — per-profile fan response curve editing (Profiles tab,
  advanced mode)
- **Undervolt** — CPU Curve Optimizer offset (requires `ryzen_smu` kernel
  module; iGPU CO is not supported on Strix Halo)
- **APU telemetry** — live temperature and fan RPM in the drawer's header, and
  charted over time on the full window's Dashboard
- **Battery charge limit** — set the charge cap (40–100%) from the drawer
- **RGB lighting** — mode, color, speed, and brightness for the keyboard
  backlight and edge lightbar
- **System toggles** — panel overdrive and boot sound on/off
- **Theme picker** — 15 built-in themes with full custom theme support
- **Gamepad navigation** — full D-pad + button control for Steam Gaming Mode

All hardware communication goes through the voltaire daemon. voltaire-gui
never touches HID devices or sysfs directly.

## Display backends

Three backends are supported, selected automatically based on the session
environment:

- **Layer-shell** (KDE Plasma, Hyprland, Sway) — Wayland layer-shell overlay
  with margin-based slide animation and focus-loss dismiss
- **Overlay** (GNOME, and any compositor without layer-shell) — a fullscreen,
  transparent window with the drawer against the right edge. Everything
  outside the drawer is click-through, so the rest of the desktop stays
  usable. See [GNOME support](#gnome-support)
- **Gamescope** (Steam Gaming Mode) — X11 overlay via the `STEAM_OVERLAY`
  atom with opacity-based visibility and a click-to-dismiss backdrop

## GNOME support

voltaire-gui works on GNOME, but with a slightly degraded experience compared
with KDE Plasma, Hyprland or Sway. This section covers why, and exactly what
differs.

### Why a workaround is needed

The drawer is normally anchored to the screen edge using the **layer-shell**
protocol (`zwlr_layer_shell_v1`). That protocol is a **wlroots extension** —
it is not part of `wayland-protocols`, and not every Wayland compositor
implements it:

| Compositor | Wayland | `zwlr_layer_shell_v1` |
|---|---|---|
| KWin (KDE Plasma) | yes | yes |
| Hyprland, Sway (wlroots) | yes | yes |
| **Mutter (GNOME)** | **yes** | **no** |

GNOME supports Wayland perfectly well. What Mutter has never implemented is
this one extension, on the long-standing position that panels and docks belong
in GNOME Shell extensions rather than in a client-side protocol.

:::caution[Installing `gtk4-layer-shell` does not help]
`gtk4-layer-shell` is the **client** side of the protocol — it is what
voltaire-gui uses to *speak* layer-shell, and the packages already depend on
it. The protocol itself has to come from the compositor, and Mutter offers no
way to add Wayland protocols (GNOME Shell extensions cannot register Wayland
globals). No package can add layer-shell to GNOME.
:::

### How the workaround works

Core Wayland deliberately gives a client no way to position its own window —
the compositor decides placement. So without layer-shell, an ordinary window
simply lands wherever Mutter puts it, which is the middle of the screen. That
is [exactly what used to happen](https://github.com/dahui/z13gui/issues/16).

Instead, voltaire-gui takes a **fullscreen** window — a standard request every
compositor honours, and one that needs no positioning — makes it fully
transparent, and draws the drawer against the right edge inside it. The
window's *input region* is then restricted to the drawer's rectangle, so
every pixel outside the drawer is click-through: clicks and scrolls pass
straight to the windows underneath, and the rest of the desktop keeps working
normally.

The missing protocol is detected at startup and this backend is selected
automatically. There is nothing to configure.

### What is unchanged

- The drawer sits against the right edge at full height, less 5% top and
  bottom
- The slide-in and slide-out animation
- Every control, theme and gamepad navigation behaviour
- Escape dismisses, as does clicking another window

### What is degraded

Because the drawer is an ordinary window rather than a compositor-managed
overlay layer:

- **It cannot be drawn above a fullscreen application.** Under layer-shell the
  drawer lives on the overlay layer, above everything; here it is a normal
  window and the compositor decides stacking.
- **It belongs to the current workspace**, rather than being present on every
  workspace the way a layer surface is.
- **It may appear in the window switcher (Alt-Tab) while open.** The window is
  unmapped when the drawer is closed, so it only shows up while on screen.
- **GNOME's top bar may be hidden while the drawer is open**, since Mutter
  hides it for fullscreen windows.
- **There is no dedicated click-outside backdrop.** Dismissal is Escape, or
  clicking another window — which works because that window takes focus.

None of this affects the controls themselves: every hardware feature behaves
identically on GNOME.

### Prefer the full experience?

Log into a session whose compositor implements layer-shell — KDE Plasma,
Hyprland and Sway are all packaged for Fedora and every other major
distribution. voltaire-gui switches back to the layer-shell backend
automatically.

## Requirements

- A Wayland compositor (layer-shell is used when available; GNOME and others
  get the overlay backend), or gamescope (Steam Gaming Mode)
- GTK 4 and gtk4-layer-shell libraries (see
  [Installation](/voltaire/installation/#runtime-dependencies-gui-only) for
  distro package names)
- The voltaire daemon running

## Troubleshooting

**Drawer doesn't appear**

Make sure the voltaire daemon is running:

```sh
systemctl --user status voltaire.service
```

Then check the log — see the note below on `--user`, which is required.

**Which display backend am I on?**

The startup log names it. Run voltaire-gui from a terminal and read the first
lines:

```sh
voltaire-gui --debug
```

Look for `backend mode=layer-shell`, `mode=overlay` or `mode=gamescope`. On
GNOME, `mode=overlay` is expected and correct: Mutter does not implement the
`zwlr_layer_shell_v1` protocol, so the drawer is drawn as a transparent
click-through overlay rather than an edge-anchored panel. See
[GNOME support](#gnome-support) for what that changes.

**The drawer is a small box in the middle of the screen**

This affects z13gui v1.3.0 and earlier on GNOME. Those versions called into
layer-shell without checking whether the compositor implements it; every
anchoring call silently did nothing, and since the anchors were the only thing
giving the drawer a height, it collapsed into a small unusable window
([#16](https://github.com/dahui/z13gui/issues/16)). Upgrade to the latest
release, which detects this and uses the overlay backend instead.

**Reading the log: `--user` is required**

voltaire-gui runs as a systemd **user** unit (installed to
`/usr/lib/systemd/user/` by the distro packages), so its output goes to the
user journal. Without `--user`, `journalctl` searches system units, finds no
such unit, and prints `-- No entries --` — which looks like the program never
ran:

```sh
journalctl --user -u voltaire-gui -n 50   # correct
sudo journalctl -u voltaire-gui           # WRONG: reads system units, prints nothing
```

**Service fails to start**

Check the journal:

```sh
journalctl --user -u voltaire-gui -n 50
```

Run with debug logging to see GTK and initialization output:

```sh
voltaire-gui --debug
```

**Touchscreen or touchpad stops responding while the drawer is open**

This was a bug in z13gui, fixed before the voltaire rename — no voltaire release
has it. On z13gui 1.4.0 and earlier, the drawer could mistake the machine's own
touchpad and touchscreen for a game controller's touchpad and take exclusive
access (`EVIOCGRAB`) to them for as long as it was open, which stopped touch
input reaching the desktop entirely. A stylus was unaffected, and pressing `Esc`
to dismiss the drawer gave the devices back.

Only users whose account can open those device nodes were affected — normally
that means membership of the `input` group, since stock udev rules grant the
session user access to joysticks but not to touch devices. Neither
`99-voltaire.rules` nor `99-voltaire-gamepad.rules` grants access to touch
devices.

If you are seeing this, you are running z13gui rather than voltaire-gui. Note
that `/usr/bin/z13gui` is a compatibility symlink to `voltaire-gui`, so the
command name alone does not tell you which binary is running — check
`voltaire-gui --version`.

**Gamescope: controller input not suppressed while drawer is open**

Grant BPF capabilities so voltaire-gui can block controller input at the
kernel level:

```sh
sudo setcap cap_bpf,cap_perfmon+ep /usr/local/bin/voltaire-gui
```

Without capabilities, voltaire-gui falls back to freezing Steam (SIGSTOP),
which also pauses running games. See
[Gamepad input blocking](/voltaire/installation/#gamepad-input-blocking-capabilities)
for what the capabilities do.

**Gamescope: drawer doesn't show**

Verify `GAMESCOPE_WAYLAND_DISPLAY` is set and the socket exists:

```sh
echo $GAMESCOPE_WAYLAND_DISPLAY
ls "$XDG_RUNTIME_DIR/$GAMESCOPE_WAYLAND_DISPLAY"
```

If the socket is missing (stale environment from a previous Gaming Mode
session), voltaire-gui automatically falls back to the Wayland path —
layer-shell where the compositor supports it, the overlay backend otherwise.

## Next steps

- [**Installation**](/voltaire/installation/) — packages or build from source
- [**Quick Start**](/voltaire/gui/quick-start/) — open the drawer and explore
  the controls
- [**Configuration**](/voltaire/gui/configuration/) — config file, environment
  variables
- [**Theming**](/voltaire/gui/theming/) — built-in themes and custom color
  definitions
