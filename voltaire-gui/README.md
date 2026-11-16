# voltaire-gui

GTK4 overlay drawer for [Voltaire](https://github.com/dahui/voltaire) on
Wayland. Formerly **z13gui**.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](../LICENSE)

![voltaire-gui](../assets/screen3.png)

`voltaire-gui` is the graphical side of Voltaire — a slide-out drawer triggered
by the Armoury Crate button (KEY_PROG3). It renders as a Wayland layer-shell
overlay (KDE Plasma, Hyprland, Sway), as a transparent click-through overlay on
compositors without layer-shell (GNOME — see
[GNOME support](https://dahui.github.io/voltaire/gui/#gnome-support)), or as a
gamescope X11 overlay in Steam Gaming Mode. All hardware communication goes
through the voltaire daemon.

## Install

`voltaire-gui` ships in the same package as the `voltaire` CLI and daemon, so
installing Voltaire installs both:

```sh
# Arch Linux (AUR)
yay -S voltaire-bin

# Debian / Ubuntu
sudo apt install ./voltaire_*.deb

# Fedora / RHEL
sudo dnf install ./voltaire_*.rpm

# Manual (from release tarball, which carries both binaries)
tar xzf voltaire_*_linux_amd64.tar.gz
sudo install -Dm755 voltaire /usr/local/bin/voltaire
sudo install -Dm755 voltaire-gui /usr/local/bin/voltaire-gui
sudo voltaire setup
```

See the [Installation guide](https://dahui.github.io/voltaire/installation/)
for systemd service setup, source builds, and uninstall instructions.

## Quick Start

Press the **Armoury Crate button** on your Z13 to open the drawer. Press it
again, click outside, or press Escape to close it.

The drawer holds the quick controls: firmware and custom profile selection,
AC/battery autoswitch, battery charge limit and RGB lighting, with refresh rate
and CPU boost available as extras. Which sections appear, and in what order, is
yours to choose.

**Double-press** the button, or tap the expand button at the bottom of the
drawer, to open the full window. Its **Dashboard** charts live telemetry
beside the same controls; **Profiles** edits custom profiles (power limits,
fan curve, undervolt); **Settings** holds the firmware switches, the quickbar
layout and voltaire's own preferences. All changes go to the voltaire daemon
and persist across reboots.

## Documentation

Full documentation at **<https://dahui.github.io/voltaire>**

- [Installation](https://dahui.github.io/voltaire/installation/)
- [GUI Overview](https://dahui.github.io/voltaire/gui/)
- [Quick Start](https://dahui.github.io/voltaire/gui/quick-start/)
- [Configuration](https://dahui.github.io/voltaire/gui/configuration/)
- [Theming](https://dahui.github.io/voltaire/gui/theming/)
- [Migrating from z13gui](https://dahui.github.io/voltaire/migrating-from-z13ctl/)
- [Contributing](https://dahui.github.io/voltaire/contributing/)

## License

[Apache 2.0](../LICENSE)
