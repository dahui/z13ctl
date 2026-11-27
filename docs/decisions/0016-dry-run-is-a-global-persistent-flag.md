# `--dry-run` is a global persistent flag;

`--dry-run` is a global persistent flag; each command checks `dryRunFlag` and
calls the appropriate `cli.DryRun*` function.
