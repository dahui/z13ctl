// Package cli provides CLI input parsing, color resolution, dry-run display
// helpers, and sysfs path discovery for z13ctl subcommands.
//
// File layout:
//
//	colors.go  — named color table, ResolveColor, ColorDisplay, PrintColorList
//	parse.go   — ParseColor, ParseBrightness
//	dryrun.go  — DryRunApply, DryRunOff, DryRunBrightness
//	sysfs.go   — sysfs path finders and setters (profile, battery, firmware)
package cli
