# The `profile` request field is silently ignored by older daemons, which makes an unguarded `--profile` edit apply to the running machine

**The `profile` request field is silently ignored by older daemons, which
makes an unguarded `--profile` edit apply to the running machine.** It is an
additive field, so a pre-1.3 daemon unmarshals the request, drops the field,
applies the setting live, and answers `ok` — the CLI would print "stored in
profile X (not applied)" over a real TDP change. This happened during
development. `cmd.ensureProfileTargetSupported` probes `profile-list` (which
answers `unknown command` on an older daemon) before every `--profile` send.
Any other client offering profile targeting must do the same.
