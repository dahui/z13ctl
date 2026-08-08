// Package cli provides CLI input parsing, color display, and dry-run
// display helpers for z13ctl subcommands.
//
// File layout:
//
//	colors.go  — color display (ColorDisplay, PrintColorList) over aura's table
//	parse.go   — ParseColor (forwarding to aura), ParseBrightness
//	dryrun.go  — DryRun* display for every mutating command
//	forward.go — transitional forwarders to internal/drivers/asusz13
//
// The sysfs hardware layer this package used to carry lives in
// internal/drivers/asusz13 since the driver extraction, and the color table
// moved to internal/aura with the lighting driver; forward.go re-exports the
// remaining surface so existing callers compile unchanged, and is deleted as
// its consumers convert to the device registry.
package cli
