# `--no-sleep-release` is likewise daemon-only: the pre-sleep fan release is skipped and a custom curve stays in force through suspend

`--no-sleep-release` is likewise daemon-only: the pre-sleep fan release is
skipped and a custom curve stays in force through suspend. Both flags reach the
daemon through `daemon.Options` rather than positional bools — `Run(ctx, true,
false)` said nothing about which flag was which, and a third would be worse.
