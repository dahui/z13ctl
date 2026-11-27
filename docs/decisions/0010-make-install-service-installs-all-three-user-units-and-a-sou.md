# `make install-service` installs all three user units, and a source install is the only path where forgetting one is invisible

**`make install-service` installs all three user units, and a source install
is the only path where forgetting one is invisible.** `voltaire-gui.service`
has been packaged in `contrib/systemd/user/` and enabled by every
distribution package since 2.0, but no make target installed it — so on a
source install `systemctl --user restart voltaire-gui` answered "unit not
found" while the drawer *was* running, started by the pre-2.0
`z13gui.service` the target also never disabled. The compatibility symlinks
are what make that confusing rather than merely broken: `/usr/local/bin`
precedes `/usr/bin`, so `ExecStart=z13gui` in the old unit resolves through
`z13gui` → `voltaire-gui` and runs the *new* binary under the *old* unit
name. A unit name stops being evidence of which binary is running the moment
those symlinks exist. `LEGACY_USER_UNITS` is therefore one list covering both
the daemon and the GUI, and `install-service` and `uninstall-service` are
each written against the full set.
