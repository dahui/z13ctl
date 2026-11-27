# `make install` must not pair a fresh daemon with a stale drawer

**`make install` must not pair a fresh daemon with a stale drawer.** `make
build` builds only the CLI, so `make build && sudo make install` installed
whatever `voltaire-gui/voltaire-gui` happened to be lying in the tree — the
CLI/GUI version skew that shipping the two as one package is meant to make
structurally impossible, reintroduced by the one install path that predates
the merge. `make build-all` is the pair; `install` compares the ldflags
version string embedded in the GUI binary against `$(VERSION)` and warns.
It reads the string with `grep -aqF` rather than running the binary, because
the recipe runs as root and `voltaire-gui`'s first statement is
`theme.MigrateFromZ13gui()`. It also re-runs `setcap cap_bpf,cap_perfmon+ep`
on the GUI every time (2026-11-15): `install(1)` replaces the file and file
capabilities do not survive that, so a source install lost the gamepad
blocker on every reinstall and fell back to SIGSTOP silently.
