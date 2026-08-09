# Voltaire

RGB lighting, performance profiles, battery limit, fan curves, TDP control,
CPU undervolting, and display settings for the 2025 ASUS ROG Flow Z13 on
Linux.

> **Formerly z13ctl and z13gui.** The two projects merged and were renamed for
> 2.0 — same maintainers, same code lineage, one repository. Old links and Go
> module paths keep resolving via GitHub's redirect. Migration notes:
> [Migrating from z13ctl](https://dahui.github.io/voltaire/migrating-from-z13ctl/).

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

`voltaire` implements the Aura HID protocol directly against the Linux `hidraw`
interface — no kernel patches, no external daemons. System settings (profiles,
battery limit, boot sound, panel overdrive, fan curves, TDP) use the standard
asus-wmi and asus-armoury sysfs interfaces. CPU undervolting uses the
`ryzen_smu` kernel module for AMD Curve Optimizer control. A background daemon
persists state across reboots, restores volatile settings after sleep/resume,
re-applies keyboard lighting when the detachable keyboard is reattached, and
watches the Armoury Crate button.

> [!TIP]
> **New to Linux? Install voltaire-gui.** Most users (especially newcomers!)
> should use **voltaire-gui**, a touch-friendly graphical overlay that exposes
> every voltaire feature (lighting, fan curves, TDP, undervolt, profiles,
> battery limit) with no command line required. The raw `voltaire` CLI shines
> for scripting and advanced tuning, but if you're not a Linux veteran,
> install voltaire-gui alongside voltaire for a far smoother experience.

## Install

```sh
# Arch Linux (AUR)
yay -S voltaire-bin

# Debian / Ubuntu
sudo apt install ./voltaire_*.deb

# Fedora / RHEL
sudo dnf install ./voltaire_*.rpm

# Manual (from release tarball)
tar xzf voltaire_*_linux_amd64.tar.gz
sudo install -Dm755 voltaire /usr/local/bin/voltaire
sudo voltaire setup
```

See the [Installation guide](https://dahui.github.io/voltaire/installation/) for
systemd service setup, source builds, and uninstall instructions.

## Quick Start

```sh
# Solid cyan at full brightness
voltaire apply --color cyan --brightness high

# Breathing red
voltaire apply --mode breathe --color red --speed slow

# Rainbow wave
voltaire apply --mode rainbow --speed normal

# Turn off lighting
voltaire off

# Set performance profile
voltaire profile --set balanced

# Cap battery charge at 80%
voltaire batterylimit --set 80

# Custom fan curve (8-point, temp:pwm pairs — both fans)
# The kernel drops custom curves on every power profile change; run the daemon
# and it re-applies yours automatically.
voltaire fancurve --set "48:2,53:22,57:30,60:43,63:56,65:68,70:89,76:102"

# Set TDP to 50W
voltaire tdp --set 50

# Undervolt CPU by -20 (Curve Optimizer, requires ryzen_smu)
voltaire undervolt --set -20

# A different profile on AC and on battery.
# --profile stores a setting without applying it, so you can build the battery
# profile while still plugged in.
voltaire profile --create battery-uv
voltaire tdp --set 35 --profile battery-uv
voltaire undervolt --set -25 --profile battery-uv
voltaire autoswitch --ac balanced --battery battery-uv
```

## Documentation

Full documentation at **<https://dahui.github.io/voltaire>**

- [Installation](https://dahui.github.io/voltaire/installation/)
- [Quick Start](https://dahui.github.io/voltaire/quick-start/)
- [Migrating from z13ctl](https://dahui.github.io/voltaire/migrating-from-z13ctl/)
- [The GUI](https://dahui.github.io/voltaire/gui/)
- [Commands](https://dahui.github.io/voltaire/reference/commands/)
- [Daemon](https://dahui.github.io/voltaire/reference/daemon/)
- [API](https://dahui.github.io/voltaire/reference/api/)
- [Contributing](https://dahui.github.io/voltaire/contributing/)

