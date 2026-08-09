# Quick Start

## Opening the drawer

Press the **Armoury Crate button** on your Z13. The drawer slides in from the
right edge of the screen.

Press it again, click anywhere outside the drawer, or press **Escape** to close it.

---

## Drawer controls

| Section | What it does |
|---------|-------------|
| **Profile** | Switch between quiet, balanced, performance, and custom TDP profiles. Selecting custom opens a dedicated view. |
| **Custom TDP** | Configurable power limits with basic (single slider) and advanced (PL1 sustained / PL2 short boost / PL3 fast boost) modes |
| **Fan Curve** | Edit the fan response curve per-profile (custom profile, advanced mode) |
| **Undervolt** | CPU Curve Optimizer offset (custom profile, advanced mode; requires `ryzen_smu`). iGPU CO is not supported on Strix Halo. |
| **Telemetry** | Live APU temperature and fan RPM readouts (custom profile view) |
| **Battery Limit** | Set the charge cap (40–100%). Changes persist across reboots. |
| **Keyboard / Lightbar** | Tab between the two lighting zones |
| **Mode** | Lighting effect: static, breathe, cycle, rainbow, strobe, or off |
| **Color 1 / Color 2** | Pick from 8 presets or open the custom color picker |
| **Speed** | Animation speed for modes that support it: slow, normal, fast |
| **Brightness** | Lighting brightness: 0–3 |
| **Panel Overdrive** | Toggle faster pixel response (may cause slight ghosting) |
| **Boot Sound** | Enable or disable the startup POST sound |

Changes take effect immediately and are sent to the z13ctl daemon. Settings
persist across reboots while the daemon is running.

The theme picker button at the bottom-left of the drawer opens the theme view.
See [Theming](theming.md) for details.

---

## Custom color picker

Click **Custom** under any color input to open the HSL color picker. Adjust
the Hue, Saturation, and Lightness sliders to dial in any color. The preview
swatch updates in real time.

---

## Gamepad navigation

z13gui supports full gamepad control for use in Steam Gaming Mode:

| Input | Action |
|-------|--------|
| D-pad | Navigate between controls |
| A (Cross) | Activate buttons/switches, or enter edit mode for sliders |
| Left/Right (in edit mode) | Adjust a slider value |
| A (in edit mode) | Commit the value |
| B (Circle) | Cancel edit, go back, or close the drawer |
| L1/R1 (shoulder) | Jump between sections |

Gamepad focus (indicated by a highlight border) is automatically hidden when
the mouse moves. To disable gamepad input entirely, set `Z13GUI_NO_GAMEPAD=1`.

---

## Gamescope (Steam Gaming Mode)

In Steam Gaming Mode, z13gui runs as a gamescope X11 overlay. The backend is
selected automatically when `GAMESCOPE_WAYLAND_DISPLAY` is set and its socket
is present.

The UI scales automatically to match the output resolution. Use `Z13GUI_SCALE`
to override the auto-detected scale factor if the UI appears too large or small.

Popups and dropdowns are replaced with full-view alternatives (theme picker,
HSL color picker) because gamescope does not composite separate popup windows.
