# Installation

## Prerequisites

- Linux kernel with `hidraw` support (standard on all mainstream distributions)
- 2025 ASUS ROG Flow Z13 (USB IDs `0b05:18c6` and `0b05:1a30`)

---

## Install

=== "Release binary"

    Download the latest `linux_amd64` archive from the
    [Releases](https://github.com/dahui/z13ctl/releases) page, then extract
    and install:

    ```sh
    tar xzf z13ctl_*_linux_amd64.tar.gz
    sudo install -Dm755 z13ctl /usr/local/bin/z13ctl
    ```

    **One-time permissions setup** (requires root):

    ```sh
    sudo z13ctl setup
    ```

    Then log out and back in (or run `newgrp users`) for the group membership
    to take effect in your current session.

    **Install the systemd user service** (socket activation):

    ```sh
    install -Dm644 contrib/systemd/user/z13ctl.socket \
        ~/.config/systemd/user/z13ctl.socket
    install -Dm644 contrib/systemd/user/z13ctl.service \
        ~/.config/systemd/user/z13ctl.service
    systemctl --user daemon-reload
    systemctl --user enable --now z13ctl.socket z13ctl.service
    ```

=== "Arch Linux (AUR)"

    Install the [z13ctl-bin](https://aur.archlinux.org/packages/z13ctl-bin)
    package with your preferred AUR helper:

    ```sh
    yay -S z13ctl-bin
    ```

    The package installs the binary, udev rules, systemd units, and the
    battery permissions service automatically. After installing, add your user
    to the `users` group if not already a member:

    ```sh
    sudo usermod -aG users $USER
    ```

    Then log out and back in for the group membership to take effect.

    Alternatively, download the `.pkg.tar.zst` package directly from the
    [Releases](https://github.com/dahui/z13ctl/releases) page and install with
    pacman:

    ```sh
    sudo pacman -U z13ctl-*.pkg.tar.zst
    ```

=== "Homebrew (Linuxbrew)"

    ```sh
    brew install dahui/z13ctl/z13ctl
    ```

    Homebrew installs only the binary. You still need to run the one-time
    permissions setup:

    ```sh
    sudo z13ctl setup
    ```

    Then log out and back in (or run `newgrp users`) for the group membership
    to take effect in your current session.

    **Install the systemd user service** (socket activation):

    Download the systemd unit files from the
    [latest release](https://github.com/dahui/z13ctl/releases) archive, then:

    ```sh
    install -Dm644 contrib/systemd/user/z13ctl.socket \
        ~/.config/systemd/user/z13ctl.socket
    install -Dm644 contrib/systemd/user/z13ctl.service \
        ~/.config/systemd/user/z13ctl.service
    systemctl --user daemon-reload
    systemctl --user enable --now z13ctl.socket z13ctl.service
    ```

=== "From source"

    Requires Go 1.23 or later.

    ```sh
    git clone https://github.com/dahui/z13ctl
    cd z13ctl
    make build
    sudo make install
    ```

    `make install` installs the binary to `/usr/local/bin` and runs
    `sudo z13ctl setup` automatically.

    Install the systemd user service:

    ```sh
    make install-service
    ```

---

## Verify the installation

```sh
z13ctl list
```

This should print the discovered hidraw devices and confirm Aura support.
If it prints nothing, see [Troubleshooting](#troubleshooting) below.

---

## What `setup` does

`sudo z13ctl setup` performs four steps:

1. Writes `/etc/udev/rules.d/99-z13ctl.rules` — grants the `users` group
   `MODE=0660` / `GROUP=users` on the ASUS HID and input device nodes; uses
   `RUN+=chgrp/chmod` to set permissions on the platform-profile attribute
   when the driver loads.
2. Reloads udev and applies permissions immediately to all currently present files.
3. Writes `/etc/systemd/system/z13ctl-perms.service` and enables it — a
   `Type=oneshot` service that runs `chgrp` + `chmod g+w` on
   `BAT*/charge_control_end_threshold` at boot.
4. Starts the service immediately so battery limit is accessible right away.

!!! note "Why a separate oneshot service for battery?"
    The `charge_control_end_threshold` sysfs attribute is added by the
    `asus_nb_wmi` kernel driver late in its `probe()` sequence — after all
    observable udev child-device events have already fired. There is no udev
    hook that can reliably target it. The `z13ctl-perms.service` unit is a
    self-contained workaround that runs at `sysinit.target`. It has no
    dependency on the z13ctl binary and can be inspected at any time:

    ```sh
    systemctl cat z13ctl-perms.service
    ```

Use `--dry-run` to preview all changes without root or any side effects:

```sh
z13ctl --dry-run setup
```

---

## Uninstall

Remove the user service first:

```sh
systemctl --user disable --now z13ctl.socket z13ctl.service
rm -f ~/.config/systemd/user/z13ctl.socket \
      ~/.config/systemd/user/z13ctl.service
systemctl --user daemon-reload
```

Then remove the binary and system files:

```sh
sudo rm /usr/local/bin/z13ctl
sudo rm /etc/udev/rules.d/99-z13ctl.rules
sudo udevadm control --reload-rules
sudo systemctl disable --now z13ctl-perms.service
sudo rm /etc/systemd/system/z13ctl-perms.service
sudo systemctl daemon-reload
```

---

## Troubleshooting

**`z13ctl list` prints nothing**

The device was not found. Check that the kernel loaded the `hid` and `hidraw`
modules:

```sh
lsmod | grep hid
ls /dev/hidraw*
```

If `/dev/hidraw*` devices exist but none match the ASUS USB IDs, the device
is present but not recognized. This tool targets the 2025 ROG Flow Z13
specifically (USB IDs `0b05:18c6` and `0b05:1a30`).

**Permission denied**

Run `sudo z13ctl setup` and log out/in to apply the group membership.
Verify with:

```sh
ls -la /dev/hidraw*
groups
```

**Daemon not starting**

```sh
systemctl --user status z13ctl.socket z13ctl.service
journalctl --user -u z13ctl -n 50
```
