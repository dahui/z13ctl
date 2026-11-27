---
title: Steam Gaming Mode
description: How voltaire behaves in Steam Gaming Mode (gamescope), what Steam controls instead, and what to check when something does not work.
---

# Steam Gaming Mode

voltaire works in Steam Gaming Mode, the full-screen Steam session handhelds
boot into (it runs on the gamescope compositor). The GUI detects it by itself;
there is nothing to configure. A few things look or behave differently there,
and on the ROG Xbox Ally X, Steam takes over two settings.

## What changes in the GUI

- **The drawer opens over your game,** drawn in Steam's overlay layer. Press
  your [voltaire button](/guide/buttons-and-controllers#opening-voltaire) as
  usual.
- **The full window fills the screen** inside voltaire's own surface rather
  than opening as a separate window, because Gaming Mode shows only one
  application window at a time. The pages and controls are the same: L1 and R1
  switch tabs, and B closes it. Its title row has **minimize** and **close**
  (no maximize, since it already fills the screen); both take you back to the
  game. The expand button at the bottom of the drawer opens it without a
  double press.
- **Menus, the color picker and hints are drawn inside voltaire's surface,**
  so they work the same as on the desktop.
- **The interface scales to the screen.** If it looks too large or too small,
  set `VOLTAIRE_GUI_SCALE`; see
  [Environment variables](/reference/configuration#environment-variables).
- **There is no refresh-rate control.** Steam sets the refresh rate in Gaming
  Mode, from its Quick Access menu, and would overwrite voltaire's choice.
  Screen brightness and volume work as usual.

## The controller while voltaire is open

While the drawer or full window is open, the controller drives voltaire and
the game sees no presses. voltaire picks the way to do this by itself:

- **Where InputPlumber manages the controller** (usual on the OneXPlayer and
  the Ally), voltaire asks InputPlumber to send the presses to voltaire
  instead of the game. This needs your user in the `wheel` or `inputplumber`
  group; see [InputPlumber](/guide/buttons-and-controllers#inputplumber).
- **Otherwise,** voltaire-gui blocks the game's reads of the controller with a
  small kernel filter. The packages grant the two capabilities this needs;
  [Installation](/installation) covers granting them by hand.
- **If neither is possible,** voltaire pauses Steam while it is open, which
  pauses the game too.

The controller goes back to the game as soon as voltaire closes.

## What Steam controls {#steam-controls}

On the **ROG Xbox Ally X**, Steam manages the **power limits** (its
performance overlay's TDP setting) and the **charge limit** in Gaming Mode.
If voltaire set them too, the two would overwrite each other, so voltaire hands
them over:

- The Dashboard's POWER card shows a note where the power limits would be:
  *Steam sets the power limits in Gaming Mode (its performance overlay);
  switch to the desktop to set them here.*
- The charge-limit slider and [full charge once](/guide/battery#full-charge-once)
  are hidden.
- `voltaire tdp` and `voltaire batterylimit` refuse with the same reason.
- A [custom profile](/guide/profiles-and-power#custom-profiles) still applies
  its fan curve, but not its power limits.

To change either setting with voltaire, switch to the desktop. The voltaire
daemon notices the switch within about ten seconds and restarts itself so the
controls follow.

Which settings Steam owns is part of each device's description in voltaire.
Today only the Ally hands any over; on the ROG Flow Z13 and the OneXPlayer X2
Mini Pro every control stays in voltaire's hands in Gaming Mode.

## What still works

Everything else works as on the desktop:

- [firmware and custom profiles](/guide/profiles-and-power) and
  [AC/battery autoswitch](/guide/profiles-and-power#autoswitch);
- [fan curves](/guide/fans), CPU boost and, on the OneXPlayer, the TDP and
  undervolt sliders;
- [lighting](/guide/lighting);
- the battery readout and the Dashboard's [charts](/guide/monitoring);
- firmware switches and [controller vibration](/guide/buttons-and-controllers#vibration);
- screen brightness and volume.

## If something does not work

- **The drawer doesn't appear.** Check that the daemon is running
  (`systemctl --user status voltaire.service`), then see
  [Troubleshooting](/troubleshooting).
- **The button does nothing on a handheld.** If InputPlumber manages the
  controller, your user must be in `wheel` or `inputplumber`; see
  [InputPlumber](/guide/buttons-and-controllers#inputplumber).
- **The game reacts to presses while voltaire is open, or pauses.** voltaire
  could not block the controller and fell back to another method; see
  [The controller while voltaire is open](#the-controller-while-voltaire-is-open).
- **A power or charge-limit control is missing on the Ally.** That is Steam's
  setting in Gaming Mode; see [What Steam controls](#steam-controls).
