# Contributing

Contributions are welcome. Please open an issue before starting work on a
significant change so the approach can be discussed first.

---

## Repository structure

This repo contains two Go modules:

| Module | Path | Purpose |
|--------|------|---------|
| `github.com/dahui/z13ctl` | `.` | Main CLI and daemon binary |
| `github.com/dahui/z13ctl/api` | `./api` | Public client library for external tools |

The `api/` module is stdlib-only so that GUI tools, Decky plugins, and other
integrations can import it without pulling in the CLI's dependencies.

---

## Development setup

```sh
git clone https://github.com/dahui/z13ctl
cd z13ctl
go mod download
cd api && go mod download && cd ..
```

To work on both modules together in your IDE or when making changes to `api/`,
create a `go.work` file (it is gitignored):

```sh
go work init . ./api
```

---

## Before submitting a pull request

```sh
make test         # run all tests
go test -race ./...  # required for changes touching the daemon
make lint         # run golangci-lint
make mod-tidy     # tidy go.mod for both modules
```

Tests do not require hardware. Pull requests must pass `make test` and
`make lint` without errors and should include tests for any new behavior.
Run `go test -race ./...` for anything touching `internal/daemon` or `api` —
both hold concurrency invariants that only the race detector enforces.

---

## Testing notes

- `internal/aura` — fully unit-testable via mock writers; covers every packet type
- `internal/cli` — a fake sysfs tree (`sysfs_fake_test.go`) backs the hwmon,
  platform-profile, PPT, battery, firmware-attribute, and `ryzen_smu` helpers;
  also covers color parsing and dry-run output
- `internal/hid` — tests cover sysfs parsing; writes are tested via pipe-backed
  mock devices
- `internal/daemon` — state persistence, `cloneState`, the `saveState` race
  regression, and request validation/dispatch. Handlers that reach hardware are
  deliberately not exercised (see below); the button watcher needs an evdev mock
- `api` — socket client tested against a stub daemon, including the read-deadline
  and subscriber-goroutine-leak regressions
- `cmd/` — the generated-vs-packaged permission artifact drift guard

!!! warning "Tests must never touch real hardware"
    `internal/cli` writes straight to sysfs and `SetProfile` shells out to
    `powerprofilesctl`. Two seams exist so tests cannot reach either: the fake
    sysfs tree redirects every path, and `ppdRunner` / `smuReadFile` /
    `smuWriteFile` are swappable. Both were added after tests changed the
    developer's live power profile and TDP.

    `internal/daemon` handlers call `internal/cli` directly, and its path vars
    are unexported, so **daemon tests must stay on validation paths that return
    before any hardware access**. A daemon test that gets past `handleTDP`
    validation will rewrite the machine's actual power limits.

---

## Release workflow (maintainers only)

The `api/` module must be tagged before the main module so the main module
can reference a real published version:

```sh
git tag api/v0.x.y && git push origin api/v0.x.y  # tag api/ first
git tag v0.x.y     && git push origin v0.x.y       # then tag main module
```

GoReleaser handles binary builds and GitHub Release creation automatically
when the main module tag is pushed.
