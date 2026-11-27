---
title: Upgrading from z13ctl
description: Upgrading from z13ctl and z13gui to voltaire 2.0, what carries over automatically, and the compatibility timeline.
---

# Upgrading from z13ctl

voltaire 2.0 is the next release of **z13ctl** and **z13gui**. The two were
combined into one project and renamed, because they now ship together and
support more machines than the ROG Flow Z13. Version numbers carry on: 2.0
follows z13ctl 1.4.

Upgrading is meant to be uneventful: install voltaire, and your settings,
profiles and scripts carry over.

## What changes

| | z13ctl / z13gui | voltaire |
|---|---|---|
| Command-line tool and daemon | `z13ctl` | `voltaire` |
| GUI | `z13gui` | `voltaire-gui`, in the same package |
| AUR packages | `z13ctl-bin`, `z13gui-bin` | `voltaire-bin` (one package) |
| Repository | `github.com/dahui/z13ctl`, `github.com/dahui/z13gui` | [`github.com/dahui/voltaire`](https://github.com/dahui/voltaire) |
| Settings | `~/.local/state/z13ctl/`, `~/.config/z13gui/` | `~/.local/state/voltaire/`, `~/.config/voltaire/` |
| Daemon socket | `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock` | `$XDG_RUNTIME_DIR/voltaire/voltaire.sock` |
| systemd units | `z13ctl.socket`, `z13ctl.service`, `z13gui.service`, `z13ctl-perms.service` | `voltaire.socket`, `voltaire.service`, `voltaire-gui.service`, `voltaire-perms.service` |
| GUI environment variables | `Z13GUI_*` | `VOLTAIRE_GUI_*` |

The packages install `z13ctl` and `z13gui` as links to the new commands, so
old scripts, keybindings and cron jobs keep working.

## What carries over automatically

- **Your settings.** The first time it starts, the daemon copies your z13ctl
  state (lighting, profile, battery limit, custom profiles, autoswitch) to
  voltaire's folder, and the GUI copies its config and themes. The copy only
  happens while voltaire has no settings of its own yet, and the old files are
  left where they are.
- **Programs that talk to the daemon.** The daemon answers on the old z13ctl
  socket as well as the new one, so a Decky plugin or script pointed at the
  old path keeps working.
- **Scripts using the old commands.** `z13ctl` and `z13gui` still work,
  printing a one-line reminder on stderr. Their output and exit codes are
  unchanged, so scripts that parse them are unaffected.
- **Old environment variables.** `Z13GUI_SCALE` and `Z13GUI_NO_GAMEPAD` still
  work when the `VOLTAIRE_GUI_*` names are not set.
- **Custom themes.** A `theme.css` using the `@z13-*` color names keeps
  working. See [Themes](/guide/themes).
- **Permissions.** `voltaire setup` replaces z13ctl's udev rules and boot
  service with its own, and removes the old ones (a rules file you edited by
  hand is left alone, with a note).

## Upgrade steps

### Arch Linux and CachyOS (AUR)

```sh
yay -S voltaire-bin
```

pacman removes `z13ctl-bin` and `z13gui-bin` in the same step, and the
package runs setup and swaps the old services for the new ones.

### Debian, Ubuntu and Fedora

Install the new package from the
[Releases](https://github.com/dahui/voltaire/releases) page. It replaces both
old packages, runs setup and swaps the services:

```sh
sudo apt install ./voltaire_*.deb     # Debian, Ubuntu
sudo dnf install ./voltaire_*.rpm     # Fedora
```

### Tarball and source installs

Install the new binaries as described in [Installation](/installation), then
run setup, which cleans up the old permissions:

```sh
sudo voltaire setup
```

Stop and remove the old user services:

```sh
systemctl --user disable --now z13ctl.socket z13ctl.service z13gui.service
rm -f ~/.config/systemd/user/z13ctl.socket \
      ~/.config/systemd/user/z13ctl.service \
      ~/.config/systemd/user/z13gui.service
```

Then install the voltaire services ([Installation](/installation#release-tarball)).
From a source checkout, `make install-service` does both.

If `make install-service` says it **masked** an old unit, an old z13ctl or
z13gui package is still installed. Uninstall it; masking only keeps its
services from starting alongside voltaire's (two GUIs at login would open the
drawer by themselves). `make uninstall-service` or
`systemctl --user unmask` undoes the mask.

### A "z13ctl.service not found" message after upgrading

If you once enabled the old units for your own user, systemd may log
`Unit z13ctl.service not found` at your first login after the upgrade. The
voltaire daemon removes those leftover links when it first starts, so the
message does not come back. To clear them yourself:

```sh
systemctl --user disable z13ctl.socket z13ctl.service z13gui.service
```

A "does not exist" error from that command is expected; the links are removed
anyway.

## Going back to z13ctl

Because the old settings files are never changed, you can reinstall z13ctl 1.x
at any point and it finds its settings as it left them. Custom profiles other
than the active one do not carry back, since z13ctl 1.x has only one.

## Update when convenient

Nothing here is urgent; all of it keeps working through the 2.x releases:

- Scripts calling `z13ctl` → call `voltaire`.
- Programs using the z13ctl socket path → use
  `$XDG_RUNTIME_DIR/voltaire/voltaire.sock`.
- `Z13GUI_*` environment variables → `VOLTAIRE_GUI_*`.
- `@z13-*` names in a hand-written `theme.css` → `@voltaire-*`.

Developers using the Go client library: see the [Go API guide](/dev/go-api).

## Compatibility timeline

| Compatibility feature | Works in | Removed in |
|---|---|---|
| `z13ctl` and `z13gui` commands | all of 2.x | 3.0 |
| The old z13ctl socket path | all of 2.x | 3.0 |
| Copying z13ctl and z13gui settings on first run | all of 2.x | 3.0 |
| `Z13GUI_*` environment variables | all of 2.x | 3.0 |
| `@z13-*` theme color names | all of 2.x | 3.0 |
| Cleanup of old services and rules by setup and the packages | all of 2.x | 3.0 |
