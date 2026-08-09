# Installation

## Prerequisites

- Linux kernel (x86_64)
- A Wayland compositor, or gamescope (Steam Gaming Mode). Compositors with
  layer-shell support (KDE Plasma, Hyprland, Sway) get the drawer as a true
  edge-anchored panel; GNOME and any other compositor without it get the overlay
  backend instead, which works but is slightly degraded — see
  [GNOME support](index.md#gnome-support)
- [z13ctl](https://github.com/dahui/z13ctl) installed and daemon running

### Runtime dependencies

z13gui links dynamically against GTK 4 and gtk4-layer-shell. The AUR package
pulls these automatically; for other install methods, install them via your
system package manager first.

| Dependency | Arch | Debian / Ubuntu | Fedora |
|---|---|---|---|
| GTK 4 | `gtk4` | `libgtk-4-1` | `gtk4` |
| gtk4-layer-shell | `gtk4-layer-shell` | `libgtk4-layer-shell0` | `gtk4-layer-shell` |

---

## Install

=== "Release binary"

    Install the [runtime dependencies](#runtime-dependencies) for your distro,
    then download the latest `linux_amd64` archive from the
    [Releases](https://github.com/dahui/z13gui/releases) page:

    ```sh
    tar xzf z13gui_*_linux_amd64.tar.gz
    sudo install -Dm755 z13gui /usr/local/bin/z13gui
    ```

    Install the systemd user service:

    ```sh
    install -Dm644 contrib/z13gui.service \
        ~/.config/systemd/user/z13gui.service
    systemctl --user daemon-reload
    systemctl --user enable --now z13gui
    ```

    Optionally install the desktop entry:

    ```sh
    install -Dm644 contrib/z13gui.desktop \
        ~/.local/share/applications/z13gui.desktop
    ```

=== "Arch Linux (AUR)"

    Install the [z13gui-bin](https://aur.archlinux.org/packages/z13gui-bin)
    package with your preferred AUR helper:

    ```sh
    yay -S z13gui-bin
    ```

    The package installs the binary, systemd service, udev rules, and desktop
    entry. Services are enabled automatically for all users on next login.

    Alternatively, download the `.pkg.tar.zst` package directly from the
    [Releases](https://github.com/dahui/z13gui/releases) page and install with
    pacman:

    ```sh
    sudo pacman -U z13gui-*.pkg.tar.zst
    ```

=== "Debian / Ubuntu"

    Download the `.deb` package from the
    [Releases](https://github.com/dahui/z13gui/releases) page, then install:

    ```sh
    sudo apt install ./z13gui_*.deb
    ```

    The package installs the binary, systemd service, udev rules, and desktop
    entry. After installing, enable the service:

    ```sh
    systemctl --user enable --now z13gui
    ```

=== "Fedora / RHEL"

    Download the `.rpm` package from the
    [Releases](https://github.com/dahui/z13gui/releases) page, then install:

    ```sh
    sudo dnf install ./z13gui_*.rpm
    ```

    The package installs the binary, systemd service, udev rules, and desktop
    entry. After installing, enable the service:

    ```sh
    systemctl --user enable --now z13gui
    ```

=== "From source"

    Requires Go 1.25+, CGO enabled, and GTK4 development libraries.

    **Arch Linux:**

    ```sh
    sudo pacman -S gtk4 gtk4-layer-shell
    ```

    **Debian/Ubuntu:**

    ```sh
    sudo apt-get install -y libgtk-4-dev libgtk4-layer-shell-dev
    ```

    **Fedora:**

    ```sh
    sudo dnf install gtk4-devel gtk4-layer-shell-devel
    ```

    Then clone and build:

    ```sh
    git clone https://github.com/dahui/z13gui
    cd z13gui
    make build
    sudo make install
    make install-service
    ```

---

## Gamepad input blocking (capabilities)

In Steam Gaming Mode (gamescope), z13gui suppresses controller input while the
drawer is open so button presses navigate the overlay instead of the game.

z13gui supports two blocking methods and selects the best one automatically:

| Method | Requires | Behaviour | Side effects |
|--------|----------|-----------|--------------|
| **BPF blocker** (preferred) | `CAP_BPF` + `CAP_PERFMON` on the binary | Blocks PS / Nintendo controller reads at the kernel level via a BPF LSM hook | None — Steam and the game keep running normally |
| **SIGSTOP fallback** | No extra capabilities | Pauses the Steam process with SIGSTOP / SIGCONT | Game also pauses; PipeWire frame delivery stops |

The AUR, `.deb`, and `.rpm` packages grant the required capabilities
automatically during installation. If you installed from source or from the
release binary, grant them manually:

```sh
sudo setcap cap_bpf,cap_perfmon+ep /usr/local/bin/z13gui
```

??? note "What are these capabilities and are they safe?"

    ### Short version

    These two capabilities let z13gui load a tiny kernel filter that tells
    the system "when Steam tries to read a PS or Nintendo controller, return
    a temporary 'try again later' error instead." That's all it does. It
    cannot access your files, network, or any other part of the system. The
    filter is automatically removed when z13gui exits.

    ### Technical details

    **CAP_BPF** allows loading BPF programs into the kernel. z13gui uses
    this to attach a single LSM (`lsm/file_permission`) hook that
    intercepts `read()` calls on hidraw character devices
    (`/dev/hidraw*`). The hook checks whether the calling PID is in a
    small allow-list map and the target device is a hidraw device
    (major 244). If both conditions match, it returns `-EAGAIN`; otherwise
    it returns `0` (allow).

    **CAP_PERFMON** is required by the kernel to attach BPF LSM programs.
    z13gui does not use performance monitoring — this capability is a
    kernel-imposed prerequisite for LSM attachment.

    **What the BPF program can do:**

    - Return `-EAGAIN` for `read()` calls on hidraw devices by specific PIDs
    - Nothing else — the program is verified by the kernel's BPF verifier
      before loading and cannot be modified at runtime

    **What the BPF program cannot do:**

    - Access files, network, or memory outside its own BPF maps
    - Survive a process exit — all BPF resources are released when z13gui
      stops, crashes, or is killed
    - Affect any process not explicitly added to its PID map
    - Block any operation other than `read()` on hidraw devices

    **Compared to running as root:** file capabilities grant only the two
    listed privileges to the z13gui binary. The process runs as your normal
    user with no other elevated access. This is strictly safer than running
    with `sudo` or as root.

---

## Verify the installation

```sh
z13gui --version
```

Then press the Armoury Crate button on your Z13. The drawer should slide in
from the right edge of the screen.

---

## Uninstall

Stop and remove the service:

```sh
make uninstall-service
```

Or manually:

```sh
systemctl --user disable --now z13gui
rm -f ~/.config/systemd/user/z13gui.service
systemctl --user daemon-reload
```

Remove the binary:

```sh
sudo rm /usr/local/bin/z13gui
```

---

## Troubleshooting

**Drawer doesn't appear**

Make sure the z13ctl daemon is running:

```sh
systemctl --user status z13ctl.service
```

Then check the log — see the note below on `--user`, which is required.

**Which display backend am I on?**

The startup log names it. Run z13gui from a terminal and read the first lines:

```sh
z13gui --debug
```

Look for `backend mode=layer-shell`, `mode=overlay` or `mode=gamescope`. On
GNOME, `mode=overlay` is expected and correct: Mutter does not implement the
`zwlr_layer_shell_v1` protocol, so the drawer is drawn as a transparent
click-through overlay rather than an edge-anchored panel. See
[GNOME support](index.md#gnome-support) for what that changes.

**The drawer is a small box in the middle of the screen**

This affects v1.3.0 and earlier on GNOME. Those versions called into layer-shell
without checking whether the compositor implements it; every anchoring call
silently did nothing, and since the anchors were the only thing giving the drawer
a height, it collapsed into a small unusable window
([#16](https://github.com/dahui/z13gui/issues/16)). Upgrade to the latest
release, which detects this and uses the overlay backend instead.

**Reading the log: `--user` is required**

z13gui runs as a systemd **user** unit (installed to `/usr/lib/systemd/user/` by
the distro packages), so its output goes to the user journal. Without `--user`,
`journalctl` searches system units, finds no such unit, and prints
`-- No entries --` — which looks like the program never ran:

```sh
journalctl --user -u z13gui -n 50   # correct
sudo journalctl -u z13gui           # WRONG: reads system units, prints nothing
```

**Service fails to start**

Check the journal:

```sh
journalctl --user -u z13gui -n 50
```

Run with debug logging to see GTK and initialization output:

```sh
z13gui --debug
```

**Gamescope: controller input not suppressed while drawer is open**

Grant BPF capabilities so z13gui can block controller input at the kernel level:

```sh
sudo setcap cap_bpf,cap_perfmon+ep /usr/local/bin/z13gui
```

Without capabilities, z13gui falls back to freezing Steam (SIGSTOP), which
also pauses running games.

**Gamescope: drawer doesn't show**

Verify `GAMESCOPE_WAYLAND_DISPLAY` is set and the socket exists:

```sh
echo $GAMESCOPE_WAYLAND_DISPLAY
ls "$XDG_RUNTIME_DIR/$GAMESCOPE_WAYLAND_DISPLAY"
```

If the socket is missing (stale environment from a previous Gaming Mode session),
z13gui automatically falls back to the Wayland path — layer-shell where the
compositor supports it, the overlay backend otherwise.
