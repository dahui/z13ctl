---
title: Troubleshooting
description: Common problems with voltaire and how to fix them, and how to collect logs for a bug report.
---

# Troubleshooting

Each entry starts with what you see, then how to fix it. If nothing here
matches, [collect the logs](#collect-logs) and open an issue on
[GitHub](https://github.com/dahui/voltaire/issues).

## Setup and permissions

### "Permission denied" when changing a setting

voltaire runs as your user, not as root, so it can only change what
`voltaire setup` has granted. Run setup (again):

```sh
sudo voltaire setup
```

Setup grants access to the `users` group by default. If you are still refused,
check the next entry.

### Setup ran, but I still get "permission denied"

Your user must be in the group setup granted (normally `users`), and a new
group membership only takes effect in a new login session. Check with:

```sh
groups
```

If the group is missing, add yourself and then **log out and back in** (or
reboot):

```sh
sudo usermod -aG users $USER
```

At every boot a small system service re-applies the grants. To see what it
did, including any device node it corrected:

```sh
journalctl -b -u voltaire-perms
```

[What setup does](/installation#what-setup-does) explains the grants in
detail.

### `voltaire list` prints "No lighting devices found"

`voltaire list` looks only for RGB lighting devices. On a machine without
lighting voltaire can drive (the OneXPlayer X2 Mini Pro), an empty list is
expected; use `voltaire status` to check that voltaire can read your hardware.

On a machine with lighting, check that the kernel's HID modules are loaded and
the device nodes exist:

```sh
lsmod | grep hid
ls /dev/hidraw*
```

If nodes exist but none match the lighting USB IDs on your
[device page](/devices/), the lighting device is present but not recognized.
A device listed as `can't open` is a permissions problem: see the next
entries. On the ROG Xbox Ally X, InputPlumber hides the lighting; see
[below](#ally-lighting-unavailable).

### A setting disappeared after a reboot or an update

Run `sudo voltaire setup` once more. Packages run it for you on install and
upgrade; a hand-copied binary does not.

## The daemon

### The daemon is not running {#the-daemon-is-not-running}

**Symptom:** the GUI shows "voltaire daemon is not running", or settings that
need the daemon (autoswitch, custom profiles, the button) do nothing.

Check both units:

```sh
systemctl --user status voltaire.socket voltaire.service
```

If they are not enabled, enable and start them:

```sh
systemctl --user enable --now voltaire.socket voltaire.service
```

If the service starts and then fails, the reason is in its log:

```sh
journalctl --user -u voltaire -n 50
```

The `--user` matters: voltaire runs as a user service, and without it
`journalctl` looks at system services and prints nothing.
[Running the daemon](/reference/daemon#running-the-daemon) has the details.

### My fan curve or power limit keeps resetting

Other software on your system can switch the power profile, for example
power-profiles-daemon on every charger change, GNOME's power modes, Fn+F5 on
the Z13, `asusctl` or `tuned`. A profile switch drops a custom fan curve and
can put the firmware's own power limits back.

While a **custom profile** is active, the daemon notices and puts your curve
and limits back within about two seconds, so:

1. **Keep the daemon running.** Without it, a curve set with
   `voltaire fancurve --set` lasts only until the next profile change.
2. **Use a custom profile.** On a firmware profile, voltaire deliberately
   leaves the fans and power limits to the firmware. See
   [Custom profiles](/guide/profiles-and-power#custom-profiles).

The daemon log shows each time it restores your settings:

```sh
journalctl --user -u voltaire -f
# reconciling custom thermal settings  reason="saved custom fan curve was disabled" ...
```

### My profile changes on its own on battery

Autoswitch only acts when you plug in or unplug. A desktop feature such as
GNOME's **Automatic Power Saver** reacts to low battery instead, so it can
still change a firmware profile afterwards. Turn that feature off, or leave
one side of autoswitch empty so your desktop owns it entirely. See
[Autoswitch](/guide/profiles-and-power#autoswitch).

## Opening voltaire

### The hardware button does not open voltaire

Work through these in order:

1. **Is a button chosen?** `voltaire button --list` shows which button opens
   voltaire. If it says none, choose one in **Settings → Open voltaire with**
   or with `voltaire button --set default`. See
   [Opening voltaire](/guide/buttons-and-controllers#opening-voltaire).
2. **Is the daemon running, and without `--no-button`?** The daemon watches
   the button; `--no-button` turns that off. See
   [the entry above](#the-daemon-is-not-running).
3. **Does InputPlumber manage your controller?** On a handheld where it does,
   voltaire reaches the button through InputPlumber, which allows that only
   for members of the `wheel` (or `inputplumber`) group. A log line
   mentioning "is this user in wheel" means your user needs to be added. See
   [InputPlumber](/guide/buttons-and-controllers#inputplumber).
4. **Does another program hold the button?** The log line
   `button watcher stopped; retrying ... permission denied` means something
   else has the device to itself. If the log shows nothing at all, find what
   has it open with `sudo fuser -v /dev/input/eventN` (the log names the
   event node voltaire watches).

```sh
journalctl --user -u voltaire -b | grep -i button
```

### The drawer does not appear

1. Check the daemon is running ([above](#the-daemon-is-not-running)).
2. Check the GUI is running:

   ```sh
   systemctl --user status voltaire-gui
   journalctl --user -u voltaire-gui -n 50
   ```

3. Start it by hand with debug output to see what goes wrong:

   ```sh
   systemctl --user stop voltaire-gui
   voltaire-gui --debug
   ```

   Restart the service afterwards with `systemctl --user start voltaire-gui`.

### On GNOME, the drawer looks different or will not cover a game

That is expected. GNOME does not support the edge panels other desktops use,
so voltaire draws the drawer inside a transparent fullscreen window. It cannot
appear above a fullscreen application, and it opens on the current workspace
only. [Desktop support](/guide/gui#desktop-support) lists the differences.

To confirm which mode the GUI chose, look for the `backend` line in its log:
`mode=layer-shell` (KDE Plasma, Hyprland, Sway), `mode=overlay` (GNOME) or
`mode=gamescope` (Steam Gaming Mode).

```sh
journalctl --user -u voltaire-gui -b | grep backend
```

## Controllers

### The controller still reaches the game while voltaire is open

In Steam Gaming Mode, voltaire keeps controller input away from the game while
the drawer is open. How it does that depends on your setup:

- **On a device InputPlumber manages**, the daemon asks InputPlumber to hand
  the controller over, which needs your user in the `wheel` (or
  `inputplumber`) group. The log line
  `cannot take the controller (is this user in wheel?)` means it is not.
- **Otherwise**, voltaire-gui blocks the controller with two file
  capabilities. The packages set them; if you copied the binary yourself, or
  replaced it, set them again:

  ```sh
  sudo setcap cap_bpf,cap_perfmon+ep /usr/local/bin/voltaire-gui
  getcap /usr/local/bin/voltaire-gui
  ```

  Without them, voltaire pauses Steam instead while the drawer is open, which
  also pauses the game. The GUI log says
  `steam: BPF LSM unavailable, using SIGSTOP fallback` when that happens.
  [Gamepad input blocking](/installation#gamepad-input-blocking-capabilities)
  explains what the capabilities allow.

See [Steam Gaming Mode](/guide/gaming-mode) and
[Buttons and controllers](/guide/buttons-and-controllers).

### The ROG Xbox Ally X's joystick lighting says "unavailable" {#ally-lighting-unavailable}

InputPlumber hides the interface the lighting uses. Let setup tell InputPlumber
to leave it visible, then restart the daemon:

```sh
sudo voltaire setup --inputplumber-passthrough
systemctl --user restart voltaire.service
```

[Ally lighting](/guide/buttons-and-controllers#ally-lighting) explains what
this changes and how to undo it.

### A control says it is unavailable

The note names what is missing (a kernel module, an InputPlumber setting) or
who owns the setting right now (Steam, in Gaming Mode). Follow it, then restart
the daemon or reboot. See
[When a control says it is unavailable](/guide/gui#when-a-control-says-it-is-unavailable).

## Sleep

### The machine wakes up right after suspending

Before sleeping with a custom profile active, voltaire hands the fans back to
the firmware so they can stop, then waits three seconds before letting the
suspend go ahead; a suspend straight after the hand-back is often woken by the
embedded controller. After each resume the daemon logs what woke the machine:

```sh
journalctl --user -u voltaire -b | grep -E "entering sleep|held the suspend|handing the suspend|wake report"
```

In the `wake report` line, `slept` is how long the suspend lasted, `hw_sleep`
how much of that the hardware really spent asleep, and `woken_by` the wakeup
sources the kernel counted. A few seconds of `slept` with almost no `hw_sleep`
is an early wake. The `handing the suspend` line before it says whether
voltaire changed the fans or power limits just before the suspend.

To find out whether voltaire is involved, compare suspends (lid closed for a
couple of minutes each):

1. With voltaire stopped: `systemctl --user stop voltaire.service voltaire.socket`.
2. On a firmware profile, where voltaire writes nothing around sleep:
   `voltaire profile --set balanced` (or another of your device's profiles).
3. On your custom profile with the pre-sleep fan hand-back turned off: see
   [Fans](/guide/fans).

If the machine wakes early in all three, the cause is elsewhere. On the Z13,
the touchscreen and the detachable keyboard are common wake sources.

### The OneXPlayer X2 Mini Pro locks up when it suspends

On this machine suspend needs two kernel parameters,
`amd_iommu=off mem_sleep_default=s2idle`. Without them it hard-locks and has to
be forced off. See the
[OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) page for details
and what the parameters cost.

## Monitoring

### The Dashboard's Power card has no package power line

The package power counter is readable only after setup has granted it. Run
`sudo voltaire setup` again.

### The Dashboard says the daemon does not serve telemetry history

The daemon is older than the GUI. Restart it after upgrading:

```sh
systemctl --user restart voltaire
```

## Collect logs {#collect-logs}

Include these in a bug report:

```sh
voltaire --version
voltaire status
journalctl --user -u voltaire -b       # the daemon, this boot
journalctl --user -u voltaire-gui -b   # the GUI, this boot
journalctl -b -u voltaire-perms        # the boot-time permission service
```

For more detail from the GUI, run it in a terminal with `voltaire-gui --debug`
(stop the service first: `systemctl --user stop voltaire-gui`).
