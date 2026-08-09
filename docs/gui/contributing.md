# Contributing

Contributions are welcome. Please open an issue before starting work on a
significant change so the approach can be discussed first.

---

## Repository structure

Single Go module (`github.com/dahui/z13gui`).

| Package | Purpose |
|---------|---------|
| `internal/gui` | Main Window type, daemon subscription, gamepad navigation |
| `internal/gui/layershell` | Wayland layer-shell display backend |
| `internal/gui/gamescope` | X11 overlay backend for Steam Gaming Mode |
| `internal/gui/gamepad` | Linux evdev gamepad reader |
| `internal/gui/gamepad/hidblocker` | BPF LSM hidraw blocker (blocks PS/Nintendo controller reads) |
| `internal/gui/fonts` | Embedded Inter font registration |
| `internal/theme` | Color definitions, TOML parsing, CSS generation — pure Go |

Decisions live in pure packages outside `internal/gui`; the GTK files read
widgets, call out, and apply the answer. `internal/gui` cannot be compiled by the
test tool at all, so anything left in there is unverifiable by construction —
including `internal/gui/gamepad`, which needs no CGO but is excluded by path.

---

## Development setup

```sh
git clone https://github.com/dahui/z13gui
cd z13gui
```

**Build dependencies (Arch Linux):**

```sh
sudo pacman -S gtk4 gtk4-layer-shell
```

**Build dependencies (Debian/Ubuntu):**

```sh
sudo apt-get install -y libgtk-4-dev libgtk4-layer-shell-dev
```

**Build dependencies (Fedora):**

```sh
sudo dnf install gtk4-devel gtk4-layer-shell-devel
```

**BPF development (optional — only for modifying the hidraw blocker):**

Requires `clang`, `bpftool`, and kernel BTF support.

```sh
make vmlinux   # generate kernel BTF header
make generate  # compile BPF and generate Go bindings
```

To work against a local copy of the z13ctl API module, create a `go.work` file
(it is gitignored):

```sh
go work init . ../z13ctl/api
```

---

## Before submitting a pull request

```sh
make build     # compile (requires GTK4 headers)
make lint      # run golangci-lint
make test      # run unit tests (pure Go, no GTK4 required)
make race      # the same tests under the race detector
```

Tests live in the pure-Go packages — everything under `internal/` except
`internal/gui` — with no
hardware or GTK4 dependency. GUI packages are integration-tested manually against
hardware.

`make test` derives that list with `go list ./internal/... | grep -v /internal/gui`
rather than using `./...`, since `internal/gui` requires CGO and GTK4 headers. A new
pure package is picked up automatically — there is nothing to register.

Because `internal/gui` cannot be compiled by the test tool at all, prefer putting
logic in a pure package and leaving only widget wiring behind. That is not a style
preference: it is the difference between code that can be verified and code that
cannot.

Pull requests must pass `make build`, `make lint`, and `make test` without errors,
and should include tests for any changes to the pure-Go packages.

---

## Testing notes

- `internal/theme` — fully unit-testable; covers color parsing, CSS generation,
  config persistence, and all 78 built-in theme/accent combinations
- `internal/power` — TDP limits and fan curve rules
- `internal/daemon` — collapsing an api `(handled, err)` pair into one error, plus
  a contract test pinning the api's "daemon not running" convention
- `internal/focusgrid` — gamepad focus navigation index math
- `internal/keyrepeat` — which held direction owns the gamepad auto-repeat
- `internal/colorconv` — hex/HSL/RGB conversion and colour validation
- `internal/lighting` — RGB mode resolution and per-mode controls
- `internal/uiscale` — gamescope UI scale factor
- `internal/startup` — CLI argument scanning and log filtering
- `internal/togglegate` — pure debounce helper for duplicate `gui-toggle` bursts
- `internal/gui` — requires GTK4; integration-tested manually against hardware
- Display backends (layershell, gamescope) — require a compositor or gamescope;
  no automated tests

---

## Release workflow (maintainers only)

```sh
git tag vX.Y.Z && git push origin vX.Y.Z
```

GoReleaser handles binary builds, the `.pkg.tar.zst`, `.deb`, and `.rpm`
packages, AUR publishing, and GitHub Release creation automatically when
the tag is pushed.
