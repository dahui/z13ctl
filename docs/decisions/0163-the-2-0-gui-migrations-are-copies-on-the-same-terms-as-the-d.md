# The 2.0 GUI migrations are copies, on the same terms as the daemon's

**The 2.0 GUI migrations are copies, on the same terms as the daemon's.**
`theme.MigrateFromZ13gui` copies `~/.config/z13gui/*` to
`~/.config/voltaire/*` **only while the voltaire directory does not exist at
all**, and never moves: the old directory staying intact is what makes a
downgrade to 1.x safe for the whole 2.x line, and the existence check means it
can never overwrite something saved since. Only top-level regular files are
copied — everything the 1.x GUI ever wrote. It must run **before anything reads
config**, which is why it is the first statement in `main`, ahead of
`--print-theme` and any GTK call.
