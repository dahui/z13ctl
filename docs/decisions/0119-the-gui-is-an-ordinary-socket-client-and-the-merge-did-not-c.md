# The GUI is an ordinary socket client, and the merge did not change that

**The GUI is an ordinary socket client, and the merge did not change that.**
`voltaire-gui` reaches the daemon only through `api/` — the same public module
a Decky plugin or a third-party tool uses — so nothing in `internal/daemon`
may grow a GUI-shaped shortcut. The two binaries share exactly two things:
`internal/version.Version` and the `api` contract. That is what keeps the CLI
free of cgo and lets the daemon be tested without GTK.
