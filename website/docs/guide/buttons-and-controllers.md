---
title: Buttons and controllers
description: Which hardware button opens voltaire, controller vibration, how voltaire works with InputPlumber, and the ROG Xbox Ally X's joystick lighting.
---

# Buttons and controllers

voltaire opens from a hardware button on your machine, and on a handheld it
works with the built-in controller: you can drive the GUI with it, set its
vibration strength where the hardware allows, and share it with InputPlumber,
the input service many handheld distributions run.

## Opening voltaire {#opening-voltaire}

A single press of the button opens the drawer (the quickbar). A quick second
press opens the full window instead. Press the button again, press Escape, or
click outside to close it. The drawer's expand button, at the bottom beside
the theme button, also opens the full window.

| Device | Buttons that can open voltaire (default first) |
|---|---|
| [ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | Armoury Crate button |
| [ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | Armoury Crate button, Library button |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | Keyboard button, OneXPlayer button, Home button |

The choice is limited to buttons that do nothing else on that machine.
voltaire never learns a key from a keyboard, so it can't see what you type.

### In the GUI

On the full window's **Settings** tab, the **VOLTAIRE** card has two rows:

- **Open voltaire with** picks the button, or **No button**. With no button,
  open voltaire from its desktop entry.
- The row named after your button (for example **Armoury Crate button**)
  chooses what a single press opens: **Quickbar** or **Full window**. A double
  press always opens the other one.

Both take effect at once.

### On the command line

```sh
voltaire button --list           # this machine's buttons; * marks the one in use
voltaire button --get
voltaire button --set library    # an id from --list
voltaire button --set none       # no button opens voltaire
voltaire button --set default
```

`--set` needs the daemon, which keeps the choice. To turn the button watcher
off completely, for example when another tool needs the button, run the
daemon with `--no-button`.

### Things to know

- **On the Z13 the keypress also reaches your desktop.** No common desktop
  uses the Armoury Crate key, but if yours does, unbind it there or choose
  **No button**.
- **Under InputPlumber, the button loses its old job while it opens voltaire**
  (see [below](#inputplumber)). On the OneXPlayer, for example, the Keyboard
  button stops taking screenshots, and choosing the OneXPlayer button means it
  no longer opens Steam's Quick Access menu.
- To navigate the GUI with a controller (D-pad, A, B, shoulder buttons), see
  [Gamepad navigation](/guide/gui#gamepad).

## Controller vibration {#vibration}

Vibration strength scales every rumble a game asks for, from 0 (off) up to
the controller's maximum. The daemon remembers it and puts it back after a
reboot or a suspend.

| Device | Vibration strength |
|---|---|
| OneXPlayer X2 Mini Pro | 0 (off) to 5 |
| ROG Xbox Ally X | Not yet ([why](/devices/asus-rog-xbox-ally-x)) |
| ROG Flow Z13 (2025) | No built-in controller |

### In the GUI

Use the **Vibration strength** slider in the **CONTROLLER** card on the full
window's **Settings** tab, or the drawer's **Controller vibration** section
(in the OneXPlayer's drawer by default; otherwise turn it on in Settings →
Quickbar).

### On the command line

```sh
voltaire rumble --get
voltaire rumble --set 2
```

### Things to know

- The setting needs the permission `sudo voltaire setup` grants. If you
  installed voltaire before your device had this setting, run setup again.
- Set with the daemon stopped, the change applies now but is not restored
  after a reboot.

## InputPlumber {#inputplumber}

[InputPlumber](https://github.com/ShadowBlip/InputPlumber) is an input service
that many handheld distributions ship (Bazzite, ChimeraOS and others; on
CachyOS it starts by itself once installed). It takes the controller over
completely: it hides the real controller from every other program and gives
games a virtual one instead. That includes the buttons voltaire opens with.

### What voltaire does on the OneXPlayer and the Ally

On these two machines voltaire works with InputPlumber on its own; there is
nothing to set up.

- **The button.** voltaire asks InputPlumber to send it the button that opens
  voltaire. Only that one button changes, and only while the daemon runs:
  choose another button and the change moves with it; choose **No button** or
  stop the daemon and InputPlumber's own mapping comes back. voltaire never
  writes InputPlumber's files for this, and it sets the mapping again if
  InputPlumber restarts.
- **The controller in voltaire's window.** While the drawer or full window is
  open, voltaire asks InputPlumber to send the controller's presses to voltaire
  instead of the game, so the game and Steam see an idle controller. The
  controller goes back to the game the moment voltaire closes, and also if
  voltaire stops unexpectedly.

If InputPlumber is stopped or not installed, voltaire reads the buttons and
the controller directly, and switches between the two by itself.

### What it needs

Both of these are privileged InputPlumber requests, which InputPlumber allows
for members of the **`wheel`** or **`inputplumber`** group. Most handheld
distributions put the first user in `wheel`; check with `id`.

`voltaire setup` deliberately does not add anyone to the `inputplumber`
group, because that group can also type keystrokes through InputPlumber's
virtual keyboard. If your user is in neither group:

- the button does nothing while InputPlumber runs, and the daemon's log says
  `is this user in wheel or the inputplumber group?`;
- in voltaire's window the controller still works on the OneXPlayer (voltaire
  reads InputPlumber's virtual controller itself), but not on the Ally.

### The ROG Flow Z13 under InputPlumber

Some distributions ship an InputPlumber configuration for the Z13 that takes
over "Asus WMI hotkeys", the device the Armoury Crate button is on. voltaire
then can't read the button, and its log repeats
`button watcher stopped; retrying ... permission denied`.

To give the button back to voltaire, tell InputPlumber to leave that one
device alone. Copy its configuration to the override directory (never edit
the one under `/usr/share`, which updates replace):

```sh
ls /usr/share/inputplumber/devices/ | grep -i z13     # usually 50-rog_flow_z13.yaml
sudo mkdir -p /etc/inputplumber/devices.d
sudo cp /usr/share/inputplumber/devices/50-rog_flow_z13.yaml /etc/inputplumber/devices.d/
```

In the copy, add `ignore: true` to the source for "Asus WMI hotkeys":

```yaml
source_devices:
  - group: keyboard
    ignore: true        # leave this device to voltaire
    evdev:
      name: Asus WMI hotkeys
      handler: event*
```

Then restart InputPlumber, restore voltaire's permissions (InputPlumber may
not hand them back when it lets go of a device), and restart the daemon:

```sh
sudo systemctl restart inputplumber.service
sudo voltaire setup --perms-only
systemctl --user restart voltaire.service
```

The rest of InputPlumber keeps working. A copy made this way does not follow
InputPlumber's updates to its own file. If you would rather leave the button
to InputPlumber, choose **No button** instead.

### Checking it

```sh
voltaire button --list
journalctl --user -u voltaire -b | grep -i inputplumber
```

The log names the button each time voltaire's mapping goes in or comes out.

## The ROG Xbox Ally X's joystick lighting {#ally-lighting}

The Ally's ring lighting is set through one of the controller's interfaces.
InputPlumber hides that interface along with the rest of the controller, even
though it reads no input from it, so voltaire can't reach the lighting and
shows it as unavailable. Without InputPlumber there is nothing to do.

InputPlumber has a setting to leave one interface visible while still using
it. voltaire turns it on for you, but only if you ask:

```sh
sudo voltaire setup --inputplumber-passthrough
systemctl --user restart voltaire.service
```

Setup writes an override of InputPlumber's configuration for this machine
into `/etc/inputplumber/devices.d/` that differs from InputPlumber's own by
that one setting on that one interface, restarts InputPlumber (the
controller pauses for a moment), and gives voltaire access to the interface.
Controller input, rumble and voltaire's controller navigation all keep
working.

- **It follows InputPlumber's updates.** Setup remakes the override from
  InputPlumber's current file every time it runs, including at every boot. If
  InputPlumber's file ever changes in a way setup can't safely edit, it writes
  no override and InputPlumber uses its own (the lighting is hidden again;
  the controller is unaffected).
- **It stays until you turn it off.** Plain `voltaire setup` runs, including
  the ones package upgrades make, keep it. To undo it:

  ```sh
  sudo voltaire setup --inputplumber-passthrough=false
  ```

  `sudo voltaire setup --remove`, which undoes all of setup, removes it too.
- **Setup only touches its own file.** A file of the same name that you wrote
  yourself is left alone, with a note.

::: info What access it gives
The same interface also carries the controller's own settings, such as button
remapping and vibration. After this change voltaire's group can write to it,
the same kind of trust that group already has over power limits and fan
curves; voltaire itself sends it lighting commands only.
:::

Without the option, `voltaire setup` prints a note when InputPlumber is hiding
this machine's lighting, naming the command above.
