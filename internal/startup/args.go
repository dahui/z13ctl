// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

// Package startup holds the process-startup logic that runs before any GTK code:
// command-line scanning and log filtering.
//
// It is separate from main and internal/gui so it can be unit tested — both of
// those need CGO and GTK4 headers to compile at all.
package startup

// Action is an immediate, exit-after-printing request from the command line.
type Action string

const (
	// ActionNone means carry on and start the GUI.
	ActionNone Action = ""

	// ActionVersion prints the version and exits.
	ActionVersion Action = "version"
	// ActionPrintTheme prints the default theme.toml and exits.
	ActionPrintTheme Action = "print-theme"
	// ActionListThemes prints the built-in theme IDs and exits.
	ActionListThemes Action = "list-themes"
)

// Args is the result of scanning the command line.
type Args struct {
	Debug   bool     // -d / --debug: log everything, including GTK internals
	Action  Action   // an immediate action to perform instead of starting
	GTKArgs []string // argv to hand to GApplication, including argv[0]
}

// ParseArgs scans argv for the flags z13gui handles itself and passes everything
// else through to GTK.
//
// The flag package is deliberately not used: app.Run() forwards the remaining
// arguments to GLib's option parser, which errors on anything it does not
// recognise, so our flags have to be removed from the slice rather than merely
// read. argv[0] is always preserved as the first passthrough element because
// GApplication expects it.
//
// When several actions are given the first one wins, matching the
// print-and-exit-immediately behaviour a user gets from a single flag. Flags are
// still scanned to the end so that -d anywhere on the line is honoured.
func ParseArgs(argv []string) Args {
	out := Args{Action: ActionNone}
	if len(argv) == 0 {
		out.GTKArgs = []string{""}
		return out
	}
	out.GTKArgs = []string{argv[0]}

	for _, arg := range argv[1:] {
		switch arg {
		case "--debug", "-d":
			out.Debug = true
		case "--version":
			if out.Action == ActionNone {
				out.Action = ActionVersion
			}
		case "--print-theme":
			if out.Action == ActionNone {
				out.Action = ActionPrintTheme
			}
		case "--list-themes":
			if out.Action == ActionNone {
				out.Action = ActionListThemes
			}
		default:
			out.GTKArgs = append(out.GTKArgs, arg)
		}
	}
	return out
}
