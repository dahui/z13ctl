// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestWarnIfLegacyNameFiresOnOldName covers the compatibility symlink:
// /usr/bin/z13ctl -> voltaire, invoked by a script written before the rename.
func TestWarnIfLegacyNameFiresOnOldName(t *testing.T) {
	for _, argv0 := range []string{"z13ctl", "/usr/bin/z13ctl", "/usr/local/bin/z13ctl", "./z13ctl"} {
		var buf bytes.Buffer
		warnIfLegacyName(argv0, &buf)
		if !strings.Contains(buf.String(), "now voltaire") {
			t.Errorf("argv0=%q produced no notice: %q", argv0, buf.String())
		}
	}
}

// TestWarnIfLegacyNameSilentOnRealName: the overwhelmingly common invocation
// must print nothing at all.
func TestWarnIfLegacyNameSilentOnRealName(t *testing.T) {
	for _, argv0 := range []string{"voltaire", "/usr/bin/voltaire", "./voltaire", "", "voltaire-gui"} {
		var buf bytes.Buffer
		warnIfLegacyName(argv0, &buf)
		if buf.Len() != 0 {
			t.Errorf("argv0=%q printed %q, want nothing", argv0, buf.String())
		}
	}
}

// TestWarnIfLegacyNameDoesNotMatchSubstrings guards the obvious wrong
// implementation: a name merely *containing* z13ctl (a fork, a wrapper called
// z13ctl-wrapper, a directory named z13ctl) is not the legacy binary.
func TestWarnIfLegacyNameDoesNotMatchSubstrings(t *testing.T) {
	for _, argv0 := range []string{"z13ctl-wrapper", "my-z13ctl", "/home/user/z13ctl/voltaire"} {
		var buf bytes.Buffer
		warnIfLegacyName(argv0, &buf)
		if buf.Len() != 0 {
			t.Errorf("argv0=%q printed %q, want nothing", argv0, buf.String())
		}
	}
}
