---
title: Migrating from z13ctl
description: What the Voltaire 2.0 rename means for z13ctl and z13gui users — what carries over automatically, what changed, and the compatibility timeline.
---

**Voltaire 2.0 is z13ctl 2.0.** The z13ctl CLI/daemon and the z13gui overlay
merged into one project and took one name. Nothing about the hardware support
changed, the same people maintain it, and the version numbers continue where
z13ctl's left off — 2.0 follows 1.3.

Upgrading is designed to be uneventful: install the voltaire packages, and
your settings, profiles, and running clients carry over. This page says
exactly what happens, so you can verify it rather than trust it.

## The short version

| | Was | Is now |
|---|---|---|
| CLI / daemon | `z13ctl` | `voltaire` |
| GUI | `z13gui` | `voltaire-gui` |
| Repository | `github.com/dahui/z13ctl` + `github.com/dahui/z13gui` | [`github.com/dahui/voltaire`](https://github.com/dahui/voltaire) |
| AUR packages | `z13ctl-bin`, `z13gui-bin` | `voltaire-bin`, `voltaire-gui-bin` |
| Daemon socket | `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock` | `…/voltaire/voltaire.sock` (old path still served) |
| Daemon state | `~/.local/state/z13ctl/state.json` | `…/voltaire/state.json` (copied on first run) |
| GUI config | `~/.config/z13gui/` | `~/.config/voltaire/` (copied on first run) |
| systemd units | `z13ctl.socket/.service`, `z13ctl-perms.service`, `z13gui.service` | `voltaire.*`, `voltaire-perms.service`, `voltaire-gui.service` |
| Go module | `github.com/dahui/z13ctl/api` | `github.com/dahui/voltaire/api/v2` |

## Upgrading

### Arch Linux (AUR)

Install the new packages; they declare `conflicts`/`replaces` on the old
ones, so pacman removes `z13ctl-bin`/`z13gui-bin` in the same transaction:

```sh
yay -S voltaire-bin voltaire-gui-bin
```

### Debian / Ubuntu and Fedora / RHEL

Install the new `.deb`/`.rpm` packages from the
[Releases](https://github.com/dahui/voltaire/releases) page. They declare
`Replaces`/`Obsoletes` on the old package names, and their install scripts
disable the old systemd units:

```sh
sudo apt install ./voltaire_*.deb ./voltaire-gui_*.deb     # Debian/Ubuntu
sudo dnf install ./voltaire_*.rpm ./voltaire-gui_*.rpm     # Fedora
```

### Tarball / manual installs

Install the new binaries and run setup — it cleans up after the old install
itself:

```sh
sudo voltaire setup
```

That writes `99-voltaire.rules` and `voltaire-perms.service`, removes the old
`99-z13ctl.rules` (after verifying z13ctl generated it — a hand-edited rules
file is left alone with a note), and runs `disable --now` on the old
`z13ctl-perms.service`. Then swap the user units:

```sh
systemctl --user disable --now z13ctl.socket z13ctl.service z13gui.service
rm -f ~/.config/systemd/user/z13ctl.socket \
      ~/.config/systemd/user/z13ctl.service \
      ~/.config/systemd/user/z13gui.service
```

and install the voltaire units as described in
[Installation](/voltaire/installation/).

## What carries over automatically

**Daemon state — every saved setting.** On first run, the daemon copies
`~/.local/state/z13ctl/state.json` to `~/.local/state/voltaire/state.json`:
lighting, profile, battery limit, custom profiles, autoswitch configuration —
all of it. The copy is byte-for-byte and only happens while the voltaire file
does not exist yet.

**GUI config and themes.** On first run, voltaire-gui copies everything in
`~/.config/z13gui/` (config.toml, theme.toml, theme.css) to
`~/.config/voltaire/`, under the same only-if-absent rule.

**Both copies leave the originals untouched.** The old files staying where
they are is deliberate: it is what makes a downgrade to 1.x safe at any point
in the 2.x line — the old version finds its state exactly where it left it.
(Note that a 1.x daemon does not know about *named* custom profiles; if you
downgrade, the active profile's settings survive but other named profiles do
not carry back.)

**Clients keep working.** The daemon listens on the old z13ctl socket path as
well as the new one, and both answer identically — a Decky plugin, script, or
any other client pointed at `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock` continues
working without changes. The Go api client dials voltaire-then-z13ctl, so it
also reaches an old daemon that has not restarted since the upgrade.

**Environment variables.** `Z13GUI_SCALE` and `Z13GUI_NO_GAMEPAD` are still
honoured when the new `VOLTAIRE_GUI_*` names are unset.

**Custom theme stylesheets.** A `theme.css` referencing the `@z13-*` color
tokens keeps working — every token is defined under both its `@z13-*` and
`@voltaire-*` names. See [Theming](/voltaire/gui/theming/#color-reference).

## What you should update (eventually)

None of this is urgent — everything below is served through the whole 2.x
line and removed at 3.0:

- Scripts calling `z13ctl …` → call `voltaire …`
- Clients dialing the z13ctl socket path → dial
  `$XDG_RUNTIME_DIR/voltaire/voltaire.sock`
- `Z13GUI_*` environment variables → `VOLTAIRE_GUI_*`
- `@z13-*` tokens in a hand-written `theme.css` → `@voltaire-*`

## For developers using the Go API

The module moved and gained the v2 semantic-import suffix:

```go
import "github.com/dahui/voltaire/api/v2"   // package name is still api
```

The old module path, `github.com/dahui/z13ctl/api`, is **frozen at its final
1.x release** but keeps resolving indefinitely — the repository rename leaves
a redirect in place, and the Go module proxy has the tags cached. Existing
programs keep building; they just never see 2.x features.

New in the v2 api, relevant to migration:

- `api.SocketPaths()` returns the canonical and legacy socket paths in dial
  order; `SocketPath()` returns the canonical one.
- `api.ErrUnknownCommand` distinguishes "this daemon predates the command"
  from a real failure: `errors.Is(err, api.ErrUnknownCommand)`. Probe with
  it — never gate on a version number.

The 1.x tags (`v1.*`, `api/v1.*`) are permanent. If you pin one, nothing
changes for you.

## Why the rename?

The project outgrew its name twice over: it is no longer one tool (the CLI
and the GUI ship and version together, from one repository), and it is no
longer only for the Z13 — 2.0's driver architecture selects hardware support
from device data, with the 2025 ROG Flow Z13 as the first supported machine.
A name that hardcoded both had to go.

The old repositories redirect to
[`github.com/dahui/voltaire`](https://github.com/dahui/voltaire), issues and
stars included. The z13gui repository is archived with its history merged
here, so its commits are all part of this repository's history.

## Compatibility timeline

| Shim | Served | Removed |
|---|---|---|
| Legacy socket path (`z13ctl/z13ctl.sock`) | all of 2.x | 3.0 |
| State-file migration copy | all of 2.x | 3.0 |
| GUI config migration copy | all of 2.x | 3.0 |
| `Z13GUI_*` environment variables | all of 2.x | 3.0 |
| `@z13-*` CSS tokens | all of 2.x | 3.0 |
| Old-unit/rules cleanup in `setup` and packages | all of 2.x | 3.0 |
| `github.com/dahui/z13ctl/api` module path | frozen at 1.x, resolves forever | — |
