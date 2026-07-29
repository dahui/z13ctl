package cmd

// undervolt_test.go — flag parsing for "undervolt --set". Every case here is
// rejected before any SMU access, so no ryzen_smu module and no hardware are
// needed. Do not add a case with a valid, in-range value: runUndervoltSet would
// reach the real Curve Optimizer and change the developer's CPU voltage.

import (
	"strconv"
	"strings"
	"testing"
)

// TestRunUndervoltSetRejectsMalformedValues pins CLI/daemon parity. The CLI used
// fmt.Sscan, which stops at the first token — so "--set '-20 40'" was accepted
// as -20 here while the daemon parsed the same string with strconv.Atoi and
// rejected it. The two paths must agree on what a valid value is, since the CLI
// forwards the raw flag string to the daemon when one is running.
func TestRunUndervoltSetRejectsMalformedValues(t *testing.T) {
	malformed := []string{
		"-20 40",   // trailing token: the Sscan case
		"-20 junk", // trailing garbage
		"",         // empty
		"abc",
		"-20.5", // not an integer
		"- 20",  // detached sign
		"0x14",  // not base 10
	}

	orig := uvSetFlag
	t.Cleanup(func() { uvSetFlag = orig })

	for _, v := range malformed {
		t.Run(v, func(t *testing.T) {
			// The daemon's parser is the reference: whatever it rejects, the
			// CLI must reject too.
			if _, err := strconv.Atoi(v); err == nil {
				t.Fatalf("test case %q parses cleanly with strconv.Atoi; it is not malformed", v)
			}
			uvSetFlag = v
			err := runUndervoltSet()
			if err == nil {
				t.Fatalf("runUndervoltSet() with --set %q = nil, want a rejection", v)
			}
			if !strings.Contains(err.Error(), "must be an integer") {
				t.Errorf("error = %q, want it to explain that an integer is required", err)
			}
		})
	}
}

// TestRunUndervoltSetRejectsOutOfRange covers the safety envelope. These parse
// but fail ValidateCOValues, still before any SMU write.
func TestRunUndervoltSetRejectsOutOfRange(t *testing.T) {
	orig := uvSetFlag
	t.Cleanup(func() { uvSetFlag = orig })

	for _, v := range []string{"-100", "1", "50"} {
		t.Run(v, func(t *testing.T) {
			uvSetFlag = v
			if err := runUndervoltSet(); err == nil {
				t.Errorf("runUndervoltSet() with --set %q = nil, want a range rejection", v)
			}
		})
	}
}
