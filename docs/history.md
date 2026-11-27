# Pre-merge history

What CLAUDE.md said about the daemon and the GUI before the 2.0 merge, kept
for the detail it still carries (moved out of CLAUDE.md, 2026-11-26).

## The daemon — COMPLETE

The daemon is fully implemented and passing `make build && make test && make lint`.

**Key daemon details:**
- Socket paths: `$XDG_RUNTIME_DIR/voltaire/voltaire.sock` (canonical) + `$XDG_RUNTIME_DIR/z13ctl/z13ctl.sock` (compat, through 2.x)
- State file: `$XDG_STATE_HOME/voltaire/state.json` (migrated by copy from the z13ctl path on first run)
- Protocol: one newline-terminated JSON request → one JSON response; long-lived
  connections for `{"cmd":"subscribe","events":["gui-toggle"]}` (GUI use)
- Button watcher uses `github.com/holoplot/go-evdev`; systemd integration uses
  `github.com/coreos/go-systemd/v22` (activation + daemon packages)
- `ATTR{name}=="asus-wmi"` was removed from the platform-profile udev rule because
  platform-profile class devices do not have a `name` sysfs attribute file. The rule
  now matches `SUBSYSTEM=="platform-profile"` alone.
- `voltaire-perms.service` (system-level oneshot) runs `chmod g+w` on
  `BAT*/charge_control_end_threshold` and firmware-attributes `current_value` files
  after `sysinit.target`. This is necessary because these attributes may be created
  after observable udev events — so no udev `RUN+=` hook can catch them reliably.

**To activate on this machine (must be done once after each `sudo voltaire setup`):**
```sh
sudo voltaire setup                # rewrites rules file; applies sysfs perms immediately
sudo make install-perms-service  # installs battery sysfs permissions service
make install-service             # installs daemon socket + service units
```

## The GUI and the api module — merged into this repo at 2.0

(This section and the one above predate the M0–M6 roadmap and describe the
pre-merge "Phase 1 / Phase 2" split. Kept for the still-current detail; the
table above is the authoritative status.)

The `api/` submodule (now `github.com/dahui/voltaire/api/v2`; the frozen
pre-2.0 releases live at `github.com/dahui/z13ctl/api`) is complete. Its
changes:
- Module path renamed from `z13ctl` to `github.com/dahui/z13ctl`
  (and again to `github.com/dahui/voltaire/v2` for the 2.0 rename — see
  CONTRIBUTING.md for the tag/module-path matrix)
- `api/` submodule created with `State`, `LightingState` types and all `Send*`, `Subscribe` client functions
- `internal/daemon/` refactored to use `api.State`/`api.LightingState`
- `cmd/*.go` updated to call `api.Send*` directly
- Multi-module dev setup: `go.work` (gitignored) + `replace` directive in `go.mod` for pre-publication

The GUI shipped as the separate `github.com/dahui/z13gui` repo through 1.x and
was merged here for 2.0 with its history intact (`--allow-unrelated-histories`,
after a path-rewrite commit on its side). It is `voltaire-gui`: a right-edge
overlay drawer on `gotk4` + `gotk4-layer-shell`, with three display backends
(layer-shell, fullscreen overlay for GNOME, gamescope X11 for Gaming Mode),
gamepad and touch navigation, and the Armoury Crate button reaching it as the
daemon's `gui-toggle` subscribe event. `internal/gui/CLAUDE.md` holds its
GTK-level decisions (its names are pre-merge — see the table at its head).
The old repo is archived at release time with a pointer README; the AUR
`z13gui-bin` package is superseded via `provides`/`replaces`/`conflicts`.

**Multi-module release workflow (v2, post-rename):**
1. Tag `api/v2.0.0` → `git tag api/v2.0.0 && git push origin api/v2.0.0`
2. Update `go.mod`: confirm `require github.com/dahui/voltaire/api/v2 v2.0.0`,
   remove the `replace` directive
3. Tag main module: `git tag v2.0.0 && git push origin v2.0.0`

Tags are repo-global; the old-path tags (`v*` ≤ 1.3.1, `api/v1.*`) are frozen
and must never be deleted — the proxy serves them to pre-rename importers via
the GitHub redirect. Never recreate a repo named `z13ctl` (severs the
redirect). Details in CONTRIBUTING.md.
