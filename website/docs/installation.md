---
title: Installation
description: Install voltaire and its GUI on Arch, Debian, Ubuntu, Fedora or any other distribution, and run the one-time setup.
---

# Installation

One package installs everything: **`voltaire`**, the command-line tool and the
background daemon that talks to the hardware, and **`voltaire-gui`**, the
overlay that drives it. They are released together, so they never fall out of
step.

::: tip Upgrading from z13ctl or z13gui?
Install voltaire over them: your settings carry over and the old packages are
replaced. See [Upgrading from z13ctl](/migrating-from-z13ctl).
:::

## Requirements

- A [supported device](/devices/).
- A current Linux kernel. Some features need a recent one; your device's page
  says which.
- For the GUI: a Wayland desktop, or Steam Gaming Mode. KDE Plasma, Hyprland
  and Sway show the drawer as a panel at the screen's edge; GNOME is supported
  with some differences ([desktop support](/guide/gui#desktop-support)).
- If InputPlumber runs on your handheld, see
  [InputPlumber](/guide/buttons-and-controllers#inputplumber) after
  installing.

### Runtime dependencies {#runtime-dependencies-gui-only}

`voltaire-gui` needs GTK 4 and gtk4-layer-shell. The packages pull them in;
for a tarball install, install them with your package manager first:

| Dependency | Arch | Debian / Ubuntu | Fedora |
|---|---|---|---|
| GTK 4 | `gtk4` | `libgtk-4-1` | `gtk4` |
| gtk4-layer-shell | `gtk4-layer-shell` | `libgtk4-layer-shell0` | `gtk4-layer-shell` |

## Install

### Arch Linux and CachyOS (AUR)

Install [voltaire-bin](https://aur.archlinux.org/packages/voltaire-bin) with
your AUR helper:

```sh
yay -S voltaire-bin
```

Or download the `.pkg.tar.zst` from the
[Releases](https://github.com/dahui/voltaire/releases) page and install it
with `sudo pacman -U voltaire_*.pkg.tar.zst`. Then continue with
[After installing](#after-installing).

### Debian, Ubuntu and Fedora

Download the `.deb` or `.rpm` from the
[Releases](https://github.com/dahui/voltaire/releases) page and install it:

```sh
sudo apt install ./voltaire_*.deb     # Debian, Ubuntu
sudo dnf install ./voltaire_*.rpm     # Fedora
```

Then continue with [After installing](#after-installing).

### Release tarball

Download the `linux_amd64` archive from the
[Releases](https://github.com/dahui/voltaire/releases) page, then extract and
install both binaries. A headless install can skip `voltaire-gui`.

```sh
tar xzf voltaire_*_linux_amd64.tar.gz
sudo install -Dm755 voltaire     /usr/local/bin/voltaire
sudo install -Dm755 voltaire-gui /usr/local/bin/voltaire-gui
sudo setcap cap_bpf,cap_perfmon+ep /usr/local/bin/voltaire-gui   # see "Gamepad input blocking"

# Optional: keep scripts that call the old names working
sudo ln -sfn voltaire     /usr/local/bin/z13ctl
sudo ln -sfn voltaire-gui /usr/local/bin/z13gui
```

Grant access to the hardware:

```sh
sudo voltaire setup
```

Install and start the user services, and the app-menu entry:

```sh
install -Dm644 contrib/systemd/user/voltaire.socket     ~/.config/systemd/user/voltaire.socket
install -Dm644 contrib/systemd/user/voltaire.service    ~/.config/systemd/user/voltaire.service
install -Dm644 contrib/systemd/user/voltaire-gui.service ~/.config/systemd/user/voltaire-gui.service
systemctl --user daemon-reload
systemctl --user enable --now voltaire.socket voltaire.service voltaire-gui.service

install -Dm644 contrib/voltaire-gui.desktop ~/.local/share/applications/voltaire-gui.desktop
```

Optionally, `contrib/udev/99-voltaire-gamepad.rules` (copy it to
`/etc/udev/rules.d/`) lets the GUI read controllers that your system does not
already give the logged-in user. Then add yourself to the group, as in
[After installing](#after-installing).

### From source

You need Go 1.26 or later, and for the GUI the GTK 4 development headers:

```sh
sudo pacman -S gtk4 gtk4-layer-shell                         # Arch
sudo apt-get install -y libgtk-4-dev libgtk4-layer-shell-dev # Debian, Ubuntu
sudo dnf install gtk4-devel gtk4-layer-shell-devel           # Fedora
```

Then build and install:

```sh
git clone https://github.com/dahui/voltaire
cd voltaire
make build-all          # both binaries; the first GUI build takes several minutes
sudo make install       # to /usr/local/bin, with the z13ctl/z13gui symlinks
sudo voltaire setup
make install-service    # installs and enables the three user services
```

`make build` builds the command-line tool alone, if you don't want the GUI.
Always rebuild both together with `make build-all`: `make install` warns if it
finds a GUI built from a different checkout.

## After installing

1. **Add yourself to the group** that setup granted access to (`users` by
   default), if you are not already in it, then log out and back in:

   ```sh
   sudo usermod -aG users $USER
   ```

2. **Start voltaire.** The packages enable the services for every user, so
   they start at your next login. To start them now instead:

   ```sh
   systemctl --user start voltaire.socket voltaire-gui.service
   ```

3. **Check it works:**

   ```sh
   voltaire status
   ```

   Then press your device's hardware button, or open **Voltaire** from your
   app menu: the drawer slides in from the edge of the screen.

The packages run `sudo voltaire setup` for you on every install and upgrade.
Next, see the [Quick Start](/quick-start).

## What setup does {#what-setup-does}

voltaire runs as you, not as root. `sudo voltaire setup` gives one group
(`users` by default) access to exactly what this machine needs: the lighting,
the hardware buttons, and the settings for profiles, fans, power limits,
firmware switches, the charge limit, CPU boost and power readings. It reads
which machine this is from the firmware and grants only what that
[device](/devices/) uses, nothing another machine would need.

It writes a udev rules file and a small boot service, both marked with the
machine and group they were written for, applies the grants right away, and
cleans up anything an old `z13ctl setup` left behind.

- **Preview it** without root or changes: `voltaire --dry-run setup`.
- **Another group:** `sudo voltaire setup --group NAME`.
- **Undo it:** `sudo voltaire setup --remove` removes both files and takes the
  access back. The packages do this when you uninstall them.
- **A machine voltaire does not know:** setup grants nothing and prints the
  firmware's vendor and product names; please
  [report them](https://github.com/dahui/voltaire/issues). If it is the same
  hardware sold under another name, `--machine ID` uses that device's file
  instead.
- **The ROG Xbox Ally X's ring lighting under InputPlumber** needs one opt-in
  option, `--inputplumber-passthrough`, which changes InputPlumber's
  configuration for that machine. See
  [The Ally's lighting](/guide/buttons-and-controllers#ally-lighting).
- **Later runs keep your choices.** A plain `sudo voltaire setup`, like the
  one a package upgrade runs, keeps the group, `--machine` and the
  passthrough option from the previous run unless you give them again.

## Optional: ryzen_smu {#ryzen-smu}

The `ryzen_smu` kernel module, installed separately, gives voltaire access to
the processor's own power controller. Without it everything else works, and
voltaire says what to install where a feature would be.

| Device | What needs ryzen_smu |
|---|---|
| [ROG Flow Z13 (2025)](/devices/asus-rog-flow-z13-2025) | CPU undervolt |
| [OneXPlayer X2 Mini Pro](/devices/onexplayer-x2-mini-pro) | The TDP slider and the CPU undervolt |
| [ROG Xbox Ally X](/devices/asus-rog-xbox-ally-x) | Not used |

::: warning Use the amkillam fork
The original `ryzen_smu` does not support Strix Halo, the processor in the Z13
and the X2 Mini Pro. Install the
[amkillam/ryzen_smu](https://github.com/amkillam/ryzen_smu) fork. If the wrong
one is installed, voltaire reports undervolting as unavailable after the first
attempt.
:::

```sh
# Arch, CachyOS
yay -S ryzen_smu-dkms-git

# Other distributions: build the amkillam fork
git clone https://github.com/amkillam/ryzen_smu.git
cd ryzen_smu
make
sudo make install
```

Then run `sudo voltaire setup` again so voltaire's group can reach the module,
and reboot (or `sudo modprobe ryzen_smu` and
`systemctl --user restart voltaire`).

## Gamepad input blocking {#gamepad-input-blocking-capabilities}

In Steam Gaming Mode, while the drawer is open, your controller presses should
move around the drawer and not reach the game. The `setcap` step above gives
`voltaire-gui` two kernel capabilities (`CAP_BPF` and `CAP_PERFMON`) that let
it hold back Steam's controller reads while the game keeps running. Without
them it pauses Steam instead, which pauses the game too.

The packages and `sudo make install` set the capabilities for you. A copied
binary loses them, so run `setcap` again whenever you replace it by hand, and
check with `getcap /usr/local/bin/voltaire-gui`.

The capabilities only allow that one filter on controller reads, removed when
`voltaire-gui` exits, and the GUI gives them up at startup outside Gaming
Mode. On handhelds where InputPlumber manages the controller, voltaire asks
InputPlumber to hand the controller over instead.

## Uninstall {#uninstall}

Remove the package with your package manager. It undoes setup and disables
the services:

```sh
yay -R voltaire-bin          # Arch, CachyOS (or: sudo pacman -R voltaire-bin)
sudo apt remove voltaire     # Debian, Ubuntu
sudo dnf remove voltaire     # Fedora
```

For a tarball or source install, remove the services, then setup's grants
and the files:

```sh
systemctl --user disable --now voltaire.socket voltaire.service voltaire-gui.service
rm -f ~/.config/systemd/user/voltaire.socket \
      ~/.config/systemd/user/voltaire.service \
      ~/.config/systemd/user/voltaire-gui.service
systemctl --user daemon-reload

sudo voltaire setup --remove
sudo rm -f /usr/local/bin/voltaire /usr/local/bin/voltaire-gui \
           /usr/local/bin/z13ctl /usr/local/bin/z13gui
rm -f ~/.local/share/applications/voltaire-gui.desktop
```

From a source checkout, `make uninstall-service` does the first part.

Your settings stay in `~/.local/state/voltaire/` and `~/.config/voltaire/`;
delete those folders to remove them too.

## Having trouble?

See [Troubleshooting](/troubleshooting) for permission errors, a button that
does nothing, or a drawer that does not appear.
