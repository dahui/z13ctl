// Package cli provides CLI input parsing, color display, and dry-run
// display helpers for voltaire subcommands.
//
// File layout:
//
//	colors.go — color display (ColorDisplay, PrintColorList) over aura's table
//	parse.go  — pure parsing/validation: colors, brightness, fan curves,
//	            TDP flag resolution, custom profile names
//	dryrun.go — DryRun* display for every mutating command
//
// The sysfs hardware layer this package used to carry lives in
// internal/drivers/asusz13 since the driver extraction, and the color table
// moved to internal/aura with the lighting driver. Callers reach hardware
// through the device registry's assembled device; what remains here is the
// pure, device-independent surface both cmd/ and the daemon share.
package cli
