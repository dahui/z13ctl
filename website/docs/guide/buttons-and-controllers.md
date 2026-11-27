---
title: Using InputPlumber
description: How voltaire's hardware button works on a handheld whose controller InputPlumber manages, what voltaire changes in InputPlumber, and what it needs.
---

# Using InputPlumber

[InputPlumber](https://github.com/ShadowBlip/InputPlumber) is the input daemon
many handheld distributions ship (Bazzite, ChimeraOS, and others; on CachyOS it
starts on its own once installed). It takes a handheld's controller over
completely: it grabs the controller's input devices and hides them, so no
other program can read them, and presents a virtual gamepad instead.

That includes the buttons voltaire opens with. On a device InputPlumber
manages, voltaire reaches the button **through InputPlumber**: it adds one
mapping to InputPlumber's running profile that turns the button into a
`voltaire_<button>` action on the system bus, and listens for it. Nothing to
set up; the daemon does it whenever InputPlumber is running.

## What voltaire changes

- **One mapping, for the button that opens voltaire, and only while the
  daemon runs.** Choose another button and the mapping moves; choose *No
  button* (`voltaire button --set none`) or stop the daemon and it comes out.
- **The button stops doing its old job while it opens voltaire.** On the
  OneXPlayer X2 Mini Pro, for example, InputPlumber's default profile sends the
  Keyboard button to *Screenshot*; while that button opens voltaire it does not
  take screenshots. The previous mapping is kept and put back when voltaire's
  comes out. The other buttons keep doing what they did.
- **No files.** voltaire never writes an InputPlumber file (not the
  package's, not `/etc/inputplumber`). The mapping lives in InputPlumber's
  memory only, which is why voltaire adds it again whenever it goes missing:
  when InputPlumber restarts (it starts every device on its default profile)
  and when another program loads a profile.

If InputPlumber is stopped or not installed, voltaire reads the button
directly, as on any other machine, and switches between the two on its own.

## What it needs

Loading a profile is a privileged InputPlumber call. Its shipped polkit rule
allows members of the **`wheel`** and **`inputplumber`** groups. Most handheld
distributions put the first user in `wheel`; check with `id`.

`voltaire setup` deliberately does **not** add anyone to the `inputplumber`
group: the same rule lets that group send keystrokes through InputPlumber's
virtual keyboard. If your user is in neither group, the daemon logs
`reading the profile failed (is this user in wheel or the inputplumber group?)`
and the button stays silent while InputPlumber runs. You can add the mapping
to a profile of your own by hand instead, mapping the button's capability to a
`dbus` target named `voltaire_<button id>`; `voltaire button --list` shows the
ids.

## The controller in voltaire's window

While voltaire's drawer or window is open, the controller drives voltaire and
not the game. Where InputPlumber manages the controller, the daemon asks
InputPlumber for its **intercept mode**: InputPlumber stops feeding the
virtual gamepad (games and Steam see an idle controller) and sends navigation
to voltaire over the system bus instead. It hands the controller back the
moment voltaire closes, and also if voltaire's GUI or daemon stops
unexpectedly. Changing the mode needs the same `wheel` or `inputplumber`
membership as the button mapping; without it, voltaire falls back to reading
the controller itself, which works where InputPlumber's virtual gamepad is an
ordinary one (the OneXPlayer) and not on the ROG Xbox Ally X, whose virtual
controller has nothing voltaire can read.

## The ROG Xbox Ally X's joystick lighting {#ally-lighting}

The Ally's ring lighting is set through one of the controller's interfaces
that InputPlumber hides (HID interface 2). InputPlumber reads no input from
it; it uses it to send rumble, and hides it only because it hides everything
it manages. Hidden, it is out of voltaire's reach, so the lighting shows as
unavailable with that reason.

InputPlumber has a setting for exactly this, `passthrough`: the interface is
still used, but not hidden. voltaire changes InputPlumber's configuration only
if you ask it to:

```sh
sudo voltaire setup --inputplumber-passthrough
systemctl --user restart voltaire.service
```

Setup reads InputPlumber's installed config for this machine
(`/usr/share/inputplumber/devices/50-rog_xbox_ally.yaml` on the Xbox Ally X),
adds `passthrough: true` to the source with the lighting's interface
(`interface_num: 2`; the gamepad's `interface_num: 5` is left alone), and
writes the result under the same name in `/etc/inputplumber/devices.d/`, which
InputPlumber reads in preference to its own. Then it restarts InputPlumber
(the controller pauses for a moment) and grants the interface.

- **It follows InputPlumber's updates.** The file is remade from
  InputPlumber's installed config every time setup runs, including at every
  boot, before InputPlumber starts. The edit is a single inserted line, and
  setup checks that this line is the only difference; if InputPlumber's file
  ever changes in a way that makes that impossible, setup writes no override
  and InputPlumber uses its own file (the lighting is hidden again, and the
  controller is unaffected). If InputPlumber starts passing the interface
  through itself, setup removes the override.
- **It stays until you turn it off.** Later plain `voltaire setup` runs,
  including the ones package upgrades make, keep it. To undo it:
  `sudo voltaire setup --inputplumber-passthrough=false` (or
  `sudo voltaire setup --remove`, which undoes all of setup).
- **Setup touches only its own file.** It marks the file in its first line,
  and a file of the same name without that mark (one you wrote yourself) is
  left alone, with a note.
- **voltaire's group can then write to that interface**, which also carries
  the controller's own settings (button remaps, gamepad mode, vibration).
  voltaire sends it lighting commands only; the grant is the same kind of
  trust the group already has over power limits and fan curves, and it is
  given only on this machine.
- Controller input, rumble and the drawer's controller navigation all work
  with it in place (checked on the Ally, 2026-11-26).

Without the option, setup notes when InputPlumber hides this machine's
lighting and names the command. To make the same change by hand instead,
copy the config to `/etc/inputplumber/devices.d/`, add `passthrough: true` to
the `interface_num: 2` source, and restart InputPlumber; a hand-made copy does
not follow InputPlumber's updates.

## Checking it

```sh
voltaire button --list                     # which button opens voltaire
journalctl --user -u voltaire -b | grep inputplumber
```

The log names the device and capability each time the mapping goes in or
comes out, and `button route carried=true` means presses are arriving through
InputPlumber.

## Which devices

A device file lists the InputPlumber capability for each of its buttons
(`inputplumber = "QuickAccess2"`; see
[Device Files](/dev/device-files)). Today that is the OneXPlayer X2
Mini Pro and the ROG Xbox Ally X.
