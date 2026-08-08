// Package cli provides CLI input parsing, color resolution, and dry-run
// display helpers for z13ctl subcommands.
//
// File layout:
//
//	colors.go  — named color table, ResolveColor, ColorDisplay, PrintColorList
//	parse.go   — ParseColor, ParseBrightness
//	dryrun.go  — DryRun* display for every mutating command
//	forward.go — transitional forwarders to internal/drivers/asusz13
//
// The sysfs hardware layer this package used to carry lives in
// internal/drivers/asusz13 since the driver extraction; forward.go re-exports
// its surface so existing callers compile unchanged, and is deleted as cmd/
// and internal/daemon convert to the device registry.
package cli
