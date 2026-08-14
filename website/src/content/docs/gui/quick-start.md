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
is replaced by a full window: a desktop-style window with a **Telemetry** tab
(the dashboard), a **Profiles** tab (the profile editor) and a **Settings** tab
(the firmware switches). Switch pages with the tabs under the titlebar. Close it with **Escape**, the window's close
button, or another press of the Armoury Crate button; the drawer is unaffected
and the next single press opens it as usual.

The first press still opens the drawer immediately, so a single press costs
nothing extra: the drawer appears, and if a second press follows quickly it is
swapped for the window. Presses more than about 400 ms apart are two separate
toggles, not a double tap.

:::note[Steam Gaming Mode]
Under gamescope the full window opens inside voltaire's own fullscreen surface
rather than as a separate window — gamescope does not composite a second
application window. The pages, tabs, and controls are the same; L1/R1 switch
tabs and B closes it.
:::

## Drawer controls

| Section | What it does |
|---------|-------------|
| **Profile** | The three firmware profiles (quiet, balanced, performance) and a **Custom** button. The Custom button is labelled with whichever custom profile is running, and opens the custom profile view. |
| **Autoswitch** | Turn it on, then pick a profile from each dropdown to apply on AC and on battery — or "(don't change)" to leave that side alone. The daemon applies them when the charger is plugged or unplugged. The two target rows appear only while autoswitch is enabled. A custom profile with no settings shows greyed out as "(empty)" — give it something to apply and it becomes selectable. |
| **Custom profile view** | Every custom profile lives here. The dropdown at the top names the one you are editing — open it and pick a name to switch to it; a dot marks the profile that is currently running. **Activate** applies it to the machine, **+ New** creates an empty named profile, **Save As** copies the active profile under a new name, and **Delete Profile** (tap twice) removes one that is not active or referenced by autoswitch. When a button is greyed out, the reason appears right beneath it. |
| **Live vs stored edits** | Editing the *active* profile applies changes to the hardware immediately. Editing any other profile stores them, to apply when it is activated — the view says "Not active — changes are stored" when that is what is happening. |
| **Custom TDP** | Configurable power limits with basic (single slider) and advanced (PL1 sustained / PL2 short boost / PL3 fast boost) modes |
| **Fan Curve** | Edit the fan response curve per-profile (profile editor, advanced mode) |
| **Undervolt** | CPU Curve Optimizer offset (profile editor, advanced mode; requires `ryzen_smu`). iGPU CO is not supported on Strix Halo. |
| **Telemetry** | Live APU temperature and fan RPM readouts (profile editor); the header also shows AC/Battery when the daemon can read the power source. The full history charts live on the full window's Telemetry tab — see [Telemetry dashboard](#telemetry-dashboard). |
| **Battery Limit** | Set the charge cap (40–100%). Changes persist across reboots. |
| **Keyboard / Lightbar** | Tab between the two lighting zones |
| **Mode** | Lighting effect: static, breathe, cycle, rainbow, strobe, or off |
| **Color 1 / Color 2** | Pick from 8 presets or open the custom color picker |
| **Speed** | Animation speed for modes that support it: slow, normal, fast |
| **Brightness** | Lighting brightness: 0–3 |

Changes take effect immediately and are sent to the voltaire daemon. Settings
persist across reboots while the daemon is running.

The firmware switches — panel overdrive and the POST boot sound — live on the
full window's [Settings tab](#the-settings-tab) rather than in the drawer. They
are BIOS settings most people set once, and the drawer is for the controls you
reach for in a hurry.

The theme picker button at the bottom-left of the drawer opens the theme
view. See [Theming](/voltaire/gui/theming/) for details.

## Telemetry dashboard

The full window's **Telemetry** tab is a dashboard: a grid of labelled cards,
one chart per quantity, each with the live reading in its header. The daemon
samples the machine once a second and keeps the last five minutes, so the
charts are drawn from readings taken whether or not anything was open.

There is one card per *kind* of quantity the machine actually measures — on
the Z13 that is eight:

- **Temp** — CPU die and GPU edge temperature, one shared scale
- **Fan** — both fan speeds
- **Power** — CPU package, GPU (GFX domain), and NPU draw in watts
- **Battery** — state of charge (see below)
- **Load** — CPU, GPU, and NPU utilisation
- **Clocks** — average CPU core clock, GPU clock, and memory clock, in GHz
- **Memory** — unified memory in use, and the iGPU's VRAM carveout, in GB
- **Net** — network throughput, down and up, in MB/s

Series on one card share one scale, so their heights compare directly. A
quantity your hardware does not report gets no card at all rather than a line
sitting at zero, which would look like a measurement.

The **NPU** reads 0 W and 0% whenever it is suspended — which is most of the
time on a machine not running local inference, and is a true reading, not a
gap. voltaire deliberately never wakes it to ask: the reading is taken only
while something else already has the NPU powered up.

The **battery card's header names what the pack is doing**. While energy is
actually moving it shows the charge level, the rate in watts, and a time
estimate — `64% · 28.0 W · 30 m to limit` while charging (the estimate targets
your charge limit when one is set, since the pack genuinely stops there; "to
full" otherwise), `81% · 12.3 W · 3 h 5 m to empty` on battery. The estimate
is remaining energy over the current rate, so it moves with the load exactly
as every battery indicator does; at rates too low to divide by honestly it is
dropped and the plain Charging/Discharging word returns. At rest the header
reads *AC · not charging* — the normal state on a machine with a charge limit:
a pack resting above its threshold on mains moves no energy, and the header
says why.

**Package power** is read from the kernel's RAPL energy counter. That file is
root-only by default (a side-channel mitigation), so the chart appears only
after `sudo voltaire setup` has granted read access — it is a **read-only**
grant, since the same directory holds the CPU's power caps. If you upgraded
from a version before this existed, re-run setup once.

**The battery chart plots state of charge** on a fixed 0–100% scale, so a day
of use reads as one slowly falling and rising line and a suspend leaves a
visible gap in it. The flow in watts lives in the card's header rather than on
the chart — watts and percent cannot share an axis. A machine with no battery
gets no chart at all.

**Network throughput** counts the machine's physical interfaces — Wi-Fi, a
docked ethernet port — and deliberately not tunnels or bridges, whose traffic
also crosses the hardware beneath them and would be counted twice. An idle
link's 0.0 MB/s is a real reading and is drawn as one.

The full card set appears the moment the tab opens, with each chart framed and
"—" beside its name, and the data fills in as it arrives — usually within a
second.

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

## The Profiles tab

The full window's **Profiles** tab is the same profile editor as the drawer's
custom view, laid out for a desktop: the profile operations (Activate, + New,
Save As, Delete Profile) in one card, the power limits and undervolt in a
**POWER** card, the fan curve editor beside them, and the **autoswitch**
controls beneath it — the same controls as the drawer's Autoswitch section,
here because autoswitch picks between the profiles this page manages.

Instead of the drawer's per-domain save buttons there is **one commit button**
in the bar along the bottom. Move any slider or drag the curve and the bar
shows what is unsaved; the button sends exactly those changes — **Apply
Changes** when the target profile is running (applied to hardware
immediately), **Save Changes** when it is not (stored, applied on
activation). Each card keeps its own *reset*, which is a different operation:
it removes that subsystem from the profile and hands the hardware back to the
firmware.

## The Settings tab

The full window's **Settings** tab lists the firmware switches your machine
exposes — on the Z13, the POST boot sound and panel overdrive — each with the
description that comes from voltaire's own device data, so a warning like "may
cause ghosting" is attached to the setting rather than left to be remembered.

The list is not written into the app: it is whatever your device reports, so a
machine with a different set of BIOS toggles shows that set instead, and one
with none says so. A switch voltaire cannot read the current value of is shown
greyed rather than defaulting to off — off would be a claim about your hardware.

Changes apply immediately and are BIOS settings, so they persist across reboots
on their own; the daemon does not need to restore them. This page is the only
place in the GUI that offers them — the drawer used to carry the same two
switches in its bottom bar and no longer does. Change one here or with the CLI
and every open surface follows within a second.

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
| B (Circle) | Cancel edit, close an open dropdown, close the full window, go back, or close the drawer — in that order |
| L1/R1 (shoulder) | Jump between sections; switch tabs while the full window is open |

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
