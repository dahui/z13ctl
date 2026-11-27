---
title: Contributing
description: Setting up a development checkout of voltaire, building and testing it, adding a device, and the release workflow.
---

# Contributing

Contributions are welcome. Please open an issue before starting a significant
change, so the approach can be discussed first.

## Repository layout

One repository, two Go modules, two binaries:

| Module | Path | Purpose |
|--------|------|---------|
| `github.com/dahui/voltaire/v2` | `.` | The `voltaire` CLI and daemon, and the `voltaire-gui` overlay |
| `github.com/dahui/voltaire/api/v2` | `./api` | The public client library for other tools |

The `api/` module uses only the standard library, so GUIs, Decky plugins and
other integrations can import it without the CLI's dependencies.
[Architecture](/dev/architecture) describes how the pieces fit together.

## Development setup

You need Go 1.26 or newer. Clone the repository and fetch both modules:

```sh
git clone https://github.com/dahui/voltaire
cd voltaire
go mod download
(cd api && go mod download)
```

When changing `api/` and the main module together, create a `go.work` file (it
is gitignored):

```sh
go work init . ./api
```

`voltaire` builds without cgo. `voltaire-gui` needs cgo and the GTK4 and
gtk4-layer-shell development headers:

```sh
sudo pacman -S gtk4 gtk4-layer-shell                         # Arch Linux
sudo apt-get install -y libgtk-4-dev libgtk4-layer-shell-dev   # Debian / Ubuntu
sudo dnf install gtk4-devel gtk4-layer-shell-devel             # Fedora
```

The website needs Node.js and pnpm.

## Building

```sh
make build        # voltaire
make build-gui    # voltaire-gui (needs the GTK4 headers)
make build-all    # both: what `make install` expects
```

To try a build on your machine, install it and restart the units:

```sh
make build-all
sudo make install
systemctl --user restart voltaire.service voltaire-gui.service
```

## Testing and linting

Before opening a pull request:

```sh
make test         # tests for both modules; no hardware and no GTK needed
make race         # the same tests under the race detector
make lint         # gofmt check, then golangci-lint over the whole tree
make docs-build   # if you changed the website
```

Pull requests must pass `make test` and `make lint`, and should include tests
for new behavior. Run `make race` for changes to `internal/daemon` or `api`.

::: warning Tests never touch hardware
A test must never write to real sysfs, a hidraw device or the SMU. Each driver
package tests against a fake sysfs tree, and its test setup points every
writable path at a directory that does not exist, so a test that forgets the
fake fails instead of changing your machine.
:::

### Testing

- `make test` runs every package that builds without cgo. The GTK code in
  `internal/gui` is left out, so any logic worth testing on the GUI side lives
  in a plain Go package that `internal/gui` calls. A new package is picked up
  automatically.
- Drivers under `internal/drivers/asus`, `amd` and `linux` each keep their
  sysfs paths as package variables and build a fake tree in
  `sysfs_fake_test.go`. The `powerprofilesctl` call and the SMU mailbox have
  their own swappable seams. See [Writing a driver](/dev/writing-a-driver).
- Daemon tests that exercise a write path use the in-memory fakes in
  `internal/daemon/fakes_test.go`, which record the order of writes. The real
  device assembly used elsewhere in those tests refuses every write.
- `cmd/docs_test.go` fails when a command or flag is missing from
  [Commands](/reference/commands); `internal/daemon/server_test.go` does the
  same for protocol commands on [Socket protocol](/dev/socket-protocol).
- Changes to power, fan or sleep handling also need a run on real hardware,
  since firmware behavior is invisible to unit tests. Build with
  `make build-hwtest`, which produces a binary that cannot send undervolt
  writes.

### Gamepad blocker (BPF)

Only needed when changing `blocker.bpf.c`; the generated bindings are
committed. You need `clang`, `bpftool` and kernel BTF:

```sh
cd internal/gui/gamepad/hidblocker
bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
go generate .
rm vmlinux.h
```

### Developer instruments

`voltaire-gui` reads two environment variables meant for development, not
for everyday use:

| Variable | Effect |
|----------|--------|
| `VOLTAIRE_GUI_DUMP_FOCUS=1` | With `-d`, logs every controller focus list at startup, so a layout change can be diffed (grep `focus list built`) |
| `VOLTAIRE_GUI_OPEN_FULL` | Opens the full window at startup without the double press. A tab name (`dashboard`, `profiles`, `settings`) opens that tab; `color` opens the color picker |

Stop `voltaire-gui.service` first: a second launch hands over to the running
instance and exits without building anything.

## Adding a device

A device is a TOML file in `internal/device/devices/` describing the machine's
hardware and which drivers serve it. Most new machines need only that file;
new hardware interfaces need a driver.

- [Device files](/dev/device-files) documents every key. A new key fails
  `make test` until it is documented there.
- [Writing a driver](/dev/writing-a-driver) covers the driver contract and
  testing without hardware.
- Every device file needs a compatibility page in `website/docs/devices/` and a
  row in the matrix on `devices/index.md`; `make test` checks both.

## Documentation

The website lives in `website/` and is built with VitePress.

```sh
make docs         # serve it locally with live reload
make docs-build   # build it as CI does, including link and anchor checks
```

Pages are in `website/docs/`, the sidebar in
`website/docs/.vitepress/config.mts`. Write internal links without the base
path and without a trailing slash (`/reference/commands#setup`).

The [Go API Reference](/dev/api-go) page is generated from the doc comments in
`api/*.go`. Edit the comments and run `make docs-api`; never edit the page by
hand.

## Pull requests

- Keep each pull request to one change, with tests.
- Run `make mod-tidy` if you changed dependencies.
- Describe what you tested on hardware, if anything.

## Release workflow (maintainers)

Tag the `api/` module first, so the main module can depend on a published
version:

```sh
git tag api/v2.x.y && git push origin api/v2.x.y
git tag v2.x.y     && git push origin v2.x.y
```

Pushing the main tag runs GoReleaser, which builds both binaries, the one
`.pkg.tar.zst`, `.deb` and `.rpm` package that carries them, and the GitHub
release; the workflow then updates the `voltaire-bin` AUR package.
`make snapshot` builds the same artifacts locally without publishing.

The pre-rename tags (`v1.x`, `api/v1.x`) are frozen and must never be deleted
or reused: the Go module proxy serves them to old importers through the
GitHub repository redirect. For the same reason, never create a new repository
named `z13ctl` under the same owner.
