# z13gui

GTK4 overlay drawer for **z13ctl** on Wayland — graphical controls for the
2025 ASUS ROG Flow Z13 on Linux.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](https://github.com/dahui/z13gui/blob/main/LICENSE)

---

## What z13gui does

- **Profile switching** — quiet, balanced, performance, and custom TDP profiles
- **Custom TDP control** — configurable power limits (PL1/PL2/PL3) in the custom profile view, with basic and advanced modes
- **Fan curve editor** — per-profile fan response curve editing (custom profile, advanced mode)
- **Undervolt** — CPU Curve Optimizer offset (requires `ryzen_smu` kernel module; iGPU CO is not supported on Strix Halo)
- **APU telemetry** — live temperature and fan RPM readouts in the custom profile view
- **Battery charge limit** — set the charge cap (40–100%) from the drawer
- **RGB lighting** — mode, color, speed, and brightness for the keyboard
  backlight and edge lightbar
- **System toggles** — panel overdrive and boot sound on/off
- **Theme picker** — 15 built-in themes with full custom theme support
- **Gamepad navigation** — full D-pad + button control for Steam Gaming Mode

All hardware communication goes through the z13ctl daemon. z13gui never
touches HID devices or sysfs directly.

---

## Display backends

Three backends are supported, selected automatically based on the session
environment:

- **Layer-shell** (KDE Plasma, Hyprland, Sway) — Wayland layer-shell overlay
  with margin-based slide animation and focus-loss dismiss
- **Overlay** (GNOME, and any compositor without layer-shell) — a fullscreen,
  transparent window with the drawer against the right edge. Everything outside
  the drawer is click-through, so the rest of the desktop stays usable.
  See [GNOME support](#gnome-support)
- **Gamescope** (Steam Gaming Mode) — X11 overlay via the `STEAM_OVERLAY` atom
  with opacity-based visibility and a click-to-dismiss backdrop

---

## GNOME support

z13gui works on GNOME, but with a slightly degraded experience compared with KDE
Plasma, Hyprland or Sway. This section covers why, and exactly what differs.

### Why a workaround is needed

The drawer is normally anchored to the screen edge using the **layer-shell**
protocol (`zwlr_layer_shell_v1`). That protocol is a **wlroots extension** — it
is not part of `wayland-protocols`, and not every Wayland compositor implements
it:

| Compositor | Wayland | `zwlr_layer_shell_v1` |
|---|---|---|
| KWin (KDE Plasma) | yes | yes |
| Hyprland, Sway (wlroots) | yes | yes |
| **Mutter (GNOME)** | **yes** | **no** |

GNOME supports Wayland perfectly well. What Mutter has never implemented is this
one extension, on the long-standing position that panels and docks belong in
GNOME Shell extensions rather than in a client-side protocol.

!!! warning "Installing `gtk4-layer-shell` does not help"

    `gtk4-layer-shell` is the **client** side of the protocol — it is what z13gui
    uses to *speak* layer-shell, and the packages already depend on it. The
    protocol itself has to come from the compositor, and Mutter offers no way to
    add Wayland protocols (GNOME Shell extensions cannot register Wayland
    globals). No package can add layer-shell to GNOME.

### How the workaround works

Core Wayland deliberately gives a client no way to position its own window — the
compositor decides placement. So without layer-shell, an ordinary window simply
lands wherever Mutter puts it, which is the middle of the screen. That is
[exactly what used to happen](https://github.com/dahui/z13gui/issues/16).

Instead, z13gui takes a **fullscreen** window — a standard request every
compositor honours, and one that needs no positioning — makes it fully
transparent, and draws the drawer against the right edge inside it. The window's
*input region* is then restricted to the drawer's rectangle, so every pixel
outside the drawer is click-through: clicks and scrolls pass straight to the
windows underneath, and the rest of the desktop keeps working normally.

The missing protocol is detected at startup and this backend is selected
automatically. There is nothing to configure.

### What is unchanged

- The drawer sits against the right edge at full height, less 5% top and bottom
- The slide-in and slide-out animation
- Every control, theme and gamepad navigation behaviour
- Escape dismisses, as does clicking another window

### What is degraded

Because the drawer is an ordinary window rather than a compositor-managed overlay
layer:

- **It cannot be drawn above a fullscreen application.** Under layer-shell the
  drawer lives on the overlay layer, above everything; here it is a normal window
  and the compositor decides stacking.
- **It belongs to the current workspace**, rather than being present on every
  workspace the way a layer surface is.
- **It may appear in the window switcher (Alt-Tab) while open.** The window is
  unmapped when the drawer is closed, so it only shows up while on screen.
- **GNOME's top bar may be hidden while the drawer is open**, since Mutter hides
  it for fullscreen windows.
- **There is no dedicated click-outside backdrop.** Dismissal is Escape, or
  clicking another window — which works because that window takes focus.

None of this affects the controls themselves: every hardware feature behaves
identically on GNOME.

### Prefer the full experience?

Log into a session whose compositor implements layer-shell — KDE Plasma, Hyprland
and Sway are all packaged for Fedora and every other major distribution. z13gui
switches back to the layer-shell backend automatically.

---

## Requirements

- A Wayland compositor (layer-shell is used when available; GNOME and others get
  the overlay backend), or gamescope (Steam Gaming Mode)
- GTK 4 and gtk4-layer-shell libraries (see [Installation](installation.md#runtime-dependencies) for distro package names)
- [z13ctl](https://github.com/dahui/z13ctl) daemon running

---

## Next steps

- [**Installation**](installation.md) — download the binary or build from source
- [**Quick Start**](getting-started.md) — open the drawer and explore the controls
- [**Configuration**](configuration.md) — config file, environment variables
- [**Theming**](theming.md) — built-in themes and custom color definitions
