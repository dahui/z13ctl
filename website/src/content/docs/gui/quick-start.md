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
is replaced by a full window: a desktop-style window with a **Dashboard** tab
(charts and the everyday controls), a **Profiles** tab (the profile editor) and
a **Settings** tab (the firmware switches). Switch pages with the tabs under the
titlebar. Close it with **Escape**, the window's close
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
| **Autoswitch** | Turn it on, then pick a profile from each dropdown to apply on AC and on battery — or "(don't change)" to leave that side alone. The daemon applies them when the charger is plugged or unplugged. The two target rows appear only while autoswitch is enabled. A custom profile with no settings shows greyed out as "(empty)" — give it something to apply and it becomes selectable. Also on the full window's Dashboard. |
| **Custom profile view** | Every custom profile lives here. The dropdown at the top names the one you are editing — open it and pick a name to switch to it; a dot marks the profile that is currently running. **Activate** applies it to the machine, **+ New** creates an empty named profile, **Save As** copies the active profile under a new name, and **Delete Profile** (tap twice) removes one that is not active or referenced by autoswitch. When a button is greyed out, the reason appears right beneath it. |
| **Live vs stored edits** | Editing the *active* profile applies changes to the hardware immediately. Editing any other profile stores them, to apply when it is activated — the view says "Not active — changes are stored" when that is what is happening. |
| **Custom TDP** | Configurable power limits with basic (single slider) and advanced (PL1 sustained / PL2 short boost / PL3 fast boost) modes |
| **Fan Curve** | Edit the fan response curve per-profile (profile editor, advanced mode) |
| **Undervolt** | CPU Curve Optimizer offset (profile editor, advanced mode; requires `ryzen_smu`). iGPU CO is not supported on Strix Halo. |
| **Telemetry** | Live APU temperature and fan RPM readouts (profile editor); the header also shows AC/Battery when the daemon can read the power source. The full history charts live on the full window's Dashboard tab — see [The Dashboard tab](#the-dashboard-tab). |
| **Battery Limit** | Set the charge cap (40–100%). Changes persist across reboots. |
| **Keyboard / Lightbar** | Tab between the two lighting zones (the full window shows both at once instead) |
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

## The Dashboard tab

The full window's **Dashboard** tab is the page to leave open. It reads in
three bands, each under its own heading: **Telemetry** — what the machine is
doing — then **System** — what it is set to — then **RGB** — what it looks
like. The daemon samples the machine once a second and keeps the
last five minutes, so the charts are drawn from readings taken whether or not
anything was open.

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

### The controls under the charts

### System

Beneath the charts are the settings that change what the machine is doing right
now — as opposed to the Profiles tab, which edits what a saved profile *says*.

**Power**

| Setting | What it does |
|---------|-------------|
| **Profile** | The three firmware profiles. **Custom…** opens the Profiles tab — the trailing dots mean it goes somewhere rather than selecting a fourth profile; its label is whichever custom profile is running. |
| **Charge limit** | The charge cap, 40–100%, with the current value beside the slider. |
| **CPU boost** | Turns the CPU's boost clocks on or off. Off caps every core at its base clock — 3.0 GHz instead of 5.19 GHz on the Z13 — for less peak power and heat. The kernel re-enables boost at every boot, so voltaire remembers your choice and restores it. |
| **Autoswitch** | Turn it on and the two indented rows appear: a profile to apply on AC, and one on battery. |

**Display**

| Setting | What it does |
|---------|-------------|
| **Refresh rate** | The rates your screen offers at the resolution it is running (see below). |
| **Autoswitch** | Off by default. Turn it on and the two indented rows appear: the rate to use on AC, and the rate to use on battery. |

### RGB

The keyboard and the lightbar get **a card each**, side by side, with the same
controls in both. The drawer switches between the two zones with a pair of tabs
because it is 320px wide; the window has room to show them together, so you can
see and change both at once.

| Setting | What it does |
|---------|-------------|
| **Effect** | Static, breathe, cycle, rainbow, strobe, off. |
| **Colour 1 / 2** | Eight presets, the current colour, and **Custom…** for the full picker. The second colour appears only for effects that use one. |
| **Speed / Brightness** | Shown for the effects that have them. Brightness is named rather than numbered — Off, Low, Medium, High. |

A card only shows the settings its effect uses, so the two are often different
heights — a zone on *cycle* has no colours to set. That is deliberate: the page
tells you at a glance what each zone is doing.

Everything here is a second copy of a control the drawer also has, not a
different one: change the profile in either place and both follow within a
second. They look different because they are laid out for a window — one
setting per line with its name in a column — where the drawer stacks each
control under its own heading for a thumb.

### Refresh rate

The refresh-rate dropdown offers the rates your screen supports **at the
resolution it is currently running**. It deliberately does not change the
resolution — that belongs in your desktop's own display settings, which ask you
to confirm that the new mode works before keeping it.

Two details worth knowing:

- Panels often report a rate like 59.87 for what everything else calls 60, so
  the list shows rounded labels. Two modes that round to the same number appear
  once.
- The control appears only where voltaire can read the display configuration.
  Today that means a **KDE Plasma** session (it uses `kscreen-doctor`); on other
  desktops, and in Steam Gaming Mode, the card is simply not shown rather than
  offering something that would fail.

If you change the rate and the compositor refuses or substitutes a mode, the
dropdown re-reads afterwards and shows what the screen is actually running — not
what was asked for.

#### Dropping the rate on battery

A 180 Hz panel costs real battery life at 180 Hz, so **Autoswitch** under the
rate dropdown changes it with the charger. It works exactly like the profile
autoswitch in the Power card beside it: one switch, and two indented rows saying
what to use on each power source.

Turning it on fills those rows in for you — **On AC** takes the rate you are
already running, so nothing changes under you, and **On battery** takes the
lowest rate your screen offers, which is the whole point of the feature. Change
either one from its dropdown. Turning the switch off keeps both, so switching
back on picks up where you left off.

Some things worth knowing:

- **Nothing happens when you pick a rate.** These rows say what should happen
  when the power source *changes*; the **Refresh rate** row above is how you
  change the rate now. Same as the autoswitch profile targets.
- **The rate is stored, not the display mode.** Set *60 Hz* and voltaire looks
  for a 60 Hz mode at the moment it needs one. Dock a screen that has no 60 Hz
  mode and it leaves that screen alone rather than picking something near it —
  the card says so too.
- **Nothing changes at login.** Your desktop already remembers the rate it was
  running; voltaire only acts on a change of power source while it is running,
  so a rate you set by hand survives a restart.
- **It needs the daemon**, because the daemon is what reports the power source.
  It does not need the window to be open.

It lives in `~/.config/voltaire/config.toml` as `refresh_autoswitch`,
`refresh_ac` and `refresh_battery` — the last two in whole hertz.

## The Profiles tab

The full window's **Profiles** tab is the same profile editor as the drawer's
custom view, laid out for a desktop: the profile operations (Activate, + New,
Save As, Delete Profile) in one card, the power limits and undervolt in a
**POWER** card, and the fan curve editor beside them.

Autoswitch is *not* here: it picks which profile the machine runs, which is a
live setting rather than part of a profile's contents, so it lives on the
Dashboard with the other live controls.

Instead of the drawer's per-domain save buttons there is **one commit button**
in the bar along the bottom. Move any slider or drag the curve and the bar
shows what is unsaved; the button sends exactly those changes — **Apply
Changes** when the target profile is running (applied to hardware
immediately), **Save Changes** when it is not (stored, applied on
activation). Each card keeps its own *reset*, which is a different operation:
it removes that subsystem from the profile and hands the hardware back to the
firmware.

## The Settings tab

The full window's **Settings** tab has two cards: **Voltaire**, for the app's
own preferences, and **Firmware**, for the switches your machine exposes.

### Which surface the button opens

**Armoury Crate button** decides what a single press of the hardware button
raises — the **Quickbar** (the default) or the **Full window**. A double press
opens the other one, whichever way round you set it, so both surfaces stay one
gesture away.

The change takes effect at once; there is nothing to restart and nothing to
save. It is stored in `~/.config/voltaire/config.toml` as `button_press`, beside
your theme.

### Firmware switches

The Firmware card lists the switches your machine exposes — on the Z13, the POST boot sound and panel overdrive — each with the
description that comes from voltaire's own device data, so a warning like "may
cause ghosting" is attached to the setting rather than left to be remembered.

The list is not written into the app: it is whatever your device reports, so a
machine with a different set of BIOS toggles shows that set instead, and one
with none says so. A switch voltaire cannot read the current value of is shown
greyed rather than defaulting to off — off would be a claim about your hardware.

Changes apply immediately and are BIOS settings, so they persist across reboots
on their own; the daemon does not need to restore them. This card is the only
place in the GUI that offers them — the drawer used to carry the same two
switches in its bottom bar and no longer does. Change one here or with the CLI
and every open surface follows within a second.

## Custom color picker

Click **Custom** under any color input to open the HSL color picker. It opens
as a panel anchored to the button, over whichever surface you are on — the
drawer or the full window — so you never leave the page you were working on.

Each slider is painted with what it will do: Hue runs through the spectrum at
your current saturation and lightness, Saturation from grey to full colour, and
Lightness from black through the colour to white. The bar along the bottom and
the hex readout show the result, and the lighting updates live as you drag.

The eight presets are repeated inside the picker so you can jump to one without
closing it. Click anywhere outside, press `Escape`, or press **B** on a
controller to dismiss it.

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

Dropdowns, their lists, the HSL color picker and help hints are all drawn
inside the drawer's own surface, so they work identically in Gaming Mode —
gamescope does not composite a separate popup window as something you could see
and click. The theme picker is a full view for the same reason.
