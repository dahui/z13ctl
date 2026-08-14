---
title: GUI Quick Start
description: Open the drawer, learn the controls, and navigate with touch, mouse, or gamepad.
---

## Opening the drawer

Press the **Armoury Crate button** on your Z13. The drawer slides in from the
right edge of the screen.

Press it again, click anywhere outside the drawer, or press **Escape** to
close it.

## Opening the full window

Press the **Armoury Crate button twice** — a normal double tap — and the drawer
is replaced by a full window with room for the telemetry charts at a readable
size and the profile editor beside them. Close it with **Escape** or the
window's own close button; the drawer is unaffected and the next single press
opens it as usual.

The first press still opens the drawer immediately, so a single press costs
nothing extra: the drawer appears, and if a second press follows quickly it is
swapped for the window. Presses more than about 400 ms apart are two separate
toggles, not a double tap.

:::note[Steam Gaming Mode]
Under gamescope a double press opens the drawer's own telemetry view rather
than a separate window — gamescope does not composite a second application
window. The charts are the same ones.
:::

## Drawer controls

| Section | What it does |
|---------|-------------|
| **Profile** | The three firmware profiles (quiet, balanced, performance) and a **Custom** button. The Custom button is labelled with whichever custom profile is running, and opens the custom profile view. |
| **Autoswitch** | Turn it on, then pick a profile from each dropdown to apply on AC and on battery — or "(don't change)" to leave that side alone. The daemon applies them when the charger is plugged or unplugged. The two target rows appear only while autoswitch is enabled. |
| **Custom profile view** | Every custom profile lives here. The dropdown at the top names the one you are editing — open it and pick a name to switch to it; a dot marks the profile that is currently running. **Activate** applies it to the machine, **+ New** creates an empty named profile, **Save As** copies the active profile under a new name, and **Delete Profile** (tap twice) removes one that is not active or referenced by autoswitch. When a button is greyed out, the reason appears right beneath it. |
| **Live vs stored edits** | Editing the *active* profile applies changes to the hardware immediately. Editing any other profile stores them, to apply when it is activated — the view says "Not active — changes are stored" when that is what is happening. |
| **Custom TDP** | Configurable power limits with basic (single slider) and advanced (PL1 sustained / PL2 short boost / PL3 fast boost) modes |
| **Fan Curve** | Edit the fan response curve per-profile (profile editor, advanced mode) |
| **Undervolt** | CPU Curve Optimizer offset (profile editor, advanced mode; requires `ryzen_smu`). iGPU CO is not supported on Strix Halo. |
| **Telemetry** | Live APU temperature and fan RPM readouts (profile editor); the header also shows AC/Battery when the daemon can read the power source |
| **Telemetry charts** | The chart button at the bottom-left opens a history view: APU temperature and fan speed over the last 1, 5 or 15 minutes. See [Telemetry charts](#telemetry-charts). |
| **Battery Limit** | Set the charge cap (40–100%). Changes persist across reboots. |
| **Keyboard / Lightbar** | Tab between the two lighting zones |
| **Mode** | Lighting effect: static, breathe, cycle, rainbow, strobe, or off |
| **Color 1 / Color 2** | Pick from 8 presets or open the custom color picker |
| **Speed** | Animation speed for modes that support it: slow, normal, fast |
| **Brightness** | Lighting brightness: 0–3 |
| **Panel Overdrive** | Toggle faster pixel response (may cause slight ghosting) |
| **Boot Sound** | Enable or disable the startup POST sound |

Changes take effect immediately and are sent to the voltaire daemon. Settings
persist across reboots while the daemon is running.

The theme picker button at the bottom-left of the drawer opens the theme
view. See [Theming](/voltaire/gui/theming/) for details.

## Telemetry charts

The chart button in the bottom bar opens a history view. The daemon samples the
machine once a second and keeps the last five minutes, so the charts are drawn
from readings taken whether or not the drawer was open.

There is one chart per quantity the machine actually measures — on the Z13 that
is APU temperature and the two fan speeds. Both fans share one chart and one
scale, so you can compare them directly. A quantity your hardware does not
report gets no chart at all rather than a line sitting at zero, which would look
like a measurement.

The buttons above the charts pick how far back to look. Only spans the daemon
can fill are offered.

Two things worth knowing when reading a chart:

- **Gaps are real.** Time runs left to right, so a suspend leaves a break in the
  line rather than a straight ramp across the hours the machine was asleep. A
  reading the daemon could not take is left out for the same reason.
- **The scale is stable, not fitted.** It stays put while values move around
  inside the normal range and only grows when a reading falls outside it, so the
  shape of the trace means the same thing from one second to the next.

If the view says the daemon does not serve telemetry history, the daemon is
older than the GUI — restart it after upgrading:

```sh
systemctl --user restart voltaire
```

## Custom color picker

Click **Custom** under any color input to open the HSL color picker. Adjust
the Hue, Saturation, and Lightness sliders to dial in any color. The preview
swatch updates in real time.

## Gamepad navigation

voltaire-gui supports full gamepad control for use in Steam Gaming Mode:

| Input | Action |
|-------|--------|
| D-pad | Navigate between controls |
| A (Cross) | Activate buttons/switches, or enter edit mode for sliders |
| Left/Right (in edit mode) | Adjust a slider value |
| A (in edit mode) | Commit the value |
| B (Circle) | Cancel edit, go back, or close the drawer |
| L1/R1 (shoulder) | Jump between sections |

Gamepad focus (indicated by a highlight border) is automatically hidden when
the mouse moves. To disable gamepad input entirely, set
`VOLTAIRE_GUI_NO_GAMEPAD=1`.

## Gamescope (Steam Gaming Mode)

In Steam Gaming Mode, voltaire-gui runs as a gamescope X11 overlay. The
backend is selected automatically when `GAMESCOPE_WAYLAND_DISPLAY` is set and
its socket is present.

The UI scales automatically to match the output resolution. Use
`VOLTAIRE_GUI_SCALE` to override the auto-detected scale factor if the UI
appears too large or small.

Dropdowns, their lists, and help hints are drawn inside the drawer's own
surface, so they work identically in Gaming Mode — gamescope does not composite
a separate popup window as something you could see and click. The two large
pickers (theme, HSL color) are full views for the same reason.
