# z13ctl

RGB lighting, performance profiles, battery limit, and display control for the
2025 ASUS ROG Flow Z13 on Linux.

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

`z13ctl` implements the Aura HID protocol directly against the Linux `hidraw`
interface — no kernel patches, no external daemons. System settings (profiles,
battery limit, boot sound, panel overdrive) use the standard asus-wmi and
asus-armoury sysfs interfaces. A background daemon persists state across reboots
and watches the Armoury Crate button.

## Install

```sh
# Arch Linux (AUR)
yay -S z13ctl-bin

# Debian / Ubuntu
sudo apt install ./z13ctl_*.deb

# Fedora / RHEL
sudo dnf install ./z13ctl_*.rpm

# Homebrew (Linuxbrew)
brew install dahui/z13ctl/z13ctl

# Manual (from release tarball)
tar xzf z13ctl_*_linux_amd64.tar.gz
sudo install -Dm755 z13ctl /usr/local/bin/z13ctl
sudo z13ctl setup
```

See the [Installation guide](https://dahui.github.io/z13ctl/installation/) for
systemd service setup, source builds, and uninstall instructions.

## Quick Start

```sh
# Solid cyan at full brightness
z13ctl apply --color cyan --brightness high

# Breathing red
z13ctl apply --mode breathe --color red --speed slow

# Rainbow wave
z13ctl apply --mode rainbow --speed normal

# Turn off lighting
z13ctl off

# Set performance profile
z13ctl profile --set balanced

# Cap battery charge at 80%
z13ctl batterylimit --set 80
```

## Documentation

Full documentation at **<https://dahui.github.io/z13ctl>**

- [Installation](https://dahui.github.io/z13ctl/installation/)
- [Quick Start](https://dahui.github.io/z13ctl/getting-started/)
- [Commands](https://dahui.github.io/z13ctl/commands/)
- [Daemon](https://dahui.github.io/z13ctl/daemon/)
- [API](https://dahui.github.io/z13ctl/api/)
- [Contributing](https://dahui.github.io/z13ctl/contributing/)

## License

[Apache 2.0](LICENSE)
