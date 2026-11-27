# `cli.DryRunTdp` takes the curve as a parameter; it must not read hwmon

**`cli.DryRunTdp` takes the curve as a parameter; it must not read hwmon.**
Mirroring `ApplyTDPSafely`'s own `want`, the CLI passes `liveFanCurve(hw)`.
Reading the fan mode inside the function made `internal/cli/dryrun_test.go`
depend on the developer's machine — and that test package is `cli_test`, so
`newFakeSysfs` is out of reach. It was not theoretical: only the `len(live) == 0`
branch prints `HighTDPMinPWM`, and `TestDryRunTdp_HighSustained` asserts on it,
so the test passed only while the machine happened to be on firmware auto and
failed outright with a curve live. Any future `DryRun*` that would consult
hardware state should take it as an argument for the same reason.
