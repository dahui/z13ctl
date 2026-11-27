# `systemctl --user disable` cannot undo a `--global` enable, so `install-service` masks what it cannot disable

**`systemctl --user disable` cannot undo a `--global` enable, so
`install-service` masks what it cannot disable.** The pre-2.0 packages enable
their units with `systemctl --global`, which writes root-owned symlinks under
`/etc/systemd/user/*.target.wants`. A `--user disable --now` stops the unit
for the session — so it looks like it worked — but `is-enabled` still reports
`enabled` and the unit returns at the next login. Printing the
`--global disable` line was the first attempt and is not enough: it leaves a
broken login one un-run command away, and the consequence is not subtle (see
the next entry). Masking is the one lever a target running as the user has,
it is user-scope only, `uninstall-service` unmasks, and the note names
`systemctl --user unmask`. A make target run as the user still has no
business writing `/etc` itself.
