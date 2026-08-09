// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package cmd

// The pre-2.0 command name, kept working through 2.x as a symlink.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// legacyCommandName is the pre-2.0 binary name. The packages install
// /usr/bin/z13ctl as a symlink to voltaire so that scripts, keybindings, and
// cron jobs written against z13ctl keep working across the rename — the CLI is
// otherwise the one interface with no compatibility path, since the socket,
// state file, config directory, and unit names all have their own.
const legacyCommandName = "z13ctl"

// warnIfLegacyName prints a deprecation notice when the binary was invoked
// through its pre-2.0 name.
//
// It writes to w (stderr in practice) and never to stdout: the whole point of
// the symlink is that a script parsing `z13ctl profile --get` keeps working
// byte-for-byte, so nothing may enter that stream. It likewise never changes
// the exit code. Warning on every invocation rather than once is deliberate —
// a script's stderr usually lands in a log its author reads, and the symlink
// goes away at 3.0, so silence until then would be a surprise later rather
// than a nudge now.
func warnIfLegacyName(argv0 string, w io.Writer) {
	if filepath.Base(argv0) != legacyCommandName {
		return
	}
	// A failed write to stderr is not worth reporting through: the notice is
	// advisory, and the command it precedes must run either way.
	_, _ = fmt.Fprintf(w, "note: %s is now voltaire; this name is a compatibility symlink and is removed in 3.0.\n"+
		"      Update scripts to call 'voltaire'. See https://dahui.github.io/voltaire/migrating-from-z13ctl/\n",
		legacyCommandName)
}

// Execute runs the root command.
func Execute() error {
	warnIfLegacyName(os.Args[0], os.Stderr)
	return rootCmd.Execute()
}
