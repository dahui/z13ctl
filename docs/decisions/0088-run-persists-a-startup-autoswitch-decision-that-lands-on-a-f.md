# `Run()` persists a startup autoswitch decision that lands on a firmware profile

**`Run()` persists a startup autoswitch decision that lands on a firmware
profile.** A custom target is saved by `applyCustomHW`; a firmware target had no
such path, so the daemon acted on a decision it never recorded and the state file
kept naming the previous session's profile. That is self-correcting only while
autoswitch stays enabled and configured identically.
