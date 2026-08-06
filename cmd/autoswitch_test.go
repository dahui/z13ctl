package cmd

// autoswitch_test.go — flag-combination rejections for the autoswitch command,
// and profile-name parity with the daemon.
//
// Only cases that return before any socket or sysfs access belong here: the
// accepted paths call api.Send*, which reaches a running daemon and would
// reconfigure the developer's machine.

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dahui/z13ctl/internal/cli"
)

// resetAutoswitchFlags restores the package-level flag vars between cases.
// Cobra flags are process-global, so a case that leaves one set changes the
// meaning of the next.
func resetAutoswitchFlags(t *testing.T) {
	t.Helper()
	prev := struct {
		get, on, off, clear bool
		ac, battery         string
	}{autoswitchGetFlag, autoswitchOnFlag, autoswitchOffFlag, autoswitchClearFlag, autoswitchACFlag, autoswitchBatteryFlag}
	t.Cleanup(func() {
		autoswitchGetFlag, autoswitchOnFlag = prev.get, prev.on
		autoswitchOffFlag, autoswitchClearFlag = prev.off, prev.clear
		autoswitchACFlag, autoswitchBatteryFlag = prev.ac, prev.battery
	})
	autoswitchGetFlag, autoswitchOnFlag, autoswitchOffFlag, autoswitchClearFlag = false, false, false, false
	autoswitchACFlag, autoswitchBatteryFlag = "", ""
}

// fakeAutoswitchCmd is a command carrying the same flag set, so
// cmd.Flags().Changed() answers correctly without touching the real rootCmd.
func fakeAutoswitchCmd(args ...string) (*cobra.Command, error) {
	c := &cobra.Command{Use: "autoswitch", RunE: func(*cobra.Command, []string) error { return nil }}
	c.Flags().BoolVar(&autoswitchGetFlag, "get", false, "")
	c.Flags().StringVar(&autoswitchACFlag, "ac", "", "")
	c.Flags().StringVar(&autoswitchBatteryFlag, "battery", "", "")
	c.Flags().BoolVar(&autoswitchOnFlag, "on", false, "")
	c.Flags().BoolVar(&autoswitchOffFlag, "off", false, "")
	c.Flags().BoolVar(&autoswitchClearFlag, "clear", false, "")
	err := c.ParseFlags(args)
	return c, err
}

func TestAutoswitchRejectsConflictingFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"on with off", []string{"--on", "--off"}, "mutually exclusive"},
		{"clear with ac", []string{"--clear", "--ac", "balanced"}, "cannot be combined"},
		{"clear with battery", []string{"--clear", "--battery", "custom"}, "cannot be combined"},
		{"clear with on", []string{"--clear", "--on"}, "cannot be combined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAutoswitchFlags(t)
			c, err := fakeAutoswitchCmd(tt.args...)
			if err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}
			err = runAutoswitchSet(c)
			if err == nil {
				t.Fatalf("runAutoswitchSet(%v) = nil, want a rejection", tt.args)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestProfileNameValidationParity keeps the CLI's front-door check in step with
// the daemon's: both go through cli.ValidateProfileName, so a name the daemon
// would refuse must not get as far as the socket.
func TestProfileNameValidationParity(t *testing.T) {
	for _, name := range []string{"", "quiet", "balanced", "performance", "custom", "Gaming", "has space", "a/b"} {
		if err := cli.ValidateProfileName(name); err == nil {
			t.Errorf("ValidateProfileName(%q) = nil, want a rejection", name)
		}
	}
	for _, name := range []string{"gaming", "battery-uv", "my_profile2"} {
		if err := cli.ValidateProfileName(name); err != nil {
			t.Errorf("ValidateProfileName(%q) = %v, want nil", name, err)
		}
	}
}

// TestRequireDaemonForProfile covers the guard that keeps "--profile X" from
// falling through to the direct path, where it would apply the setting to the
// running machine — the opposite of what --profile asks for.
func TestRequireDaemonForProfile(t *testing.T) {
	if err := requireDaemonForProfile(""); err != nil {
		t.Errorf("requireDaemonForProfile(\"\") = %v, want nil so the normal direct path still works", err)
	}
	err := requireDaemonForProfile("battery-uv")
	if err == nil {
		t.Fatal("requireDaemonForProfile(\"battery-uv\") = nil, want an error explaining the daemon is required")
	}
	if !strings.Contains(err.Error(), "battery-uv") {
		t.Errorf("error = %q, want it to name the profile", err)
	}
}

func TestProfileEditMessage(t *testing.T) {
	if got := profileEditMessage("", "TDP set to 35W"); got != "TDP set to 35W\n" {
		t.Errorf("profileEditMessage with no profile = %q, want the applied message unchanged", got)
	}
	got := profileEditMessage("battery-uv", "TDP set to 35W")
	if strings.Contains(got, "TDP set to 35W") {
		t.Errorf("a --profile edit reported as applied: %q", got)
	}
	if !strings.Contains(got, "battery-uv") || !strings.Contains(got, "not applied") {
		t.Errorf("message = %q, want it to name the profile and say it was not applied", got)
	}
}

// TestAutoswitchPreservesTheUnnamedSide documents the rule the flag handling
// implements: naming one side changes only that side. Without it,
// "autoswitch --ac quiet" silently wiped the battery target, because the unset
// flag read as an explicit empty value.
//
// It asserts on the flag-resolution rule rather than driving runAutoswitchSet,
// which needs a live daemon to read the current configuration back.
func TestAutoswitchPreservesTheUnnamedSide(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		wantACGiven      bool
		wantBatteryGiven bool
	}{
		{"only --ac", []string{"--ac", "quiet"}, true, false},
		{"only --battery", []string{"--battery", "custom"}, false, true},
		{"both", []string{"--ac", "quiet", "--battery", "custom"}, true, true},
		{"neither", []string{"--on"}, false, false},
		// An explicit empty value is a deliberate clear, not an omission.
		{"explicit empty ac", []string{"--ac", ""}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAutoswitchFlags(t)
			c, err := fakeAutoswitchCmd(tt.args...)
			if err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}
			if got := c.Flags().Changed("ac"); got != tt.wantACGiven {
				t.Errorf("ac given = %v, want %v", got, tt.wantACGiven)
			}
			if got := c.Flags().Changed("battery"); got != tt.wantBatteryGiven {
				t.Errorf("battery given = %v, want %v", got, tt.wantBatteryGiven)
			}
		})
	}
}
