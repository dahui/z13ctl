# `make test` and `make lint` must run both modules

**`make test` and `make lint` must run both modules.** `api/` is a separate Go
module, so a bare `go test ./...` / `golangci-lint run ./...` from the root
silently skips it — which is how two lint issues sat unnoticed in a *released*
module. Both targets now `cd api` as a second step.
