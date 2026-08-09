---
title: Contributing
description: Development setup, testing rules, and the release workflow for the voltaire repository.
---

Contributions are welcome. Please open an issue before starting work on a
significant change so the approach can be discussed first.

## Repository structure

This repo produces two binaries from two Go modules:

| Module | Path | Purpose |
|--------|------|---------|
| `github.com/dahui/voltaire/v2` | `.` | The `voltaire` CLI/daemon and the `voltaire-gui` overlay |
| `github.com/dahui/voltaire/api/v2` | `./api` | Public client library for external tools |

The `api/` module is stdlib-only so that GUI tools, Decky plugins, and other
integrations can import it without pulling in the CLI's dependencies.

`voltaire` builds with `CGO_ENABLED=0`; `voltaire-gui` (the `voltaire-gui/`
main package plus everything under `internal/gui`) needs CGO and the GTK4
headers. Every rule worth testing on the GUI side therefore lives in a pure-Go
package — `internal/limits`, `lighting`, `theme`, `colorconv`, `focusgrid`,
`keyrepeat`, `panelgeom`, `uiscale`, `togglegate`, `startup`, `apiresult` —
and the GTK files are thin adaptors that read widgets, call out, and apply the
answer. That is not a style preference: `internal/gui` is excluded from the
test run entirely, so logic left in there is unverifiable by construction.

## Development setup

```sh
git clone https://github.com/dahui/voltaire
cd voltaire
go mod download
cd api && go mod download && cd ..
```

To work on both modules together in your IDE or when making changes to
`api/`, create a `go.work` file (it is gitignored):

```sh
go work init . ./api
```

Building the GUI additionally needs the GTK4 development headers:

```sh
# Arch Linux
sudo pacman -S gtk4 gtk4-layer-shell

# Debian / Ubuntu
sudo apt-get install -y libgtk-4-dev libgtk4-layer-shell-dev

# Fedora
sudo dnf install gtk4-devel gtk4-layer-shell-devel
```

**BPF development (optional — only for modifying the gamepad hidraw
blocker):** requires `clang`, `bpftool`, and kernel BTF support.

```sh
make vmlinux   # generate kernel BTF header
make generate  # compile BPF and generate Go bindings
```

## Before submitting a pull request

```sh
make test         # hermetic tests, both modules (no GTK, no hardware)
make race         # the same set under the race detector
make lint         # gofmt check + golangci-lint over the full tree
make build        # compile voltaire
make build-gui    # compile voltaire-gui (requires GTK4 headers)
make mod-tidy     # tidy go.mod for both modules
```

Tests do not require hardware or GTK. Pull requests must pass `make test` and
`make lint` without errors and should include tests for any new behavior. Run
`make race` for anything touching `internal/daemon` or `api` — both hold
concurrency invariants that only the race detector enforces.

`make test` derives its package list from `go list ./...` minus
`internal/gui` and `voltaire-gui`, so a new pure package is picked up
automatically — there is nothing to register.

## Testing notes

- `internal/aura` — fully unit-testable via mock writers; covers every packet
  type
- `internal/cli` — a fake sysfs tree (`sysfs_fake_test.go`) backs the hwmon,
  platform-profile, PPT, battery, firmware-attribute, and `ryzen_smu`
  helpers; also covers color parsing and dry-run output
- `internal/hid` — tests cover sysfs parsing; writes are tested via
  pipe-backed mock devices
- `internal/daemon` — state persistence, `cloneState`, the `saveState` race
  regression, and request validation/dispatch. Handlers that reach hardware
  are deliberately not exercised (see below); the button watcher needs an
  evdev mock
- `api` — socket client tested against a stub daemon, including the
  read-deadline and subscriber-goroutine-leak regressions
- `cmd/` — the generated-vs-packaged permission artifact drift guard
- `internal/theme` — color parsing, CSS generation, config persistence, the
  z13gui config migration, and all built-in theme/accent combinations
- `internal/limits` — TDP limits and fan curve rules the drawer enforces
- `internal/apiresult` — collapsing an api `(handled, err)` pair into one
  error, plus a contract test pinning the api's "daemon not running"
  convention
- `internal/focusgrid`, `keyrepeat`, `colorconv`, `lighting`, `uiscale`,
  `startup`, `togglegate`, `panelgeom` — the drawer's extracted decision logic
- `internal/gui` and its display backends — require GTK4 and a compositor;
  integration-tested manually against hardware

:::caution[Tests must never touch real hardware]
`internal/cli` writes straight to sysfs and `SetProfile` shells out to
`powerprofilesctl`. Two seams exist so tests cannot reach either: the fake
sysfs tree redirects every path, and `ppdRunner` / `smuReadFile` /
`smuWriteFile` are swappable. Both were added after tests changed the
developer's live power profile and TDP.

`internal/daemon` handlers call `internal/cli` directly, and its path vars are
unexported, so **daemon tests must stay on validation paths that return before
any hardware access**. A daemon test that gets past `handleTDP` validation
will rewrite the machine's actual power limits.
:::

## Documentation

This site lives in `website/` (Astro Starlight). Preview it locally with:

```sh
make docs
```

The [Go API Reference](/voltaire/reference/api-go/) page is generated by
gomarkdoc — edit the doc comments in `api/*.go` and run `make docs-api` to
regenerate it rather than editing the page.

## Release workflow (maintainers only)

The `api/` module must be tagged before the main module so the main module
can reference a real published version:

```sh
git tag api/v2.x.y && git push origin api/v2.x.y  # tag api/ first
git tag v2.x.y     && git push origin v2.x.y       # then tag main module
```

GoReleaser handles binary builds, the `.pkg.tar.zst`, `.deb`, and `.rpm`
packages for both binaries, AUR publishing, and GitHub Release creation
automatically when the main module tag is pushed.

The pre-rename tags (`v1.x`, `api/v1.x`) are frozen and must never be deleted
or reused — the Go module proxy serves them to pre-rename importers through
the GitHub repository redirect. For the same reason, never create a new
repository named `z13ctl` under the same owner: it would sever the redirect
that keeps old module paths and every historical link resolving.
