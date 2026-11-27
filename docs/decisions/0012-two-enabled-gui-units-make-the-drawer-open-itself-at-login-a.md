# Two enabled GUI units make the drawer open itself at login, and the compatibility symlinks are why

**Two enabled GUI units make the drawer open itself at login, and the
compatibility symlinks are why.** `voltaire-gui` is a GApplication holding
`io.github.dahui.Voltaire`, so a second launch does not start a second
drawer: it forwards `activate` to the running instance and exits, and
`activate` on a running instance is deliberately a *toggle* (that is what
makes the desktop entry work as a show gesture). Leave `z13gui.service`
enabled beside `voltaire-gui.service` and login starts the same binary twice
— `ExecStart=z13gui` resolves through `/usr/local/bin/z13gui` →
`voltaire-gui` — 0.6 ms apart, and the loser toggles the winner open. The
user meets a drawer they never asked for, and nothing in the drawer's own
logic is wrong.
Both halves of the fix are needed and neither substitutes for the other.
`Conflicts=` + `After=` in the shipped units guarantees only *one* process:
measured over ten login transactions it removed the spurious toggle every
time but let **either** unit win, because when both are queued together
systemd resolves the conflict by processing order and the second one
processed stops the first. Making `voltaire-gui` the survivor is the job of
removing the old unit — `--global disable` in the package scripts, the mask
in `install-service`. With the mask in place the same ten-run sweep is
unanimous. Any future unit pair aliased by a compatibility symlink needs the
same treatment.
