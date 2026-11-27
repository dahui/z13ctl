# Daemon systemd integration

**Daemon systemd integration**: `voltaire.socket` uses socket activation (`LISTEN_FDS`).
It carries **two** `ListenStream=` lines — the canonical voltaire path plus the
pre-rename z13ctl path — and the daemon accept-loops over every fd
`activation.Listeners()` hands it; the non-systemd path creates both sockets
itself (legacy-path failure is a warning, canonical failure is fatal). The
client (`api.SocketPaths`) dials canonical-then-legacy, which also covers the
upgrade window where a pre-2.0 daemon still runs on the old path only.
`voltaire.service` uses `Type=notify` (sd_notify READY=1 after HID open + state restore),
`WantedBy=graphical-session.target` (works in both desktop and Steam Gaming Mode).
Logging goes to journald via stdout/stderr.
