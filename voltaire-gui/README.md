# z13gui

GTK4 overlay drawer for [z13ctl](https://github.com/dahui/z13ctl) on Wayland.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

![z13gui](assets/screen3.png)

`z13gui` is the graphical companion to z13ctl — a slide-out drawer triggered by the
Armoury Crate button (KEY_PROG3). It renders as a Wayland layer-shell overlay (KDE Plasma,
Hyprland, Sway), as a transparent click-through overlay on compositors without layer-shell
(GNOME — see [GNOME support](https://dahui.github.io/z13gui/#gnome-support)), or as a
gamescope X11 overlay in Steam Gaming Mode. All hardware communication goes through the
z13ctl daemon.

## Install

```sh
# Arch Linux (AUR)
yay -S z13gui-bin

# Debian / Ubuntu
sudo apt install ./z13gui_*.deb

# Fedora / RHEL
sudo dnf install ./z13gui_*.rpm

# Manual (from release tarball)
tar xzf z13gui_*_linux_amd64.tar.gz
sudo install -Dm755 z13gui /usr/local/bin/z13gui
```

See the [Installation guide](https://dahui.github.io/z13gui/installation/) for
systemd service setup, source builds, and uninstall instructions.

## Quick Start

Press the **Armoury Crate button** on your Z13 to open the drawer. Press it again,
click outside, or press Escape to close it.

The drawer controls profiles, battery charge limit, RGB lighting (mode, color, speed,
brightness), panel overdrive, and boot sound. All changes are sent to the z13ctl daemon
immediately and persist across reboots.

## Documentation

Full documentation at **<https://dahui.github.io/z13gui>**

- [Installation](https://dahui.github.io/z13gui/installation/)
- [Quick Start](https://dahui.github.io/z13gui/getting-started/)
- [Configuration](https://dahui.github.io/z13gui/configuration/)
- [Theming](https://dahui.github.io/z13gui/theming/)
- [Contributing](https://dahui.github.io/z13gui/contributing/)

## License

[Apache 2.0](LICENSE)
