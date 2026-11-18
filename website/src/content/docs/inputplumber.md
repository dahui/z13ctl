---
title: Using InputPlumber
description: How voltaire's hardware button works on a handheld whose controller InputPlumber manages, what voltaire changes in InputPlumber, and what it needs.
---

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
[Device Files](/voltaire/dev/device-files/)). Today that is the OneXPlayer X2
Mini Pro.
