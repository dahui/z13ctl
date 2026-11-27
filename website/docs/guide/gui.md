---
title: The GUI
description: voltaire-gui's drawer and full window, the controls on each, and how to drive them with touch, mouse or a controller.
---

# The GUI

`voltaire-gui` is voltaire's graphical interface. It has two surfaces:

- **The drawer** (also called the quickbar): a narrow panel that slides in from
  the edge of the screen with the controls you reach for in a hurry.
- **The full window**: a larger window with telemetry charts, the profile
  editor and the settings.

Both work on the desktop and in Steam Gaming Mode, and with touch, a mouse or
a controller. The GUI is a client of the voltaire daemon: every change goes
through the daemon, so the drawer, the full window and the `voltaire` command
always show the same values. If the daemon is not running, the GUI has nothing
to show; see [Troubleshooting](/troubleshooting#the-daemon-is-not-running).

Which controls appear depends on your device: a control your hardware does not
have is left out. [Supported devices](/devices/) lists what each machine
supports.

## The drawer {#drawer}

Press your device's hardware button (the Armoury Crate button on the ASUS
machines) and the drawer slides in from the right. Press it again, press
**Escape**, or click outside the drawer to close it. You can choose which
button opens voltaire, or none, in
[Buttons and controllers](/guide/buttons-and-controllers#opening-voltaire).
With no button, open voltaire from your desktop's application menu.

The header shows the power source, the CPU temperature and the fan speed. At
the bottom are two fixed buttons: the **theme** button (see
[Themes](/guide/themes)) and the **expand** button, which opens the full
window.

### Drawer sections

The drawer shows a set of sections under group headings (TDP AND POWER,
DISPLAY, SOUND, CONTROLLER, RGB). You can show, hide and reorder them in
**Settings → Quickbar**.

| Section | What it does | Default |
|---------|--------------|---------|
| **TDP** | One power slider, on a device whose TDP is a single setting | on |
| **Profile** | Switch between your device's firmware profiles and your saved custom profiles. On a single-slider device this is a **Saved profile** picker instead | on |
| **Autoswitch** | Pick a profile to apply on AC and one on battery | on |
| **Charge limit** | The battery charge limit slider | on |
| **Full charge once** | Charge to 100% once, then go back to the limit | off |
| **CPU boost** | Turn the CPU's boost clocks on or off | off |
| **Undervolt** | The undervolt offset, on a single-slider device | off |
| **Screen brightness** | The screen's brightness | off |
| **Refresh rate** | The screen's refresh rate (KDE Plasma only) | off |
| **Volume** | The sound volume | off |
| **Controller vibration** | The built-in controller's vibration strength | off |
| **RGB lighting** | Lighting zones, effect, colors, speed and brightness | on |

These defaults vary by device: the OneXPlayer X2 Mini Pro and the ROG Xbox
Ally X ship their own drawer layouts, with brightness, volume and other
handheld-friendly sections switched on.
[Customizing the GUI](/guide/gui-configuration#drawer-sections) lists each
device's default and how to change it.

Changes take effect immediately. The drawer switches profiles but does not
edit them: power limits, the fan curve and the undervolt of a custom profile
are on the full window's Profiles tab.

## The full window {#full-window}

Press the hardware button **twice** (a quick double press), or tap the
drawer's **expand** button, and the full window opens in place of the drawer.
You can swap which surface a single press opens in Settings.

The window has three tabs: **Dashboard**, **Profiles** and **Settings**. Its
title row has minimize, maximize and close buttons; drag it to move the
window. Close it with **Escape**, the close button, or another press of the
hardware button.

### Dashboard

The Dashboard is the page to leave open. At the top are the telemetry cards:
temperatures, fans, power, battery, load, clocks, memory and network, each with
a history chart. [Monitoring](/guide/monitoring) explains the cards.

Beneath them are the live controls, the settings that change what the machine
is doing right now:

- **Power**: the profile, the TDP slider and undervolt on a single-slider
  device, the **Charge limit** with its **Full charge** switch, **CPU boost**
  and **Autoswitch**.
- **Display**: brightness, the [refresh rate](#refresh-rate) and its
  autoswitch.
- **Sound**: the volume.
- **RGB**: one card per lighting zone, so you see and change every zone at
  once.

See [Profiles and power](/guide/profiles-and-power) and
[Battery](/guide/battery) for what each control does, and
[Lighting](/guide/lighting) for the RGB card.

### Profiles

The Profiles tab is the custom profile editor. Pick the profile to edit from
the list at the top; **Activate**, **+ New**, **Save As** and **Delete
Profile** manage your profiles.

- **Power**: the TDP, as one slider or, in advanced mode, separate sustained
  and boost limits. Advanced mode also shows the **undervolt** where your
  device supports it.
- **Fan curve**: drag the points on the chart. A dot marks the point the
  curve is commanding at the current temperature, and the preset buttons load
  a ready-made curve into the editor.

Editing the active profile changes the hardware when you press **Apply
Changes**; editing another profile stores the change (**Save Changes**) until
you activate it. Each card has its own reset (**Reset TDP**, **Reset Fans**,
**Reset UV**), which removes that setting from the profile and hands it back
to the firmware; **Reset All** removes all of them at once. See
[Profiles and power](/guide/profiles-and-power#custom-profiles) and
[Fans](/guide/fans).

### Settings

The Settings tab has up to four cards, depending on your device:

- **VOLTAIRE**: **Open voltaire with** chooses which of your device's buttons
  opens voltaire, or none. The next row chooses what a single press opens,
  **Quickbar** or **Full window**; a double press opens the other.
  **Hints on controller focus** turns the controller hints on or off (see
  [Gamepad navigation](#gamepad)).
- **QUICKBAR**: which sections the drawer shows and in what order. See
  [Customizing the GUI](/guide/gui-configuration#drawer-sections).
- **CONTROLLER**: the vibration strength, on a device with a built-in
  controller whose vibration voltaire can set. See
  [Buttons and controllers](/guide/buttons-and-controllers#vibration).
- **FIRMWARE**: the firmware switches your device offers (such as the boot
  sound), each with a short description. They are BIOS settings and persist on
  their own. If one only takes effect after a restart, a note at the top of the
  card says so.

### When a control says it is unavailable

Sometimes a control your device has cannot be used right now. Instead of
leaving a gap, the Dashboard shows a note in its place saying why and what to
do. For example:

- The TDP needs a kernel module that is not installed (the OneXPlayer X2 Mini
  Pro without `ryzen_smu`).
- InputPlumber hides the ROG Xbox Ally X's joystick lighting; see
  [Buttons and controllers](/guide/buttons-and-controllers#ally-lighting).
- In Steam Gaming Mode, Steam owns a setting such as the Ally's power limits;
  see [Steam Gaming Mode](/guide/gaming-mode#steam-controls).

## Gamepad navigation {#gamepad}

The drawer and the full window can be driven entirely with a controller.

| Input | Action |
|-------|--------|
| D-pad | Move between controls |
| A | Press a button or switch; on a slider, start adjusting it |
| Left / Right while adjusting | Change the slider's value |
| A while adjusting | Keep the new value |
| B | Cancel an adjustment, close a list or popup, close the full window, or close the drawer, in that order |
| L1 / R1 | Jump between drawer sections; switch tabs in the full window |

A highlight shows which control has focus. Moving the mouse hides it.

Rest on a control for about a second and a hint describing it appears. To turn
the hints off, switch off **Hints on controller focus** in the Settings tab.
Hovering with the mouse still shows hints either way.

While voltaire is open, the controller drives voltaire instead of the game.
[Steam Gaming Mode](/guide/gaming-mode) explains how.

## Color picker

Choose **Custom** under a color to open the color picker over the page you are
on. Hue, Saturation and Lightness sliders each show what moving them will do,
and the lighting follows live as you drag. The eight preset colors are repeated
inside the picker. Click outside, press **Escape**, or press **B** to close it.

## Refresh rate

The refresh-rate control is the one control that does not go through the
daemon: the GUI asks the compositor directly. It works on **KDE Plasma** only,
and offers the rates your screen supports at its current resolution. In Steam
Gaming Mode it is hidden, because Steam sets the refresh rate there.

**Autoswitch** under it changes the rate when you plug or unplug the charger.
Turning it on sets **On AC** to your current rate and **On battery** to the
lowest rate your screen offers; change either as you like. Picking a rate in
those rows changes nothing until the power source changes. It needs the daemon
running (the daemon reports the power source) but not the window open.

## Brightness

The brightness control sets the screen's backlight. On KDE Plasma it goes
through KWin, so Plasma's own slider and on-screen display stay in step; on
other desktops and in Steam Gaming Mode it goes through logind. The lowest
setting is 1%, so the screen never turns fully dark.

## Volume

The volume control sets your default sound output through PipeWire (`wpctl`),
or `pactl` on a PulseAudio system. Moving it also unmutes the output.

## Desktop support {#desktop-support}

| Session | How the drawer appears |
|---------|------------------------|
| KDE Plasma, Hyprland, Sway | A panel at the screen edge, above other windows (Wayland layer-shell) |
| GNOME | A transparent fullscreen overlay with the drawer at the edge |
| Steam Gaming Mode | An overlay inside gamescope |

voltaire picks the right one automatically.

**GNOME** does not support the protocol other desktops use for edge panels,
so voltaire draws the drawer inside a transparent fullscreen window; clicks
outside the drawer pass through to the desktop. Every control works the same,
with a few differences:

- The drawer cannot appear above a fullscreen application.
- It opens on the current workspace only.
- It may appear in the Alt-Tab switcher while open.
- GNOME's top bar may hide while the drawer is open.
- Close it with **Escape** or by clicking another window.

Installing `gtk4-layer-shell` does not change this; the support has to come
from the desktop itself. For the full panel experience, use a KDE Plasma,
Hyprland or Sway session.

**Steam Gaming Mode** gets its own page:
[Steam Gaming Mode](/guide/gaming-mode). There the full window opens
fullscreen inside voltaire's overlay, with minimize and close buttons that
return you to the game.
