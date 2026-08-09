# Contributing

Contributions are welcome. Please open an issue before starting work on a
significant change.

Full contributing guide: <https://dahui.github.io/voltaire/contributing/>

## Short version

- Run `make test && make lint` before submitting a pull request
- Tests do not require hardware
- The `api/` module must be tagged before the main module on releases

## Module paths and tags (do not "fix" these)

This repository was renamed from `dahui/z13ctl` to `dahui/voltaire` for 2.0.
GitHub's repository redirect is what keeps the old Go module paths and every
old `go get`/issue/release link resolving. **Never create a new repository
named `z13ctl` under this owner** — that would sever the redirect and break
module resolution for every pre-2.0 consumer. The old names live on only as
package keywords and redirects.

Tags are repo-global, so both module histories share one tag namespace. The Go
proxy filters versions per module path using each tag's `go.mod`, which makes
the mixed history safe:

| Tag range | Module path | Status |
|---|---|---|
| `v0.x`–`v1.3.1` | `github.com/dahui/z13ctl` | frozen (pre-rename releases) |
| `api/v1.0.0`–`api/v1.2.0` | `github.com/dahui/z13ctl/api` | frozen — do not delete; old importers resolve these through the redirect |
| `v2.0.0+` | `github.com/dahui/voltaire/v2` | current main module |
| `api/v2.0.0+` | `github.com/dahui/voltaire/api/v2` | current api module |

The `/v2` suffixes are Go semantic-import-versioning: a module tagged v2+
must carry the suffix in its path. The suffix is virtual — the code lives in
`./` and `api/` as before.
