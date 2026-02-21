// Package cli provides CLI input parsing, color resolution, and dry-run
// display helpers for z13ctl subcommands.
//
// File layout:
//
//	colors.go  — named color table, ResolveColor, ColorDisplay, PrintColorList
//	parse.go   — ParseColor, ParseBrightness
//	dryrun.go  — DryRunApply, DryRunOff, DryRunBrightness
package cli
